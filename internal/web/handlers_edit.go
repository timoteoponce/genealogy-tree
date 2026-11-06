package web

import (
	"context"
	"net/http"
	"strconv"

	"genealogy-tree/internal/auth"
	"genealogy-tree/internal/models"
	"genealogy-tree/internal/store"
)

// userIDof returns the current user id (0 for system actions).
func userIDof(r *http.Request) *int64 {
	if sess, ok := auth.FromContext(r.Context()); ok {
		id := sess.User.ID
		return &id
	}
	return nil
}

func qOf(s *Server, r *http.Request) *store.Queries {
	return s.St.Queries()
}

// -- person create ----------------------------------------------------------

// personNewPage handles GET /person/new?[relationship=father|mother|spouse]
// with either ?of=<personID> (spouse flow) or ?child=<personID> (parent
// flow, via /family/add alias) or nothing (plain new person).
func (s *Server) personNewPage(w http.ResponseWriter, r *http.Request) {
	data := s.baseData(r, "Add person")
	rel := r.URL.Query().Get("relationship")
	ofID, _ := strconv.ParseInt(r.URL.Query().Get("of"), 10, 64)

	data["Mode"] = "new"
	data["Action"] = "/person/new"
	data["Rel"] = rel
	data["RelLabel"] = relationshipLabel(rel)
	data["Sex"] = models.SexUnknown
	data["SexOptions"] = sexOptions(models.SexUnknown)
	if ofID > 0 {
		if p, err := qOf(s, r).PersonByID(r.Context(), ofID); err == nil {
			data["Of"] = p
			data["OfID"] = ofID
		}
	}
	s.render(w, "person_form.html", http.StatusOK, data)
}

// personCreate handles POST /person/new with relationship context:
//   - relationship=spouse&of=<PID>  → create new family with both partners
//   - plain                          → just the person
func (s *Server) personCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	p, formErr := personFromForm(r, 0)
	rel := formVal(r, "relationship")
	ofID := formID(r, "of")

	if formErr != "" {
		s.personFormError(w, r, "new", "/person/new", rel, ofID, formErr, p)
		return
	}

	tx, err := s.St.Begin(ctx)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Database unavailable.")
		return
	}
	defer tx.Rollback()
	tq := store.NewQueries(tx)

	var partner models.Person
	withPartner := rel == "spouse" && ofID > 0
	if withPartner {
		var err error
		partner, err = tq.PersonByID(ctx, ofID)
		if err != nil {
			s.personFormError(w, r, "new", "/person/new", rel, ofID, "The referenced person no longer exists.", p)
			return
		}
	}

	newID, err := tq.InsertPerson(ctx, &p)
	if err != nil {
		s.personFormError(w, r, "new", "/person/new", rel, ofID, "Could not save: "+err.Error(), p)
		return
	}
	p.ID = newID

	var famID int64
	if withPartner {
		fam := models.Family{Status: models.FamilyMarried}
		if partner.Sex == models.SexFemale && p.Sex != models.SexFemale {
			fam.Partner2ID = &p.ID
			fam.Partner1ID = &partner.ID
		} else {
			fam.Partner1ID = &p.ID
			fam.Partner2ID = &partner.ID
		}
		if famID, err = tq.InsertFamily(ctx, &fam); err != nil {
			s.personFormError(w, r, "new", "/person/new", rel, ofID, "Person saved but family failed: "+err.Error(), p)
			return
		}
	}

	if err := tq.InsertAudit(ctx, tx, userIDof(r), "create", "person", &newID, p.DisplayName()); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Audit write failed.")
		return
	}
	if err := tx.Commit(); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not save changes.")
		return
	}

	if famID > 0 {
		http.Redirect(w, r, "/family/"+strconv.FormatInt(famID, 10), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/person/"+strconv.FormatInt(newID, 10), http.StatusFound)
}

// -- person edit / delete ----------------------------------------------------

func (s *Server) personEditPage(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown person.")
		return
	}
	p, err := qOf(s, r).PersonByID(r.Context(), id)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That person does not exist.")
		return
	}

	data := s.baseData(r, "Edit "+p.DisplayName())
	data["Mode"] = "edit"
	data["Action"] = "/person/" + strconv.FormatInt(id, 10) + "/edit"
	data["P"] = p
	data["Sex"] = p.Sex
	data["SexOptions"] = sexOptions(p.Sex)
	s.render(w, "person_form.html", http.StatusOK, data)
}

