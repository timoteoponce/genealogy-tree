package store

import (
	"context"
	"database/sql"
	"fmt"

	"genealogy-tree/internal/models"
)

// UpsertSource inserts a GEDCOM source, or refreshes it if the same xref
// already exists. Sources are import-only objects in v1.
func (q *Queries) UpsertSource(ctx context.Context, s *models.Source) (int64, error) {
	var id int64
	if s.GedcomXref == "" {
		err := q.DB.QueryRowContext(ctx,
			`INSERT INTO sources (gedcom_xref, title, author, publication, text)
VALUES (NULL,?,?,?,?) RETURNING id`,
			s.Title, s.Author, s.Publication, s.Text).Scan(&id)
		return id, err
	}
	err := q.DB.QueryRowContext(ctx,
		`INSERT INTO sources (gedcom_xref, title, author, publication, text)
VALUES (?,?,?,?,?)
ON CONFLICT(gedcom_xref) DO UPDATE SET
  title=excluded.title, author=excluded.author,
  publication=excluded.publication, text=excluded.text
RETURNING id`,
		s.GedcomXref, s.Title, s.Author, s.Publication, s.Text).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert source %s: %w", s.GedcomXref, err)
	}
	return id, nil
}

// CountSources returns the number of imported sources.
func (q *Queries) CountSources(ctx context.Context) (int64, error) {
	var n int64
	err := q.DB.QueryRowContext(ctx, `SELECT count(*) FROM sources`).Scan(&n)
	return n, err
}

// ListSources returns all imported sources.
func (q *Queries) ListSources(ctx context.Context) ([]models.Source, error) {
	rows, err := q.DB.QueryContext(ctx,
		`SELECT id, gedcom_xref, title, author, publication, text FROM sources ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Source{}
	for rows.Next() {
		var s models.Source
		var xref, title, author, publication, text sql.NullString
		if err := rows.Scan(&s.ID, &xref, &title, &author, &publication, &text); err != nil {
			return nil, err
		}
		s.GedcomXref = optString(xref)
		s.Title = optString(title)
		s.Author = optString(author)
		s.Publication = optString(publication)
		s.Text = optString(text)
		out = append(out, s)
	}
	return out, rows.Err()
}

// InsertCitation links a source to a person or family.
func (q *Queries) InsertCitation(ctx context.Context, c *models.Citation) (int64, error) {
	res, err := q.DB.ExecContext(ctx,
		`INSERT INTO citations (source_id, person_id, family_id, detail) VALUES (?,?,?,?)`,
		c.SourceID, yearArg(c.PersonID), yearArg(c.FamilyID), c.Detail)
	if err != nil {
		return 0, fmt.Errorf("insert citation: %w", err)
	}
	return lastID(res)
}

// CitationWithSource is a citation joined with its source for display.
type CitationWithSource struct {
	Citation     models.Citation
	SourceTitle  string
	SourceAuthor string
}

func queryCitations(ctx context.Context, db DBTX, query string, args ...any) ([]CitationWithSource, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CitationWithSource{}
	for rows.Next() {
		var cw CitationWithSource
		var personID, familyID sql.NullInt64
		var detail, title, author sql.NullString
		if err := rows.Scan(&cw.Citation.ID, &cw.Citation.SourceID, &personID, &familyID,
			&detail, &title, &author); err != nil {
			return nil, err
		}
		cw.Citation.PersonID = ptrInt64(personID)
		cw.Citation.FamilyID = ptrInt64(familyID)
		cw.Citation.Detail = optString(detail)
		cw.SourceTitle = optString(title)
		cw.SourceAuthor = optString(author)
		out = append(out, cw)
	}
	return out, rows.Err()
}

// ListCitationsForPerson returns a person's citations including source title.
func (q *Queries) ListCitationsForPerson(ctx context.Context, personID int64) ([]CitationWithSource, error) {
	return queryCitations(ctx, q.DB,
		`SELECT c.id, c.source_id, c.person_id, c.family_id, c.detail, s.title, s.author
FROM citations c JOIN sources s ON s.id = c.source_id
WHERE c.person_id=? ORDER BY c.id`, personID)
}

// ListCitationsForFamily returns a family's citations including source title.
func (q *Queries) ListCitationsForFamily(ctx context.Context, familyID int64) ([]CitationWithSource, error) {
	return queryCitations(ctx, q.DB,
		`SELECT c.id, c.source_id, c.person_id, c.family_id, c.detail, s.title, s.author
FROM citations c JOIN sources s ON s.id = c.source_id
WHERE c.family_id=? ORDER BY c.id`, familyID)
}
