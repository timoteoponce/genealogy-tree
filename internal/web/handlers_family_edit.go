package web

import (
	"context"
	"net/http"
	"strconv"

	"genealogy-tree/internal/models"
	"genealogy-tree/internal/store"
)

// -- family edit -------------------------------------------------------------

func (s *Server) familyEditPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	famID, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown family.")
		return
	}
	family, err := qOf(s, r).FamilyByID(ctx, famID)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That family does not exist.")
		return
	}

	var partners [2]models.Person
	slots := [2]*int64{family.Partner1ID, family.Partner2ID}
	for i, pid := range slots {
		if pid != nil {
			if p, err := qOf(s, r).PersonByID(ctx, *pid); err == nil {
				partners[i] = p
			}
		}
	}
	kids, _ := qOf(s, r).ChildrenOfFamily(ctx, famID)

	data := s.baseData(r, "Edit family")
	data["Family"] = family
	data["Partner1"] = partners[0]
	data["Partner2"] = partners[1]
	data["Partner1Filled"] = partners[0].ID != 0
	data["Partner2Filled"] = partners[1].ID != 0
	data["Children"] = kids
	data["StatusOptions"] = statusOptions(family.Status)
	data["People"] = s.pickerPeople(ctx)
	s.render(w, "family_form.html", http.StatusOK, data)
}

func (s *Server) familyUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	famID, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown family.")
		return
	}
	f, formErr := familyFromForm(r, famID)
	if formErr != "" {
		s.renderError(w, r, http.StatusUnprocessableEntity, formErr)
		return
	}

	q := qOf(s, r)
	// Partners may be reassigned on the form (or left untouched when the
	// hidden fields carry the existing values).
	if p1 := formID(r, "partner1_id"); p1 > 0 {
		f.Partner1ID = &p1
	} else {
		f.Partner1ID = nil
	}
	if p2 := formID(r, "partner2_id"); p2 > 0 {
		f.Partner2ID = &p2
	} else {
		f.Partner2ID = nil
	}

	existing, err := q.FamilyByID(ctx, famID)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That family does not exist.")
		return
	}
	// Preserve the xref from import if this family had one.
	f.GedcomXref = existing.GedcomXref
	f.RawFacts = existing.RawFacts
	if f.Status == "" {
		f.Status = existing.Status
	}

	if err := q.UpdateFamily(ctx, &f); err != nil {
		s.renderError(w, r, http.StatusUnprocessableEntity, "Could not save family: "+err.Error())
		return
	}
	_ = q.InsertAudit(ctx, s.St.DB, userIDof(r), "update", "family", &famID, "")
	http.Redirect(w, r, "/family/"+strconv.FormatInt(famID, 10), http.StatusFound)
}

func (s *Server) familyDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	famID, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown family.")
		return
	}
	if formVal(r, "confirm") != "yes" {
		s.renderError(w, r, http.StatusBadRequest, "Deletion needs the confirm box ticked.")
		return
	}
	q := qOf(s, r)
	if err := q.DeleteFamily(ctx, famID); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Delete failed: "+err.Error())
		return
	}
	_ = q.InsertAudit(ctx, s.St.DB, userIDof(r), "delete", "family", &famID, "")
	http.Redirect(w, r, "/people", http.StatusFound)
}

// familyAttachPartner POSTs a person into a free partner slot.
func (s *Server) familyAttachPartner(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	famID, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown family.")
		return
	}
	slot := formVal(r, "slot")
	personID := formID(r, "person_id")
	if slot != "1" && slot != "2" {
		s.renderError(w, r, http.StatusBadRequest, "slot must be 1 or 2")
		return
	}

	q := qOf(s, r)
	family, err := q.FamilyByID(ctx, famID)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That family does not exist.")
		return
	}
	if _, err := q.PersonByID(ctx, personID); err != nil {
		s.renderError(w, r, http.StatusNotFound, "That person does not exist.")
		return
	}

	slotTarget := &personID
	slotVal := slotTarget
	switch slot {
	case "1":
		family.Partner1ID = slotVal
	case "2":
		family.Partner2ID = slotVal
	}
	if err := q.UpdateFamily(ctx, &family); err != nil {
		s.renderError(w, r, http.StatusUnprocessableEntity, "Could not attach partner: "+err.Error())
		return
	}
	_ = q.InsertAudit(ctx, s.St.DB, userIDof(r), "attach-partner", "family", &famID, "slot "+slot)
	http.Redirect(w, r, "/family/"+strconv.FormatInt(famID, 10)+"/edit", http.StatusFound)
}