// personFormError re-renders the person form with an error banner.
func (s *Server) personFormError(w http.ResponseWriter, r *http.Request, mode, action, rel string, ofID int64, msg string, p models.Person) {
	data := s.baseData(r, "Edit person")
	data["Mode"] = mode
	data["Action"] = action
	data["Rel"] = rel
	data["RelLabel"] = relationshipLabel(rel)
	data["OfID"] = ofID
	data["Error"] = msg
	data["P"] = p
	data["Sex"] = p.Sex
	data["SexOptions"] = sexOptions(p.Sex)
	s.render(w, "person_form.html", http.StatusUnprocessableEntity, data)
}

func (s *Server) personUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown person.")
		return
	}
	p, formErr := personFromForm(r, id)
	if formErr != "" {
		s.personFormError(w, r, "edit", "/person/"+strconv.FormatInt(id, 10)+"/edit", "", 0, formErr, p)
		return
	}
	if err := qOf(s, r).UpdatePerson(ctx, &p); err != nil {
		s.personFormError(w, r, "edit", "/person/"+strconv.FormatInt(id, 10)+"/edit", "", 0, "Could not save: "+err.Error(), p)
		return
	}
	_ = qOf(s, r).InsertAudit(ctx, s.St.DB, userIDof(r), "update", "person", &id, p.DisplayName())
	http.Redirect(w, r, "/person/"+strconv.FormatInt(id, 10), http.StatusFound)
}

func (s *Server) personDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown person.")
		return
	}
	if formVal(r, "confirm") != "yes" {
		s.renderError(w, r, http.StatusBadRequest, "Deletion needs the confirm box ticked.")
		return
	}
	q := qOf(s, r)
	p, err := q.PersonByID(ctx, id)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That person does not exist.")
		return
	}
	if err := q.DeletePerson(ctx, id); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Delete failed: "+err.Error())
		return
	}
	if _, err := q.PruneEmptyFamilies(ctx); err != nil {
		// Non-fatal: tree already consistent; families become visible empty.
		_ = err
	}
	_ = q.InsertAudit(ctx, s.St.DB, userIDof(r), "delete", "person", &id, p.DisplayName())
	http.Redirect(w, r, "/people", http.StatusFound)
}

// -- "add parents" (create family + parents) ---------------------------------

// familyAddParents handles GET/POST /family/add?child=<PID> to add a parent.
// When the child already has a parent-family with a free slot, the form
// attaches the new parent into that family; otherwise it creates a new
// family with father/mother and links the child.
func (s *Server) familyAddParents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := qOf(s, r)

	if r.Method == http.MethodGet {
		child, err := s.personOf(w, r, "child")
		if err != nil {
			return
		}
		data := s.baseData(r, "Add parents of "+child.DisplayName())
		data["Child"] = child
		data["ChildID"] = child.ID
		data["People"] = s.pickerPeople(ctx)
		if famID, slot, role, existing, ok := s.parentFamilyToFill(ctx, q, child); ok {
			data["AttachFamilyID"] = famID
			data["AttachSlot"] = slot
			data["AttachRole"] = role
			data["ExistingParent"] = existing
		}
		s.render(w, "add_parents.html", http.StatusOK, data)
		return
	}

	childID := formID(r, "child")
	child, err := q.PersonByID(ctx, childID)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That child does not exist.")
		return
	}

	// Attach mode: fill the free slot of an existing parent family.
	if famID := formID(r, "family_id"); famID > 0 {
		role := formVal(r, "role")
		if role != "father" && role != "mother" {
			role = "father"
		}
		parentID, err := resolveParent(ctx, q, r, role)
		if err != nil {
			s.renderError(w, r, http.StatusUnprocessableEntity, "Could not save parent: "+err.Error())
			return
		}
		if parentID == 0 {
			s.renderAddParentsError(w, r, child, "Enter or pick a parent.")
			return
		}
		fam, err := q.FamilyByID(ctx, famID)
		if err != nil {
			s.renderError(w, r, http.StatusNotFound, "That family does not exist.")
			return
		}
		if role == "father" {
			fam.Partner1ID = &parentID
		} else {
			fam.Partner2ID = &parentID
		}
		if err := q.UpdateFamily(ctx, &fam); err != nil {
			s.renderError(w, r, http.StatusUnprocessableEntity, "Could not save family: "+err.Error())
			return
		}
		_ = q.InsertAudit(ctx, s.St.DB, userIDof(r), "set-parent", "family", &famID, child.DisplayName())
		http.Redirect(w, r, "/family/"+strconv.FormatInt(famID, 10), http.StatusFound)
		return
	}

	// Create mode: a new family with father/mother, then link the child.
	tx, err := s.St.Begin(ctx)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Database unavailable.")
		return
	}
	defer tx.Rollback()
	tq := store.NewQueries(tx)

	// A parent can be an existing person (picked by name) or a new one.
	fatherID, err := resolveParent(ctx, tq, r, "father")
	if err != nil {
		s.renderError(w, r, http.StatusUnprocessableEntity, "Father: "+err.Error())
		return
	}
	motherID, err := resolveParent(ctx, tq, r, "mother")
	if err != nil {
		s.renderError(w, r, http.StatusUnprocessableEntity, "Mother: "+err.Error())
		return
	}
	if fatherID == 0 && motherID == 0 {
		tx.Rollback()
		s.renderAddParentsError(w, r, child, "Enter or pick at least one parent.")
		return
	}

	fam := models.Family{Status: models.FamilyMarried}
	if fatherID > 0 {
		fam.Partner1ID = &fatherID
	}
	if motherID > 0 {
		fam.Partner2ID = &motherID
	}
	fam.MarriageDate = formVal(r, "marriage_date")
	fam.MarriagePlace = formVal(r, "marriage_place")
	fam.MarriageYear = formYear(fam.MarriageDate)
	rel := formVal(r, "relationship")
	if rel == "" {
		rel = models.ChildBirth
	}

	famID, err := tq.InsertFamily(ctx, &fam)
	if err != nil {
		tx.Rollback()
		s.renderError(w, r, http.StatusUnprocessableEntity, "Family could not be saved: "+err.Error())
		return
	}
	if _, err := tq.AddChildLink(ctx, famID, child.ID, rel); err != nil {
		tx.Rollback()
		s.renderError(w, r, http.StatusUnprocessableEntity, "Linking child failed: "+err.Error())
		return
	}
	if err := tq.InsertAudit(ctx, tx, userIDof(r), "add-parents", "family", &famID, child.DisplayName()); err != nil {
		tx.Rollback()
		s.renderError(w, r, http.StatusInternalServerError, "Audit write failed.")
		return
	}
	if err := tx.Commit(); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not save changes.")
		return
	}
	http.Redirect(w, r, "/family/"+strconv.FormatInt(famID, 10), http.StatusFound)
}

