package web

import (
	"context"
	"net/http"
	"strconv"

	"genealogy-tree/internal/models"
)

// --- Fan chart (radial pedigree) -------------------------------------------
//
// The fan chart shows the focus person in the center with ancestors in
// concentric rings. Data is a d3-hierarchy-shaped tree (children = parents),
// which the frontend lays out with d3.cluster into a radial fan. Missing
// ancestors are simply absent (d3 leaves a gap), so it tolerates real,
// incomplete trees without fake slots.

// fanNode is one person in the ancestor tree.
type fanNode struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Years    string     `json:"years"`
	Sex      string     `json:"sex"`
	Avatar   string     `json:"avatar"`
	Children []*fanNode `json:"children,omitempty"`
}

// ancestorTree builds the ancestor fan tree (cycle-safe via a seen set).
func (s *Server) ancestorTree(ctx context.Context, person models.Person, gens int) *fanNode {
	q := s.St.Queries()
	seen := map[int64]bool{person.ID: true}

	var build func(p models.Person, depth int) *fanNode
	build = func(p models.Person, depth int) *fanNode {
		n := &fanNode{
			ID:     personIDstr(p.ID),
			Name:   p.DisplayName(),
			Years:  p.Years(),
			Sex:    p.Sex,
			Avatar: "/person/" + personIDstr(p.ID) + "/thumb",
		}
		if depth >= gens {
			return n
		}
		fams, _ := q.FamiliesOfChild(ctx, p.ID)
		for _, fam := range fams {
			for _, pid := range []*int64{fam.Partner1ID, fam.Partner2ID} {
				if pid == nil || seen[*pid] {
					continue
				}
				seen[*pid] = true
				if parent, err := q.PersonByID(ctx, *pid); err == nil {
					n.Children = append(n.Children, build(parent, depth+1))
				}
			}
		}
		return n
	}
	return build(person, 0)
}

// fanData serves GET /fan.json?person={id}&gens={n}.
func (s *Server) fanData(w http.ResponseWriter, r *http.Request) {
	focusID, err := strconv.ParseInt(r.URL.Query().Get("person"), 10, 64)
	if err != nil || focusID <= 0 {
		s.renderError(w, r, http.StatusBadRequest, "expected ?person=<id>")
		return
	}
	person, err := s.St.Queries().PersonByID(r.Context(), focusID)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "person not found")
		return
	}
	gens := defaultTreeDepth
	if raw := r.URL.Query().Get("gens"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 && n <= 10 {
			gens = n
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = WriteJSON(w, s.ancestorTree(r.Context(), person, gens))
}

// --- Force-directed "everyone" graph ---------------------------------------
//
// Every person is a node; parent↔child and spouse relationships are links.
// The frontend simulates with d3-force, so the whole dataset (including
// disconnected branches) is visible at once.

type graphNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Years  string `json:"years"`
	Sex    string `json:"sex"`
	Avatar string `json:"avatar"`
}

type graphLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"` // "spouse" | "parent"
}

type graphData struct {
	Nodes []graphNode `json:"nodes"`
	Links []graphLink `json:"links"`
}

// graphData serves GET /graph.json (the whole dataset).
func (s *Server) graphData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.St.Queries()

	people, _ := q.AllPeople(ctx)
	families, _ := q.AllFamilies(ctx)
	links, _ := q.AllChildLinks(ctx)

	out := graphData{Nodes: make([]graphNode, 0, len(people))}
	for _, p := range people {
		out.Nodes = append(out.Nodes, graphNode{
			ID:     personIDstr(p.ID),
			Name:   p.DisplayName(),
			Years:  p.Years(),
			Sex:    p.Sex,
			Avatar: "/person/" + personIDstr(p.ID) + "/thumb",
		})
	}

	for _, f := range families {
		if f.Partner1ID != nil && f.Partner2ID != nil {
			out.Links = append(out.Links, graphLink{
				Source: personIDstr(*f.Partner1ID),
				Target: personIDstr(*f.Partner2ID),
				Type:   "spouse",
			})
		}
	}
	for _, l := range links {
		fam, ok := famByID(families, l.FamilyID)
		if !ok {
			continue
		}
		for _, parent := range []*int64{fam.Partner1ID, fam.Partner2ID} {
			if parent != nil {
				out.Links = append(out.Links, graphLink{
					Source: personIDstr(*parent),
					Target: personIDstr(l.PersonID),
					Type:   "parent",
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = WriteJSON(w, out)
}

// famByID indexes families for the graph builder.
func famByID(fams []models.Family, id int64) (models.Family, bool) {
	for _, f := range fams {
		if f.ID == id {
			return f, true
		}
	}
	return models.Family{}, false
}

// --- adaptive view default -------------------------------------------------

// defaultView picks the starting visualization from how many people are in
// scope, so the standard tree is used while it's readable and denser
// layouts take over when it gets too big.
func defaultView(nodeCount int64, isFull bool) string {
	switch {
	case nodeCount <= 20:
		return "tree"
	case nodeCount <= 200:
		return "compact"
	default:
		if isFull {
			return "graph" // many branches: only the force graph shows them all
		}
		return "fan" // deep ancestors: the radial fan scales best
	}
}

// resolveView normalizes the ?view= query into one of the valid chart modes
// (tree|compact|fan|graph|list), or falls back to the adaptive default.
func resolveView(query, fallback string) string {
	switch query {
	case "tree", "compact", "fan", "graph", "list":
		return query
	default:
		return fallback
	}
}
