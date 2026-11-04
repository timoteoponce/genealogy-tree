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

// AuditWithUser is an audit row denormalized with the acting username.
type AuditWithUser struct {
	models.AuditEntry
	Username string
}

// RecentAuditWithUsers returns the latest entries joined with usernames.
func (q *Queries) RecentAuditWithUsers(ctx context.Context, limit int) ([]AuditWithUser, error) {
	rows, err := q.DB.QueryContext(ctx,
		`SELECT a.id, a.user_id, a.action, a.entity, a.entity_id, a.detail, a.at, u.username
FROM audit_log a LEFT JOIN users u ON u.id = a.user_id
ORDER BY a.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditWithUser{}
	for rows.Next() {
		var aw AuditWithUser
		var userID, entityID sql.NullInt64
		if err := rows.Scan(&aw.ID, &userID, &aw.Action, &aw.Entity, &entityID,
			&aw.Detail, &aw.At, &aw.Username); err != nil {
			return nil, err
		}
		aw.UserID = ptrInt64(userID)
		aw.EntityID = ptrInt64(entityID)
		out = append(out, aw)
	}
	return out, rows.Err()
}

// ImportRunRow is one imports-table row for the admin pages.
type ImportRunRow struct {
	ID        int64
	Filename  string
	Status    string
	People    int64
	Families  int64
	Sources   int64
	Events    int64
	MediaRows int64
	StartedAt string
}

// RecentImports returns the latest import runs.
func (q *Queries) RecentImports(ctx context.Context, limit int) ([]ImportRunRow, error) {
	rows, err := q.DB.QueryContext(ctx,
		`SELECT id, filename, status, people_count, families_count, sources_count, events_count, media_count, started_at
FROM imports ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ImportRunRow{}
	for rows.Next() {
		var r ImportRunRow
		if err := rows.Scan(&r.ID, &r.Filename, &r.Status, &r.People, &r.Families,
			&r.Sources, &r.Events, &r.MediaRows, &r.StartedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
