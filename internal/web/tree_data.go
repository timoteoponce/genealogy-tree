package web

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"genealogy-tree/internal/models"
)

// chartNode mirrors the family-chart data format exactly:
//
//	https://github.com/donatso/family-chart/blob/HEAD/docs/data-format.md
//
// id: string; data.gender MUST be "M" or "F"; rels must be bidirectional
// (if A lists B in children, B lists A in parents).
type chartNode struct {
	ID   string    `json:"id"`
	Data chartData `json:"data"`
	Rels chartRels `json:"rels"`
}

type chartData struct {
	Gender    string `json:"gender"`
	FirstName string `json:"first name"`
	LastName  string `json:"last name"`
	Birthday  string `json:"birthday"`
	Avatar    string `json:"avatar"`
	Years     string `json:"years"`
}

type chartRels struct {
	Parents  []string `json:"parents,omitempty"`
	Spouses  []string `json:"spouses,omitempty"`
	Children []string `json:"children,omitempty"`
}

// personIDstr is the stable JSON id of a person (chart data uses strings).
func personIDstr(id int64) string { return strconv.FormatInt(id, 10) }

// chartGender maps our sex codes to the two the chart library understands.
func chartGender(sex string) string {
	if sex == models.SexFemale {
		return "F"
	}
	return "M"
}

// chartDataOf builds the tree-data JSON around a focus person.
//
// Included nodes: the focus, up to `gens` generations of ancestors, `gens`
// generations of descendants, and every spouse of anyone included.
func (s *Server) chartDataOf(ctx context.Context, focus models.Person, gens int) []chartNode {
	q := s.St.Queries()

	families, _ := q.AllFamilies(ctx)
	links, _ := q.AllChildLinks(ctx)

	famByID := map[int64]models.Family{}
	kidsByFam := map[int64][]string{}    // family id -> child ids (ordered)
	famsByPartner := map[int64][]int64{} // person id -> family ids (as partner)
	byChild := map[int64][]int64{}       // person id -> family ids (as child)
	for _, f := range families {
		famByID[f.ID] = f
		for _, pid := range []*int64{f.Partner1ID, f.Partner2ID} {
			if pid != nil {
				famsByPartner[*pid] = append(famsByPartner[*pid], f.ID)
			}
		}
	}
	for _, l := range links {
		kidsByFam[l.FamilyID] = append(kidsByFam[l.FamilyID], personIDstr(l.PersonID))
		byChild[l.PersonID] = append(byChild[l.PersonID], l.FamilyID)
	}

	// BFS up (parents) and down (children), depth <= gens.
	included := map[int64]bool{focus.ID: true}
	frontier := []int64{focus.ID}
	for level := 1; level <= gens; level++ {
		var next []int64
		for _, pid := range frontier {
			// Parents of pid.
			for _, famID := range byChild[pid] {
				for _, partner := range []*int64{famByID[famID].Partner1ID, famByID[famID].Partner2ID} {
					if partner != nil && !included[*partner] {
						included[*partner] = true
						next = append(next, *partner)
					}
				}
			}
			// Children of pid.
			for _, famID := range famsByPartner[pid] {
				for _, cid := range kidsByFam[famID] {
					cid64, _ := strconv.ParseInt(cid, 10, 64)
					if !included[cid64] {
						included[cid64] = true
						next = append(next, cid64)
					}
				}
			}
		}
		frontier = next
	}

	// Spouses of everyone included join the set (they anchor the cards).
	for pid := range included {
		for _, famID := range famsByPartner[pid] {
			for _, partner := range []*int64{famByID[famID].Partner1ID, famByID[famID].Partner2ID} {
				if partner != nil && !included[*partner] {
					included[*partner] = true
				}
			}
		}
	}

	// Build rels ( bidirectional, chart-compatible shapes only).
	strID := personIDstr
	nodes := map[string]chartNode{}
	nodeOrder := []string{}
	personCache := map[int64]models.Person{}

	for pid := range included {
		p, ok := personCache[pid]
		if !ok {
			var err error
			if p, err = q.PersonByID(ctx, pid); err != nil {
				continue
			}
			personCache[pid] = p
		}
		n := chartNode{
			ID: strID(p.ID),
			Data: chartData{
				Gender:    chartGender(p.Sex),
				FirstName: p.GivenNames,
				LastName:  p.Surname,
				Birthday:  yearsPrefix(p),
				Avatar:    "/person/" + strID(p.ID) + "/thumb",
				Years:     p.Years(),
			},
			Rels: chartRels{},
		}
		nodes[n.ID] = n
		nodeOrder = append(nodeOrder, n.ID)
	}

	// Families fill rels.
	for famID, fam := range famByID {
		p1, p2 := fam.Partner1ID, fam.Partner2ID

		inclP1 := p1 != nil && included[*p1]
		inclP2 := p2 != nil && included[*p2]

		var kids []string
		for _, k := range kidsByFam[famID] {
			if kid64, err := strconv.ParseInt(k, 10, 64); err == nil && included[kid64] {
				kids = append(kids, strID(kid64))
			}
		}

		// Spouses + children on each included partner.
		if !inclP1 && !inclP2 {
			continue
		}
		if inclP1 {
			n := nodes[strID(*p1)]
			if p2 != nil && inclP2 {
				n.Rels.Spouses = appendUnique(n.Rels.Spouses, strID(*p2))
			}
			n.Rels.Children = appendUnique(n.Rels.Children, kids...)
			nodes[n.ID] = n
		}
		if inclP2 {
			n := nodes[strID(*p2)]
			if p1 != nil && inclP1 {
				n.Rels.Spouses = appendUnique(n.Rels.Spouses, strID(*p1))
			}
			n.Rels.Children = appendUnique(n.Rels.Children, kids...)
			nodes[n.ID] = n
		}

		// Parents on each included child.
		for _, k := range kids {
			n := nodes[k]
			if n.ID == "" {
				continue
			}
			if inclP1 {
				n.Rels.Parents = appendUnique(n.Rels.Parents, strID(*p1))
			}
			if inclP2 {
				n.Rels.Parents = appendUnique(n.Rels.Parents, strID(*p2))
			}
			nodes[n.ID] = n
		}
	}

	// Focus node first (the library centers on data[0] initially).
	out := make([]chartNode, 0, len(nodes))
	out = append(out, nodes[strID(focus.ID)])
	nodeOrder = slices.DeleteFunc(nodeOrder, func(id string) bool { return id == out[0].ID })
	slices.Sort(nodeOrder)
	for _, id := range nodeOrder {
		out = append(out, nodes[id])
	}
	return out
}

