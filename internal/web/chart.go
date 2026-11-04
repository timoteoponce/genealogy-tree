package web

import (
	"context"
	"net/http"

	"genealogy-tree/internal/models"
	"genealogy-tree/internal/store"
)

// treeRow is one line of the tree view.
type treeRow struct {
	Person models.Person
	Depth  int
	Spouse *models.Person // shown next to the row when present
}

const defaultTreeDepth = 4

// ancestorsOf walks parents upward (depth-first), max generations deep.
func ancestorsOf(ctx context.Context, q *store.Queries, start models.Person, maxDepth int) []treeRow {
	var rows []treeRow
	seen := map[int64]bool{start.ID: true}

	var walk func(p models.Person, depth int)
	walk = func(p models.Person, depth int) {
		if depth > maxDepth {
			return
		}
		fams, _ := q.FamiliesOfChild(ctx, p.ID)
		for _, fam := range fams {
			for _, pid := range []*int64{fam.Partner1ID, fam.Partner2ID} {
				if pid == nil || seen[*pid] {
					continue
				}
				seen[*pid] = true
				if pp, err := q.PersonByID(ctx, *pid); err == nil {
					rows = append(rows, treeRow{Person: pp, Depth: depth})
					walk(pp, depth+1)
				}
			}
		}
	}
	walk(start, 1)
	return rows
}

// descendantsOf walks children downward (depth-first), showing spouses.
func descendantsOf(ctx context.Context, q *store.Queries, start models.Person, maxDepth int) []treeRow {
	var rows []treeRow
	seen := map[int64]bool{start.ID: true}

	var walk func(p models.Person, depth int)
	walk = func(p models.Person, depth int) {
		if depth > maxDepth {
			return
		}
		fams, _ := q.FamiliesOfPerson(ctx, p.ID)
		for _, fam := range fams {
			for _, pid := range []*int64{fam.Partner1ID, fam.Partner2ID} {
				if pid != nil && *pid != p.ID && !seen[*pid] {
					seen[*pid] = true
					if sp, err := q.PersonByID(ctx, *pid); err == nil {
						rows = append(rows, treeRow{Person: sp, Depth: depth, Spouse: &sp})
					}
				}
			}
			kids, _ := q.ChildrenOfFamily(ctx, fam.ID)
			for _, kid := range kids {
				if seen[kid.ID] {
					continue
				}
				seen[kid.ID] = true
				rows = append(rows, treeRow{Person: kid, Depth: depth + 1})
				walk(kid, depth+1)
			}
		}
	}
	walk(start, 1)
	return rows
}

// treePage renders the tree view (server-side fallback for M4; the
// interactive family-chart widget replaces this in M5).
func (s *Server) treePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.St.Queries()

	id, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown person.")
		return
	}
	person, err := q.PersonByID(ctx, id)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That person does not exist.")
		return
	}
	depth := clampInt(atoiDefault(r.URL.Query().Get("gens"), defaultTreeDepth), 1, 8)

	data := s.baseData(r, "Tree: "+person.DisplayName())
	data["Person"] = person
	data["Ancestors"] = ancestorsOf(ctx, q, person, depth)
	data["Descendants"] = descendantsOf(ctx, q, person, depth)
	data["Gens"] = depth
	data["GensChoices"] = []int{2, 3, 4, 5, 6}
	data["PhotoID"], _ = q.ProfilePhotoID(ctx, person.ID)
	s.render(w, "tree.html", http.StatusOK, data)
}

// treeData serves the JSON for the interactive chart (arrives in M5).
func (s *Server) treeData(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "coming soon", http.StatusNotImplemented)
}
