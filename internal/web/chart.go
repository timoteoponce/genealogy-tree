package web

import (
	"context"
	"net/http"
	"strconv"

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

// fullTreePage handles GET /tree: the whole-tree view (all reachable
// people), focused on any person via ?person= (default: the largest root).
func (s *Server) fullTreePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := qOf(s, r)

	roots, err := s.rootStats(ctx)
	if err != nil || len(roots) == 0 {
		data := s.baseData(r, "Family tree")
		data["Empty"] = true
		s.render(w, "tree.html", http.StatusOK, data)
		return
	}

	// Focus: ?person=<any id> wins; otherwise the largest root.
	var person models.Person
	if raw := r.URL.Query().Get("person"); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			person, _ = q.PersonByID(ctx, id)
		}
	}
	if person.ID == 0 {
		person, _ = q.PersonByID(ctx, roots[0].ID)
	}
	if person.ID == 0 {
		data := s.baseData(r, "Family tree")
		data["Empty"] = true
		s.render(w, "tree.html", http.StatusOK, data)
		return
	}

	data := s.baseData(r, "Family tree")
	data["Person"] = person
	data["Gens"] = 0 // 0 = everything reachable
	data["FullTree"] = true
	data["TreeBase"] = "/tree"
	data["PhotoID"], _ = q.ProfilePhotoID(ctx, person.ID)
	data["Orient"] = orientParam(r, "h") // descendant-heavy → horizontal by default

	// Adaptive view: default by how many people are in scope.
	nodeCount, _ := q.CountPeople(ctx)
	view := resolveView(r.URL.Query().Get("view"), defaultView(nodeCount, true))
	data["NodeCount"] = nodeCount
	data["View"] = view
	data["Compact"] = view == "tree" && nodeCount > 20

	if view == "list" {
		data["Ancestors"] = ancestorsOf(ctx, q, person, 100)
		data["Descendants"] = descendantsOf(ctx, q, person, 100)
	} else {
		data["Ancestors"], data["Descendants"] = nil, nil
	}

	data["People"] = s.pickerPeople(ctx)
	s.render(w, "tree.html", http.StatusOK, data)
}

// treePage renders the tree view: the interactive family-chart by default,
// or the plain server-rendered indent lists with ?view=list.
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
	data["Gens"] = depth
	data["GensChoices"] = []int{2, 3, 4, 5, 6}
	data["TreeBase"] = "/person/" + strconv.FormatInt(id, 10) + "/tree"
	data["PhotoID"], _ = q.ProfilePhotoID(ctx, person.ID)
	data["Orient"] = orientParam(r, "v") // classic pedigree: vertical by default

	// Adaptive view default from how many people the tree reaches.
	nodeCount := int64(len(s.chartDataOf(ctx, person, depth)))
	view := resolveView(r.URL.Query().Get("view"), defaultView(nodeCount, false))
	data["NodeCount"] = nodeCount
	data["View"] = view
	data["Compact"] = view == "tree" && nodeCount > 20

	if view == "list" {
		data["Ancestors"] = ancestorsOf(ctx, q, person, depth)
		data["Descendants"] = descendantsOf(ctx, q, person, depth)
	} else {
		data["Ancestors"], data["Descendants"] = nil, nil
	}
	data["People"] = s.pickerPeople(ctx)
	s.render(w, "tree.html", http.StatusOK, data)
}

// orientParam returns "h" (horizontal) when the query asks for it, else the
// given default.
func orientParam(r *http.Request, fallback string) string {
	if r.URL.Query().Get("orient") == "h" {
		return "h"
	}
	return fallback
}

// treeData moved to tree_data.go (family-chart JSON with full rels).
