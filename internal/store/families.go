package store

import (
	"context"
	"database/sql"
	"fmt"

	"genealogy-tree/internal/models"
)

const familyCols = "id, gedcom_xref, partner1_id, partner2_id, status, marriage_date," +
	" marriage_year, marriage_place, notes, raw_facts, created_at, updated_at"

func scanFamily(row interface{ Scan(dest ...any) error }) (models.Family, error) {
	var f models.Family
	var p1, p2, marriageYear sql.NullInt64
	var xref, status, marriageDate, marriagePlace, notes, rawFacts, createdAt, updatedAt sql.NullString
	err := row.Scan(&f.ID, &xref, &p1, &p2, &status, &marriageDate, &marriageYear,
		&marriagePlace, &notes, &rawFacts, &createdAt, &updatedAt)
	if err != nil {
		return f, err
	}
	f.GedcomXref = optString(xref)
	f.Partner1ID = ptrInt64(p1)
	f.Partner2ID = ptrInt64(p2)
	f.Status = optString(status)
	if f.Status == "" {
		f.Status = models.FamilyMarried
	}
	f.MarriageDate = optString(marriageDate)
	f.MarriageYear = ptrInt64(marriageYear)
	f.MarriagePlace = optString(marriagePlace)
	f.Notes = optString(notes)
	f.RawFacts = optString(rawFacts)
	f.CreatedAt = optString(createdAt)
	f.UpdatedAt = updatedAt.String
	return f, nil
}

// InsertFamily creates a family row (partners may be nil) and returns its ID.
func (q *Queries) InsertFamily(ctx context.Context, f *models.Family) (int64, error) {
	switch f.Status {
	case models.FamilyMarried, models.FamilyUnmarried, models.FamilyDivorced,
		models.FamilySeparated, models.FamilyOther:
	default:
		f.Status = models.FamilyMarried
	}

	res, err := q.DB.ExecContext(ctx, `INSERT INTO families
(gedcom_xref, partner1_id, partner2_id, status, marriage_date, marriage_year, marriage_place, notes, raw_facts)
VALUES (?,?,?,?,?,?,?,?,?)`,
		textArg(f.GedcomXref), yearArg(f.Partner1ID), yearArg(f.Partner2ID), f.Status,
		f.MarriageDate, yearArg(f.MarriageYear), f.MarriagePlace, f.Notes, f.RawFacts)
	if err != nil {
		return 0, fmt.Errorf("insert family: %w", err)
	}
	return lastID(res)
}

// UpdateFamily overwrites the editable fields of an existing family.
func (q *Queries) UpdateFamily(ctx context.Context, f *models.Family) error {
	_, err := q.DB.ExecContext(ctx, `UPDATE families SET
partner1_id=?, partner2_id=?, status=?, marriage_date=?, marriage_year=?, marriage_place=?,
notes=?, raw_facts=?, updated_at=datetime('now')
WHERE id=?`,
		yearArg(f.Partner1ID), yearArg(f.Partner2ID), f.Status,
		f.MarriageDate, yearArg(f.MarriageYear), f.MarriagePlace,
		f.Notes, f.RawFacts, f.ID)
	return err
}

// FamilyByID fetches one family or returns sql.ErrNoRows.
func (q *Queries) FamilyByID(ctx context.Context, id int64) (models.Family, error) {
	f, err := scanFamily(q.DB.QueryRowContext(ctx,
		`SELECT `+familyCols+` FROM families WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return f, err
	}
	if err != nil {
		return f, fmt.Errorf("family %d: %w", id, err)
	}
	return f, nil
}

// FamiliesOfPerson lists families where the person is a partner.
func (q *Queries) FamiliesOfPerson(ctx context.Context, personID int64) ([]models.Family, error) {
	return queryFamilies(ctx, q.DB,
		`SELECT `+familyCols+` FROM families
WHERE partner1_id=? OR partner2_id=? ORDER BY id`, personID, personID)
}

// FamiliesOfChild lists families in which the person is a child.
func (q *Queries) FamiliesOfChild(ctx context.Context, personID int64) ([]models.Family, error) {
	return queryFamilies(ctx, q.DB,
		`SELECT f.`+familyCols+` FROM families f
JOIN child_links c ON c.family_id = f.id
WHERE c.person_id=? ORDER BY c.ord, f.id`, personID)
}

// ChildrenOfFamily lists the children of a family, in recorded order.
func (q *Queries) ChildrenOfFamily(ctx context.Context, familyID int64) ([]models.Person, error) {
	rows, err := q.DB.QueryContext(ctx,
		`SELECT p.* FROM child_links c JOIN people p ON p.id = c.person_id
WHERE c.family_id=? ORDER BY c.ord, c.id`, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Person{}
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AddChildLink adds a person to a family as a child; ord is appended at the end.
func (q *Queries) AddChildLink(ctx context.Context, familyID, personID int64, relationship string) (int64, error) {
	switch relationship {
	case models.ChildBirth, models.ChildAdopted, models.ChildFoster:
	default:
		relationship = models.ChildBirth
	}
	var ord int64
	if err := q.DB.QueryRowContext(ctx,
		`SELECT COALESCE(max(ord)+1, 0) FROM child_links WHERE family_id=?`, familyID).Scan(&ord); err != nil {
		return 0, err
	}
	res, err := q.DB.ExecContext(ctx, `INSERT INTO child_links (family_id, person_id, ord, relationship)
VALUES (?,?,?,?)`, familyID, personID, ord, relationship)
	if err != nil {
		return 0, fmt.Errorf("add child link: %w", err)
	}
	return lastID(res)
}

// RemoveChildLink removes the child's membership in one family.
func (q *Queries) RemoveChildLink(ctx context.Context, familyID, personID int64) error {
	_, err := q.DB.ExecContext(ctx,
		`DELETE FROM child_links WHERE family_id=? AND person_id=?`, familyID, personID)
	return err
}

// DeleteFamily removes a family (child_links cascade).
func (q *Queries) DeleteFamily(ctx context.Context, id int64) error {
	_, err := q.DB.ExecContext(ctx, `DELETE FROM families WHERE id=?`, id)
	return err
}

// CountFamilies returns the total number of families.
func (q *Queries) CountFamilies(ctx context.Context) (int64, error) {
	var n int64
	err := q.DB.QueryRowContext(ctx, `SELECT count(*) FROM families`).Scan(&n)
	return n, err
}

func queryFamilies(ctx context.Context, db DBTX, query string, args ...any) ([]models.Family, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Family{}
	for rows.Next() {
		f, err := scanFamily(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
