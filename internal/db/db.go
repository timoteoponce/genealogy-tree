// Package db opens the SQLite database and applies migrations.
//
// The driver is modernc.org/sqlite (pure Go — the binary stays statically
// linked with CGO_ENABLED=0). Journal mode is WAL and the busy timeout and
// foreign keys are enabled through the DSN pragmas; keep those pragmas —
// they are required for correct concurrent behavior.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // register the "sqlite" database/sql driver
)

// migrationsFS embeds ./migrations/*.sql from the repository, so the binary
// needs no runtime files.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Pragmas set on every connection:
//   - journal_mode(WAL): concurrent readers while one writes
//   - busy_timeout(5000): wait instead of erroring on lock contention
//   - foreign_keys(1):   enforce every FK in the schema
const dsnPragmas = "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"

// Open opens (creating if needed) the database at path and applies
// pending migrations. Callers must Close.
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir %s: %w", dir, err)
		}
	}
	d, err := sql.Open("sqlite", "file:"+path+dsnPragmas)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// One reusable connection is the fastest, safest setup for SQLite.
	d.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.PingContext(ctx); err != nil {
		d.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := Migrate(d); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// Migrate applies every migration file (by numeric prefix, oldest first)
// that has not been recorded in schema_version yet.
func Migrate(d *sql.DB) error {
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files) // 001_..., 002_... — numeric prefix ordering

	for _, name := range files {
		version := migrationVersion(name)
		applied, err := isApplied(d, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := apply(d, name, version); err != nil {
			return fmt.Errorf("migration %d (%s): %w", version, name, err)
		}
	}
	return nil
}

// migrationVersion extracts "3" from "migrations/003_foo.sql"; it errors out
// (by name) on malformed prefixes only when applied.
func migrationVersion(name string) int {
	base := strings.TrimSuffix(strings.TrimPrefix(name, "migrations/"), ".sql")
	if len(base) < 4 || base[3] != '_' {
		panic("migration filename must be NNN_something.sql: " + name)
	}
	var v int
	if _, err := fmt.Sscanf(base[:3], "%d", &v); err != nil {
		panic("bad migration numeric prefix in: " + name)
	}
	return v
}

func isApplied(d *sql.DB, version int) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT count(*) FROM schema_version WHERE version = ?`, version).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		// Table itself missing on a brand-new database.
		if strings.Contains(err.Error(), "no such table") {
			return false, nil
		}
		return false, fmt.Errorf("check schema_version: %w", err)
	}
	return n > 0, nil
}

func apply(d *sql.DB, name string, version int) error {
	sqlBytes, err := migrationsFS.ReadFile(name)
	if err != nil {
		return err
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(string(sqlBytes)); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, version); err != nil {
		return err
	}
	return tx.Commit()
}