// resolveParent returns the id of a parent, either an existing person picked
// by name (<suffix>_person) or a newly-inserted one built from <suffix>_*
// fields. Returns 0 when neither was given.
func resolveParent(ctx context.Context, tq *store.Queries, r *http.Request, suffix string) (int64, error) {
	if id := formID(r, suffix+"_person"); id > 0 {
		if _, err := tq.PersonByID(ctx, id); err != nil {
			return 0, err
		}
		return id, nil
	}
	p, ok := parentFromForm(r, suffix)
	if !ok {
		return 0, nil
	}
	return tq.InsertPerson(ctx, &p)
}

// parentFamilyToFill returns the first parent family of a child that still
// has a free partner slot, so "add parent" can attach into it instead of
// creating a duplicate family.
func (s *Server) parentFamilyToFill(ctx context.Context, q *store.Queries, child models.Person) (int64, int, string, models.Person, bool) {
	fams, _ := q.FamiliesOfChild(ctx, child.ID)
	for _, f := range fams {
		var slot int
		var role string
		var other *int64
		switch {
		case f.Partner1ID == nil:
			slot, role, other = 1, "father", f.Partner2ID
		case f.Partner2ID == nil:
			slot, role, other = 2, "mother", f.Partner1ID
		}
		if slot == 0 {
			continue
		}
		var existing models.Person
		if other != nil {
			existing, _ = q.PersonByID(ctx, *other)
		}
		return f.ID, slot, role, existing, true
	}
	return 0, 0, "", models.Person{}, false
}

// renderAddParentsError re-renders the add-parents form with an error and
// the same attach/create context the GET produced.
func (s *Server) renderAddParentsError(w http.ResponseWriter, r *http.Request, child models.Person, msg string) {
	ctx := r.Context()
	q := qOf(s, r)
	data := s.baseData(r, "Add parents")
	data["Child"] = child
	data["ChildID"] = child.ID
	data["People"] = s.pickerPeople(ctx)
	data["Error"] = msg
	if famID, slot, role, existing, ok := s.parentFamilyToFill(ctx, q, child); ok {
		data["AttachFamilyID"] = famID
		data["AttachSlot"] = slot
		data["AttachRole"] = role
		data["ExistingParent"] = existing
	}
	s.render(w, "add_parents.html", http.StatusUnprocessableEntity, data)
}

