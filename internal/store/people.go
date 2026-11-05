package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"genealogy-tree/internal/models"
)

const personCols = "id, gedcom_xref, given_names, surname, married_name, nickname, sex," +
	" birth_date, birth_year, birth_place, death_date, death_year, death_place," +
	" living, notes, raw_facts, created_at, updated_at"

func scanPerson(row interface{ Scan(dest ...any) error }) (models.Person, error) {
	var p models.Person
	var sex string
	var living int64
	var birthYear, deathYear sql.NullInt64
	var xref, given, surname, married, nick, birthDate, birthPlace, deathDate, deathPlace,
		notes, rawFacts, createdAt, updatedAt sql.NullString
	err := row.Scan(&p.ID, &xref, &given, &surname, &married, &nick, &sex,
		&birthDate, &birthYear, &birthPlace, &deathDate, &deathYear, &deathPlace,
		&living, &notes, &rawFacts, &createdAt, &updatedAt)
	if err != nil {
		return p, err
	}
	p.GedcomXref = optString(xref)
	p.GivenNames = optString(given)
	p.Surname = optString(surname)
	p.MarriedName = optString(married)
	p.Nickname = optString(nick)
	p.Sex = sex
	p.BirthDate = optString(birthDate)
	p.BirthYear = ptrInt64(birthYear)
	p.BirthPlace = optString(birthPlace)
	p.DeathDate = optString(deathDate)
	p.DeathYear = ptrInt64(deathYear)
	p.DeathPlace = optString(deathPlace)
	p.Living = living != 0
	p.Notes = optString(notes)
	p.RawFacts = optString(rawFacts)
	p.CreatedAt = optString(createdAt)
	p.UpdatedAt = updatedAt.String
	return p, nil
}

// InsertPerson creates a person row and returns its ID. Living is recomputed.
func (q *Queries) InsertPerson(ctx context.Context, p *models.Person) (int64, error) {
	if p.Sex == "" {
		p.Sex = models.SexUnknown
	}
	p.Living = models.IsLiving(p.DeathYear, p.BirthYear)

	res, err := q.DB.ExecContext(ctx, `INSERT INTO people
(gedcom_xref, given_names, surname, married_name, nickname, sex,
 birth_date, birth_year, birth_place, death_date, death_year, death_place,
 living, notes, raw_facts)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		textArg(p.GedcomXref), p.GivenNames, p.Surname, p.MarriedName, p.Nickname, p.Sex,
		p.BirthDate, yearArg(p.BirthYear), p.BirthPlace,
		p.DeathDate, yearArg(p.DeathYear), p.DeathPlace,
		int64from(p.Living), p.Notes, p.RawFacts)
	if err != nil {
		return 0, fmt.Errorf("insert person: %w", err)
	}
	return lastID(res)
}

// UpdatePerson overwrites all editable fields of an existing person.
func (q *Queries) UpdatePerson(ctx context.Context, p *models.Person) error {
	if p.Sex == "" {
		p.Sex = models.SexUnknown
	}
	p.Living = models.IsLiving(p.DeathYear, p.BirthYear)
	_, err := q.DB.ExecContext(ctx, `UPDATE people SET
 given_names=?, surname=?, married_name=?, nickname=?, sex=?,
 birth_date=?, birth_year=?, birth_place=?, death_date=?, death_year=?, death_place=?,
 living=?, notes=?, raw_facts=?, updated_at=datetime('now')
WHERE id=?`,
		p.GivenNames, p.Surname, p.MarriedName, p.Nickname, p.Sex,
		p.BirthDate, yearArg(p.BirthYear), p.BirthPlace,
		p.DeathDate, yearArg(p.DeathYear), p.DeathPlace,
		int64from(p.Living), p.Notes, p.RawFacts, p.ID)
	return err
}

// PersonByID fetches one person or sql.ErrNoRows.
func (q *Queries) PersonByID(ctx context.Context, id int64) (models.Person, error) {
	p, err := scanPerson(q.DB.QueryRowContext(ctx,
		`SELECT `+personCols+` FROM people WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return p, err
	}
	if err != nil {
		return p, fmt.Errorf("person %d: %w", id, err)
	}
	return p, nil
}

// PersonByXref looks a person up by their original GEDCOM reference ("@I12@").
func (q *Queries) PersonByXref(ctx context.Context, xref string) (models.Person, error) {
	p, err := scanPerson(q.DB.QueryRowContext(ctx,
		`SELECT `+personCols+` FROM people WHERE gedcom_xref=?`, xref))
	return p, err
}

// SearchPeople returns a page of people ordered by surname. An empty query
// lists everyone. The second result is the total match count.
func (q *Queries) SearchPeople(ctx context.Context, search string, limit, offset int) ([]models.Person, int64, error) {
	search = strings.TrimSpace(strings.ToLower(search))
	var where string
	args := []any{}
	if search != "" {
		where = ` WHERE lower(surname || ' ' || given_names || ' ' || married_name || ' ' || nickname) LIKE '%' || ? || '%'`
		args = append(args, search)
	}

	var total int64
	if err := q.DB.QueryRowContext(ctx, `SELECT count(*) FROM people`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	rows, err := q.DB.QueryContext(ctx,
		`SELECT `+personCols+` FROM people`+where+
			` ORDER BY surname COLLATE NOCASE, given_names COLLATE NOCASE, id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.Person{}
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

// AllIDsPeople returns every person id (used to build chart data).
func (q *Queries) AllIDsPeople(ctx context.Context) ([]int64, error) {
	rows, err := q.DB.QueryContext(ctx, `SELECT id FROM people ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DeletePerson removes a person; events, child_links, citations and media
// rows that reference them are removed by FK cascades. Families whose
// parents were both removed may remain — call PruneEmptyFamilies after.
func (q *Queries) DeletePerson(ctx context.Context, id int64) error {
	_, err := q.DB.ExecContext(ctx, `DELETE FROM people WHERE id=?`, id)
	return err
}

// PruneEmptyFamilies deletes families with no partners and no children.
func (q *Queries) PruneEmptyFamilies(ctx context.Context) (int64, error) {
	res, err := q.DB.ExecContext(ctx, `DELETE FROM families
WHERE partner1_id IS NULL AND partner2_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM child_links WHERE child_links.family_id = families.id)`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountPeople returns the total number of people.
func (q *Queries) CountPeople(ctx context.Context) (int64, error) {
	var n int64
	err := q.DB.QueryRowContext(ctx, `SELECT count(*) FROM people`).Scan(&n)
	return n, err
}

// AllPeople loads every person (for the force-directed "everyone" graph).
// Ordered by id; fine for small/medium datasets (the graph endpoint is only
// surfaced when the tree is large).
func (q *Queries) AllPeople(ctx context.Context) ([]models.Person, error) {
	rows, err := q.DB.QueryContext(ctx, `SELECT `+personCols+` FROM people ORDER BY id`)
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

// LivingCount returns how many people are marked living.
func (q *Queries) LivingCount(ctx context.Context) (int64, error) {
	var n int64
	err := q.DB.QueryRowContext(ctx, `SELECT count(*) FROM people WHERE living=1`).Scan(&n)
	return n, err
}

func int64from(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func lastID(res sql.Result) (int64, error) {
	return res.LastInsertId()
}
