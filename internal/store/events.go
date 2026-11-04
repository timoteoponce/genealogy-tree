package store

import (
	"context"
	"database/sql"
	"fmt"

	"genealogy-tree/internal/models"
)

// InsertEvent stores one life event/fact row. Exactly one of PersonID /
// FamilyID should be set (both NULL is technically allowed but pointless).
func (q *Queries) InsertEvent(ctx context.Context, e *models.Event) (int64, error) {
	res, err := q.DB.ExecContext(ctx, `INSERT INTO events (person_id, family_id, tag, date, year, place, detail, sort)
VALUES (?,?,?,?,?,?,?,?)`,
		yearArg(e.PersonID), yearArg(e.FamilyID), e.Tag, e.Date, yearArg(e.Year),
		e.Place, e.Detail, e.Sort)
	if err != nil {
		return 0, fmt.Errorf("insert event %s: %w", e.Tag, err)
	}
	return lastID(res)
}

// DeleteEventsForPerson removes every event of a person (used by imports).
func (q *Queries) DeleteEventsForPerson(ctx context.Context, personID int64) error {
	_, err := q.DB.ExecContext(ctx, `DELETE FROM events WHERE person_id=?`, personID)
	return err
}

// ListEventsForPerson returns a person's events in display order.
func (q *Queries) ListEventsForPerson(ctx context.Context, personID int64) ([]models.Event, error) {
	return queryEvents(ctx, q.DB,
		`SELECT id, person_id, family_id, tag, date, year, place, detail, sort
FROM events WHERE person_id=? ORDER BY sort, id`, personID)
}

// ListEventsForFamily returns a family's events (e.g. MARR/DIV detail rows).
func (q *Queries) ListEventsForFamily(ctx context.Context, familyID int64) ([]models.Event, error) {
	return queryEvents(ctx, q.DB,
		`SELECT id, person_id, family_id, tag, date, year, place, detail, sort
FROM events WHERE family_id=? ORDER BY sort, id`, familyID)
}

func queryEvents(ctx context.Context, db DBTX, query string, args ...any) ([]models.Event, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Event{}
	for rows.Next() {
		var e models.Event
		var personID, familyID, year sql.NullInt64
		if err := rows.Scan(&e.ID, &personID, &familyID, &e.Tag, &e.Date, &year,
			&e.Place, &e.Detail, &e.Sort); err != nil {
			return nil, err
		}
		e.PersonID = ptrInt64(personID)
		e.FamilyID = ptrInt64(familyID)
		e.Year = ptrInt64(year)
		out = append(out, e)
	}
	return out, rows.Err()
}
