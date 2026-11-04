package store

import (
	"context"
	"database/sql"
	"fmt"

	"genealogy-tree/internal/models"
)

// InsertMedia adds a media row. For real uploads pass data+thumb bytes;
// for import placeholders (GEDCOM OBJE references) pass nil and set
// m.ImportNote = true.
func (q *Queries) InsertMedia(ctx context.Context, m *models.Media, data, thumb []byte) (int64, error) {
	if data == nil {
		m.ImportNote = true
	}
	res, err := q.DB.ExecContext(ctx, `INSERT INTO media
(person_id, family_id, filename, mime, size, caption, is_profile, import_note, data, thumb)
VALUES (?,?,?,?,?,?,?,?,?,?)`,
		yearArg(m.PersonID), yearArg(m.FamilyID), m.Filename, m.Mime, m.Size,
		m.Caption, int64from(m.IsProfile), int64from(m.ImportNote), data, thumb)
	if err != nil {
		return 0, fmt.Errorf("insert media: %w", err)
	}
	return lastID(res)
}

func scanMedia(row interface{ Scan(dest ...any) error }) (models.Media, error) {
	var m models.Media
	var personID, familyID sql.NullInt64
	var filename, mime, caption, createdAt, updatedAt sql.NullString
	var size int64
	var isProfile, importNote int64
	err := row.Scan(&m.ID, &personID, &familyID, &filename, &mime, &size, &caption,
		&isProfile, &importNote, &createdAt, &updatedAt)
	if err != nil {
		return m, err
	}
	m.PersonID = ptrInt64(personID)
	m.FamilyID = ptrInt64(familyID)
	m.Filename = optString(filename)
	m.Mime = optString(mime)
	m.Size = size
	m.Caption = optString(caption)
	m.IsProfile = isProfile != 0
	m.ImportNote = importNote != 0
	m.CreatedAt = optString(createdAt)
	m.UpdatedAt = updatedAt.String
	return m, nil
}

const mediaMetaCols = "id, person_id, family_id, filename, mime, size, caption, is_profile, import_note, created_at, updated_at"

// ListMediaForPerson lists a person's photos (metadata only, no blobs).
// The profile photo comes first.
func (q *Queries) ListMediaForPerson(ctx context.Context, personID int64) ([]models.Media, error) {
	rows, err := q.DB.QueryContext(ctx,
		`SELECT `+mediaMetaCols+` FROM media WHERE person_id=?
ORDER BY is_profile DESC, id`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Media{}
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListMediaForFamily lists a family's media (metadata only).
func (q *Queries) ListMediaForFamily(ctx context.Context, familyID int64) ([]models.Media, error) {
	rows, err := q.DB.QueryContext(ctx,
		`SELECT `+mediaMetaCols+` FROM media WHERE family_id=? ORDER BY id`, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Media{}
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MediaByID fetches metadata for one media row or sql.ErrNoRows.
func (q *Queries) MediaByID(ctx context.Context, id int64) (models.Media, error) {
	m, err := scanMedia(q.DB.QueryRowContext(ctx,
		`SELECT `+mediaMetaCols+` FROM media WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return m, err
	}
	if err != nil {
		return m, fmt.Errorf("media %d: %w", id, err)
	}
	return m, nil
}

// ProfilePhoto returns the person's profile photo, if any.
func (q *Queries) ProfilePhoto(ctx context.Context, personID int64) (models.Media, error) {
	m, err := scanMedia(q.DB.QueryRowContext(ctx,
		`SELECT `+mediaMetaCols+` FROM media WHERE person_id=? AND is_profile=1 LIMIT 1`, personID))
	if err == sql.ErrNoRows {
		return m, err
	}
	if err != nil {
		return m, fmt.Errorf("profile photo for %d: %w", personID, err)
	}
	return m, nil
}

// MediaData loads the blobs of one media row (call only when serving bytes).
func (q *Queries) MediaData(ctx context.Context, id int64) (models.MediaData, error) {
	var d models.MediaData
	var data, thumb []byte
	var mime sql.NullString
	err := q.DB.QueryRowContext(ctx,
		`SELECT mime, data, thumb FROM media WHERE id=?`, id).Scan(&mime, &data, &thumb)
	if err != nil {
		return d, err
	}
	d.Mime = optString(mime)
	d.Data = data
	d.Thumb = thumb
	return d, nil
}

// SetProfilePhoto makes mediaID the one profile photo of the person.
func (q *Queries) SetProfilePhoto(ctx context.Context, personID, mediaID int64) error {
	tx, err := beginTx(ctx, q.DB)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE media SET is_profile=0 WHERE person_id=? AND is_profile=1`, personID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE media SET is_profile=1, updated_at=datetime('now') WHERE id=? AND person_id=?`,
		mediaID, personID); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateMediaCaption changes the display caption of a media row.
func (q *Queries) UpdateMediaCaption(ctx context.Context, id int64, caption string) error {
	_, err := q.DB.ExecContext(ctx,
		`UPDATE media SET caption=?, updated_at=datetime('now') WHERE id=?`, caption, id)
	return err
}

// DeleteMedia removes one media row.
func (q *Queries) DeleteMedia(ctx context.Context, id int64) error {
	_, err := q.DB.ExecContext(ctx, `DELETE FROM media WHERE id=?`, id)
	return err
}

// CountMedia returns the number of media rows (import placeholders included).
func (q *Queries) CountMedia(ctx context.Context) (int64, error) {
	var n int64
	err := q.DB.QueryRowContext(ctx, `SELECT count(*) FROM media`).Scan(&n)
	return n, err
}
