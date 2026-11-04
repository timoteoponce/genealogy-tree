package store

import (
	"context"
	"fmt"

	"genealogy-tree/internal/models"
)

const userCols = "id, username, password_hash, role, created_at, updated_at"

func scanUser(row interface{ Scan(dest ...any) error }) (models.User, error) {
	var u models.User
	var username, passwordHash, role string
	err := row.Scan(&u.ID, &username, &passwordHash, &role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return u, err
	}
	u.Username = username
	u.PasswordHash = passwordHash
	u.Role = role
	return u, nil
}

// CreateUser inserts a user (password must already be bcrypt-hashed).
func (q *Queries) CreateUser(ctx context.Context, username, passwordHash, role string) (int64, error) {
	switch role {
	case "admin", "editor", "viewer":
	default:
		return 0, fmt.Errorf("invalid role %q", role)
	}
	res, err := q.DB.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role) VALUES (?,?,?)`,
		username, passwordHash, role)
	if err != nil {
		return 0, fmt.Errorf("create user %s: %w", username, err)
	}
	return lastID(res)
}

// UserByUsername fetches a login user or sql.ErrNoRows.
func (q *Queries) UserByUsername(ctx context.Context, username string) (models.User, error) {
	return scanUser(q.DB.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users WHERE username=?`, username))
}

// UserByID fetches a user or sql.ErrNoRows.
func (q *Queries) UserByID(ctx context.Context, id int64) (models.User, error) {
	return scanUser(q.DB.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users WHERE id=?`, id))
}

// ListUsers returns all users ordered by name.
func (q *Queries) ListUsers(ctx context.Context) ([]models.User, error) {
	rows, err := q.DB.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY username COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateUserPassword sets a new bcrypt hash.
func (q *Queries) UpdateUserPassword(ctx context.Context, id int64, passwordHash string) error {
	_, err := q.DB.ExecContext(ctx,
		`UPDATE users SET password_hash=?, updated_at=datetime('now') WHERE id=?`,
		passwordHash, id)
	return err
}

// UpdateUserRole changes one user's role.
func (q *Queries) UpdateUserRole(ctx context.Context, id int64, role string) error {
	switch role {
	case "admin", "editor", "viewer":
	default:
		return fmt.Errorf("invalid role %q", role)
	}
	_, err := q.DB.ExecContext(ctx,
		`UPDATE users SET role=?, updated_at=datetime('now') WHERE id=?`, role, id)
	return err
}

// DeleteUser removes the account; open sessions cascade away.
func (q *Queries) DeleteUser(ctx context.Context, id int64) error {
	_, err := q.DB.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	return err
}

// CountUsers returns the number of accounts.
func (q *Queries) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	err := q.DB.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// --- sessions -----------------------------------------------------------

// InsertSession stores a session (its SHA-256 hash, not the raw token).
func (q *Queries) InsertSession(ctx context.Context, tokenHash string, userID int64, csrf, expiresAt string) error {
	_, err := q.DB.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, expires_at, csrf) VALUES (?,?,?,?)`,
		tokenHash, userID, expiresAt, csrf)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// LookupSession returns the user behind a (hashed) session token, the stored
// CSRF token and the expiry. Returns sql.ErrNoRows when unknown/expired.
func (q *Queries) LookupSession(ctx context.Context, tokenHash string) (models.User, string, string, error) {
	var u models.User
	var csrf, expires string
	err := q.DB.QueryRowContext(ctx,
		`SELECT u.id, u.username, u.password_hash, u.role, u.created_at, u.updated_at, s.csrf, s.expires_at
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.id = ? AND s.expires_at > datetime('now')`, tokenHash).Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt,
		&csrf, &expires)
	if err != nil {
		return u, "", "", err
	}
	return u, csrf, expires, nil
}

// DeleteSession removes one session.
func (q *Queries) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := q.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, tokenHash)
	return err
}

// DeleteExpiredSessions prunes stale sessions; returns rows removed.
func (q *Queries) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := q.DB.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at < datetime('now')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpdateSessionExpiry rotates the expiry of a live session (sliding window).
func (q *Queries) UpdateSessionExpiry(ctx context.Context, tokenHash, expiresAt string) error {
	_, err := q.DB.ExecContext(ctx,
		`UPDATE sessions SET expires_at=? WHERE id=?`, expiresAt, tokenHash)
	return err
}