func yearsPrefix(p models.Person) string {
	if p.BirthYear != nil {
		return strconv.Itoa(int(*p.BirthYear))
	}
	return ""
}

func appendUnique(list []string, items ...string) []string {
	for _, it := range items {
		if !slices.Contains(list, it) {
			list = append(list, it)
		}
	}
	return list
}

// chartRelsAreSymmetric is a helper used by tests. Returns nil when every
// children/spouses pointer has its mirror in the partner node.
func chartRelsAreSymmetric(nodes []chartNode) error {
	childIDs := map[string]map[string]bool{}
	spouseIDs := map[string]map[string]bool{}
	for _, n := range nodes {
		childIDs[n.ID] = map[string]bool{}
		for _, pid := range n.Rels.Parents {
			childIDs[pid][n.ID] = true
		}
		for _, sid := range n.Rels.Spouses {
			spouseIDs[sid][n.ID] = true
		}
	}
	for _, n := range nodes {
		for _, cid := range n.Rels.Children {
			if !childIDs[n.ID][cid] {
				return errors.New("asymmetric child rel: " + n.ID + " -> " + cid)
			}
		}
		for _, sid := range n.Rels.Spouses {
			if !spouseIDs[n.ID][sid] {
				return errors.New("asymmetric spouse rel: " + n.ID + " -> " + sid)
			}
		}
	}
	return nil
}

// treeData serves GET /tree.json?person={id}&gens={n}.
func (s *Server) treeData(w http.ResponseWriter, r *http.Request) {
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
	gens := clampInt(atoiDefault(r.URL.Query().Get("gens"), defaultTreeDepth), 1, 8)
	nodes := s.chartDataOf(r.Context(), person, gens)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = WriteJSON(w, nodes)
}