// parentFromForm builds a parent person (suffix = "father"|"mother").
func parentFromForm(r *http.Request, suffix string) (models.Person, bool) {
	p := models.Person{
		GivenNames: formVal(r, suffix+"_given"),
		Surname:    formVal(r, suffix+"_surname"),
		BirthDate:  formVal(r, suffix+"_birth"),
		Sex:        models.SexUnknown,
	}
	if suffix == "father" {
		p.Sex = models.SexMale
	}
	if suffix == "mother" {
		p.Sex = models.SexFemale
	}
	p.BirthYear = formYear(p.BirthDate)
	if p.GivenNames == "" && p.Surname == "" {
		return p, false
	}
	return p, true
}

// personOf fetches a person by query param (used by add-relative flows).
func (s *Server) personOf(w http.ResponseWriter, r *http.Request, param string) (models.Person, error) {
	id, err := strconv.ParseInt(r.URL.Query().Get(param), 10, 64)
	if err != nil || id <= 0 {
		s.renderError(w, r, http.StatusBadRequest, "Expected ?"+param+"=<person id>.")
		return models.Person{}, err
	}
	p, err := qOf(s, r).PersonByID(r.Context(), id)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That person does not exist.")
		return models.Person{}, err
	}
	return p, nil
}

// -- spouse flow -------------------------------------------------------------

// familyNew handles GET/POST /family/new?partner=<PID> to add a spouse.
func (s *Server) familyNew(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := qOf(s, r)

	if r.Method == http.MethodGet {
		partner, err := s.personOf(w, r, "partner")
		if err != nil {
			return
		}
		data := s.baseData(r, "Add spouse of "+partner.DisplayName())
		data["Partner"] = partner
		data["People"] = s.pickerPeople(ctx)
		s.render(w, "add_spouse.html", http.StatusOK, data)
		return
	}

	partnerID := formID(r, "partner")
	partner, err := q.PersonByID(ctx, partnerID)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That person does not exist.")
		return
	}

	// Either an existing person id or new-spouse fields.
	var spouseID int64
	if id := formID(r, "spouse_person"); id > 0 {
		if _, err := q.PersonByID(ctx, id); err != nil {
			s.renderError(w, r, http.StatusNotFound, "The chosen spouse does not exist.")
			return
		}
		if id == partnerID {
			s.renderError(w, r, http.StatusUnprocessableEntity, "Cannot marry a person to themselves.")
			return
		}
		spouseID = id
	} else {
		spouse, ok := parentFromForm(r, "spouse")
		if !ok {
			data := s.baseData(r, "Add spouse")
			data["Partner"] = partner
			data["People"] = s.pickerPeople(ctx)
			data["Error"] = "Pick an existing person or fill in the new spouse fields."
			s.render(w, "add_spouse.html", http.StatusUnprocessableEntity, data)
			return
		}
		if sx := formVal(r, "spouse_sex"); sx == models.SexMale || sx == models.SexFemale {
			spouse.Sex = sx
		}
		spouse.ID, err = q.InsertPerson(ctx, &spouse)
		if err != nil {
			s.renderError(w, r, http.StatusUnprocessableEntity, "Spouse could not be saved: "+err.Error())
			return
		}
		spouseID = spouse.ID
	}

	fam := models.Family{Status: models.FamilyMarried}
	if partner.Sex == models.SexFemale && anySex(ctx, q, spouseID) == models.SexMale {
		fam.Partner1ID = &spouseID
		fam.Partner2ID = &partner.ID
	} else {
		fam.Partner1ID = &partner.ID
		fam.Partner2ID = &spouseID
	}
	fam.MarriageDate = formVal(r, "marriage_date")
	fam.MarriagePlace = formVal(r, "marriage_place")
	fam.MarriageYear = formYear(fam.MarriageDate)

	famID, err := q.InsertFamily(ctx, &fam)
	if err != nil {
		s.renderError(w, r, http.StatusUnprocessableEntity, "Family could not be saved: "+err.Error())
		return
	}
	_ = q.InsertAudit(ctx, s.St.DB, userIDof(r), "add-spouse", "family", &famID, partner.DisplayName())
	http.Redirect(w, r, "/family/"+strconv.FormatInt(famID, 10), http.StatusFound)
}

// anySex looks up a person's sex (kept separate to avoid nil handling noise).
func anySex(ctx context.Context, q *store.Queries, id int64) string {
	if p, err := q.PersonByID(ctx, id); err == nil {
		return p.Sex
	}
	return models.SexUnknown
}

func relationshipLabel(rel string) string {
	switch rel {
	case "spouse":
		return "Add spouse"
	case "child":
		return "Add child"
	case "father", "mother":
		return "Add parent"
	default:
		return "Add person"
	}
}

func sexOptions(current string) [][2]string {
	return [][2]string{
		{models.SexMale, "Male"},
		{models.SexFemale, "Female"},
		{models.SexUnknown, "Unknown"},
		{models.SexOther, "Other"},
	}
}
