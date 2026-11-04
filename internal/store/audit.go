package store

import (
	"context"
	"database/sql"

	"genealogy-tree/internal/models"
)

// InsertAudit records one action in the audit log. userID may be nil
// (system/import actions).
func (q *Queries) InsertAudit(ctx context.Context, db DBTX, userID *int64,
	action, entity string, entityID *int64, detail string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO audit_log (user_id, action, entity, entity_id, detail) VALUES (?,?,?,?,?)`,
		yearArg(userID), action, entity, yearArg(entityID), detail)
	return err
}

// RecentAudit returns the latest audit entries, newest first.
func (q *Queries) RecentAudit(ctx context.Context, limit int) ([]models.AuditEntry, error) {
	rows, err := q.DB.QueryContext(ctx,
		`SELECT id, user_id, action, entity, entity_id, detail, at
FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.AuditEntry{}
	for rows.Next() {
		var e models.AuditEntry
		var userID, entityID sql.NullInt64
		if err := rows.Scan(&e.ID, &userID, &e.Action, &e.Entity, &entityID,
			&e.Detail, &e.At); err != nil {
			return nil, err
		}
		e.UserID = ptrInt64(userID)
		e.EntityID = ptrInt64(entityID)
		out = append(out, e)
	}
	return out, rows.Err()
}
