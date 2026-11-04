package store

import (
	"context"
	"database/sql"
	"fmt"
)

// ProfilePhotoIDs returns person_id -> media_id maps for a batch of people
// (used by list pages; one query instead of N).
func (q *Queries) ProfilePhotoIDs(ctx context.Context, personIDs []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(personIDs) == 0 {
		return out, nil
	}
	// SQLite has no array params; build a safe (internal, non-user) list.
	in := make([]byte, 0, len(personIDs)*4)
	args := make([]any, 0, len(personIDs))
	for i, id := range personIDs {
		if i > 0 {
			in = append(in, ',')
		}
		in = append(in, '?')
		args = append(args, id)
	}
	query := `SELECT person_id, id FROM media WHERE is_profile=1 AND person_id IN (` + string(in) + `)`
	rows, err := q.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var pid, mid int64
		if err := rows.Scan(&pid, &mid); err != nil {
			return out, err
		}
		out[pid] = mid
	}
	return out, rows.Err()
}

// ProfilePhotoID returns the single profile photo id of a person (0 = none).
func (q *Queries) ProfilePhotoID(ctx context.Context, personID int64) (int64, error) {
	var id int64
	err := q.DB.QueryRowContext(ctx,
		`SELECT id FROM media WHERE person_id=? AND is_profile=1 LIMIT 1`, personID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("profile photo id %d: %w", personID, err)
	}
	return id, nil
}

// ChildRelationship returns how a person is linked to a family ("birth",
// "adopted", "foster") and whether such a link exists.
func (q *Queries) ChildRelationship(ctx context.Context, familyID, personID int64) (string, bool, error) {
	var rel string
	err := q.DB.QueryRowContext(ctx,
		`SELECT relationship FROM child_links WHERE family_id=? AND person_id=?`,
		familyID, personID).Scan(&rel)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return rel, true, nil
}
