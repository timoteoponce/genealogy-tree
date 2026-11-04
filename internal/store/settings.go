package store

import (
	"context"
	"database/sql"
)

// GetSetting returns a setting value and whether it exists.
func (q *Queries) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := q.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetSetting inserts or updates one setting.
func (q *Queries) SetSetting(ctx context.Context, key, value string) error {
	_, err := q.DB.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// Settings returns every setting as a key -> value map.
func (q *Queries) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := q.DB.QueryContext(ctx, `SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
