package web

import (
	"context"

	"genealogy-tree/internal/models"
	"genealogy-tree/internal/store"
)

// personView is a person row joined with its avatar for list templates.
type personView struct {
	Person         models.Person
	ProfilePhotoID int64 // 0 => placeholder
}

// decoratePeople attaches each person's profile photo id (one query).
func decoratePeople(ctx context.Context, q *store.Queries, people []models.Person) []personView {
	ids := map[int64]int64{}
	if len(people) > 0 {
		ids, _ = q.ProfilePhotoIDs(ctx, peopleIDs(people))
	}
	out := make([]personView, len(people))
	for i, p := range people {
		out[i] = personView{Person: p, ProfilePhotoID: ids[p.ID]}
	}
	return out
}

func peopleIDs(people []models.Person) []int64 {
	out := make([]int64, 0, len(people))
	for _, p := range people {
		out = append(out, p.ID)
	}
	return out
}

// marriageView is one spouse-family of a person.
type marriageView struct {
	Family   models.Family
	Spouse   *models.Person
	Children []models.Person
}

// marriagesOf returns the families where person is a partner.
func (s *Server) marriagesOf(ctx context.Context, person models.Person) []marriageView {
	q := s.St.Queries()
	fams, _ := q.FamiliesOfPerson(ctx, person.ID)
	out := make([]marriageView, 0, len(fams))
	for _, fam := range fams {
		mv := marriageView{Family: fam, Children: []models.Person{}}
		if fam.Partner1ID != nil && *fam.Partner1ID != person.ID {
			if p, err := q.PersonByID(ctx, *fam.Partner1ID); err == nil {
				sp := p
				mv.Spouse = &sp
			}
		}
		if fam.Partner2ID != nil && *fam.Partner2ID != person.ID {
			if p, err := q.PersonByID(ctx, *fam.Partner2ID); err == nil {
				sp := p
				mv.Spouse = &sp
			}
		}
		if mv.Children, _ = q.ChildrenOfFamily(ctx, fam.ID); mv.Children == nil {
			mv.Children = []models.Person{}
		}
		out = append(out, mv)
	}
	return out
}

// parentsAndSiblingsOf resolves the parent families of a person.
func (s *Server) parentsAndSiblingsOf(ctx context.Context, person models.Person) ([]models.Person, []models.Person) {
	q := s.St.Queries()
	fams, _ := q.FamiliesOfChild(ctx, person.ID)

	var parents []models.Person
	var siblings []models.Person
	seen := map[int64]bool{person.ID: true}

	for _, fam := range fams {
		for _, pid := range []*int64{fam.Partner1ID, fam.Partner2ID} {
			if pid == nil || seen[*pid] {
				continue
			}
			seen[*pid] = true
			if p, err := q.PersonByID(ctx, *pid); err == nil {
				parents = append(parents, p)
			}
		}
		kids, _ := q.ChildrenOfFamily(ctx, fam.ID)
		for _, k := range kids {
			if !seen[k.ID] {
				seen[k.ID] = true
				siblings = append(siblings, k)
			}
		}
	}
	return parents, siblings
}

// childRelationship returns how a person relates to a family ("birth",
// "adopted"…) looking at the child_links row.
func childRelationship(ctx context.Context, q *store.Queries, familyID, personID int64) string {
	fams, _ := q.FamiliesOfChild(ctx, personID)
	for _, f := range fams {
		if f.ID == familyID {
			// FamiliesOfChild joins only; relationship needs the link row.
			if rel, ok, _ := q.ChildRelationship(ctx, familyID, personID); ok {
				return rel
			}
		}
	}
	return models.ChildBirth
}