// familyDetachPartner removes a partner from the family (person stays).
func (s *Server) familyDetachPartner(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	famID, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown family.")
		return
	}
	slot := formVal(r, "slot")
	if slot != "1" && slot != "2" {
		s.renderError(w, r, http.StatusBadRequest, "slot must be 1 or 2")
		return
	}
	q := qOf(s, r)
	family, err := q.FamilyByID(ctx, famID)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That family does not exist.")
		return
	}
	switch slot {
	case "1":
		family.Partner1ID = nil
	case "2":
		family.Partner2ID = nil
	}
	if err := q.UpdateFamily(ctx, &family); err != nil {
		s.renderError(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if _, err := q.PruneEmptyFamilies(ctx); err != nil {
		_ = err // non-fatal
	}
	_ = q.InsertAudit(ctx, s.St.DB, userIDof(r), "detach-partner", "family", &famID, "slot "+slot)
	http.Redirect(w, r, "/family/"+strconv.FormatInt(famID, 10)+"/edit", http.StatusFound)
}

// familyAddChild POSTs either an existing person id or a new person into a
// family's child list.
func (s *Server) familyAddChild(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	famID, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown family.")
		return
	}
	q := qOf(s, r)
	if _, err := q.FamilyByID(ctx, famID); err != nil {
		s.renderError(w, r, http.StatusNotFound, "That family does not exist.")
		return
	}

	var childID int64
	if id := formID(r, "person_id"); id > 0 {
		if _, err := q.PersonByID(ctx, id); err != nil {
			s.renderError(w, r, http.StatusNotFound, "That person does not exist.")
			return
		}
		childID = id
	} else {
		child, ok := parentFromForm(r, "child")
		if !ok {
			s.renderError(w, r, http.StatusUnprocessableEntity, "Pick an existing child or fill in the fields.")
			return
		}
		surnameFallback(ctx, q, famID, &child)
		if child.ID, err = q.InsertPerson(ctx, &child); err != nil {
			s.renderError(w, r, http.StatusUnprocessableEntity, "Child could not be saved: "+err.Error())
			return
		}
		childID = child.ID
	}

	rel := formVal(r, "relationship")
	if _, err := q.AddChildLink(ctx, famID, childID, rel); err != nil {
		s.renderError(w, r, http.StatusUnprocessableEntity, "Could not link child: "+err.Error())
		return
	}
	_ = q.InsertAudit(ctx, s.St.DB, userIDof(r), "add-child", "family", &famID, "")
	redirectTo := "/family/" + strconv.FormatInt(famID, 10) + "/edit"
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// surnameFallback pre-fills a new child's surname with the first partner's.
func surnameFallback(ctx context.Context, q *store.Queries, famID int64, child *models.Person) {
	if child.Surname != "" {
		return
	}
	f, err := q.FamilyByID(ctx, famID)
	if err != nil {
		return
	}
	for _, pid := range []*int64{f.Partner1ID, f.Partner2ID} {
		if pid == nil {
			continue
		}
		if p, err := q.PersonByID(ctx, *pid); err == nil && p.Surname != "" {
			child.Surname = p.Surname
			return
		}
	}
}

// familyRemoveChild removes the child link (person itself stays).
func (s *Server) familyRemoveChild(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	famID, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown family.")
		return
	}
	personID := formID(r, "person_id")
	if personID == 0 {
		s.renderError(w, r, http.StatusBadRequest, "person_id required.")
		return
	}
	q := qOf(s, r)
	if err := q.RemoveChildLink(ctx, famID, personID); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = q.InsertAudit(ctx, s.St.DB, userIDof(r), "remove-child", "family", &famID, "")
	http.Redirect(w, r, "/family/"+strconv.FormatInt(famID, 10)+"/edit", http.StatusFound)
}

func statusOptions(current string) [][2]string {
	all := []string{models.FamilyMarried, models.FamilyUnmarried, models.FamilyDivorced,
		models.FamilySeparated, models.FamilyOther}
	out := [][2]string{}
	for _, s := range all {
		out = append(out, [2]string{s, s})
	}
	return out
}
