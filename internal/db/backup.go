// Package db: backup helper using SQLite's online VACUUM INTO.

package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BackupTo writes a fresh, consistent copy of the database to dst using
// `VACUUM INTO` (safe against concurrent access, WAL included).
func BackupTo(src *sql.DB, dst string) (int64, error) {
	if abs, err := filepath.Abs(dst); err == nil {
		dst = abs
	}
	if _, err := os.Stat(dst); err == nil {
		return 0, fmt.Errorf("backup file %s already exists", dst)
	}
	dst = filepath.Clean(dst)
	if err := ensureDir(dst); err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if _, err := src.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		return 0, fmt.Errorf("vacuum into %s: %w", dst, err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// ensureDir creates the parent directory of a file path.
func ensureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}
