// Package store contains all SQL access. It uses hand-written queries with
// database/sql only (no ORM), in the "Queries over DBTX" style:
//
// Every query is a method on *Queries, which wraps either the *sql.DB or an
// open *sql.Tx. That lets callers (e.g. the GEDCOM importer) run many writes
// inside one transaction and still reuse the same query functions.
//
// Conventions:
//   - Nullable integer columns (birth_year, …) are *int64 in models; passing
//     a nil *int64 as a query arg stores NULL (database/sql dereferences).
//   - Timestamps are SQLite text values (datetime('now')), scanned as string.
//   - BLOB columns (media.data/thumb) are only read by dedicated functions.
package store

import (
	"context"
	"database/sql"
)

// DBTX is the subset of *sql.DB and *sql.Tx that Queries needs.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Queries runs one set of statements against a DBTX (DB or Tx).
type Queries struct {
	DB DBTX
}

// NewQueries wraps db (a *sql.DB or *sql.Tx) into a read-write query handle.
func NewQueries(db DBTX) *Queries { return &Queries{DB: db} }

// Store owns the connection. Constructed by internal/db.Open.
type Store struct {
	DB *sql.DB
}

// New wraps an open *sql.DB.
func New(db *sql.DB) *Store { return &Store{DB: db} }

// Close closes the underlying database.
func (s *Store) Close() error { return s.DB.Close() }

// Queries returns a handle running directly on the connection pool.
func (s *Store) Queries() *Queries { return NewQueries(s.DB) }

// Begin opens a transaction; use store.NewQueries(tx) to run queries inside
// it, then tx.Commit() / tx.Rollback(). Callers MUST handle the error.
func (s *Store) Begin(ctx context.Context) (*sql.Tx, error) {
	return s.DB.BeginTx(ctx, nil)
}

// --- small scan helpers -------------------------------------------------

// ptrInt64 converts a nullable scanned value into *int64.
func ptrInt64(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

// yearArg allows nil-*int64 args (stored as NULL).
func yearArg(p *int64) any { return p }

// textArg maps "" to NULL so unique text columns (gedcom_xref) don't hold
// multiple copies of the empty string, which would violate UNIQUE.
func textArg(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// beginTx opens a transaction on db regardless of whether it is a *sql.DB or
// already an open *sql.Tx (in which case it is simply reused).
func beginTx(ctx context.Context, db DBTX) (*sql.Tx, error) {
	switch d := db.(type) {
	case *sql.DB:
		return d.BeginTx(ctx, nil)
	case *sql.Tx:
		return d, nil
	default:
		panic("store: unknown DBTX implementation")
	}
}

// optString yields "" instead of NULL text values.
func optString(s sql.NullString) string {
	if !s.Valid {
		return ""
	}
	return s.String
}
