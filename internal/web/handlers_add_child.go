package web

import (
	"context"
	"net/http"
	"strconv"

	"genealogy-tree/internal/models"
	"genealogy-tree/internal/store"
)

// familyOption is a target-family choice for the add-child form.
type familyOption struct {
	ID    int64
	Label string
}

// targetFamilies returns the families a new child can be linked into:
//   - child mode: the families where person is a partner (their own families)
//   - sibling mode: the families where person is a child (their parents)
func (s *Server) targetFamilies(ctx context.Context, q *store.Queries, person models.Person, siblingMode bool) []familyOption {
	var fams []models.Family
	if siblingMode {
		fams, _ = q.FamiliesOfChild(ctx, person.ID)
	} else {
		fams, _ = q.FamiliesOfPerson(ctx, person.ID)
	}
	out := make([]familyOption, 0, len(fams))
	for _, f := range fams {
		out = append(out, familyOption{ID: f.ID, Label: s.familyLabel(ctx, q, f)})
	}
	return out
}

// familyLabel describes a family by its partner names.
func (s *Server) familyLabel(ctx context.Context, q *store.Queries, f models.Family) string {
	var parts []string
	for _, pid := range []*int64{f.Partner1ID, f.Partner2ID} {
		if pid == nil {
			continue
		}
		if p, err := q.PersonByID(ctx, *pid); err == nil {
			parts = append(parts, p.DisplayName())
		}
	}
	switch len(parts) {
	case 0:
		return "(unnamed family)"
	case 1:
		return parts[0]
	default:
		return parts[0] + " & " + parts[1]
	}
}

// addChildPage handles GET /person/{id}/child/new.
func (s *Server) addChildPage(w http.ResponseWriter, r *http.Request) {
	s.renderAddChildForm(w, r, false, "")
}

// addSiblingPage handles GET /person/{id}/sibling/new.
func (s *Server) addSiblingPage(w http.ResponseWriter, r *http.Request) {
	person, err := s.addChildPerson(w, r)
	if err != nil {
		return
	}
	if len(s.targetFamilies(r.Context(), qOf(s, r), person, true)) == 0 {
		s.renderError(w, r, http.StatusBadRequest, s.tr(r, "This person has no recorded parents, so a sibling cannot be added here."))
		return
	}
	s.renderAddChildForm(w, r, true, "")
}

// addChildPerson loads the context person from the {id} path.
func (s *Server) addChildPerson(w http.ResponseWriter, r *http.Request) (models.Person, error) {
	id, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, s.tr(r, "Unknown person."))
		return models.Person{}, err
	}
	person, err := qOf(s, r).PersonByID(r.Context(), id)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, s.tr(r, "That person does not exist."))
		return models.Person{}, err
	}
	return person, nil
}

// renderAddChildForm renders the "add child / sibling" form.
func (s *Server) renderAddChildForm(w http.ResponseWriter, r *http.Request, siblingMode bool, errMsg string) {
	person, err := s.addChildPerson(w, r)
	if err != nil {
		return
	}
	title := s.tr(r, "Add child of %s", person.DisplayName())
	action := "/person/" + strconv.FormatInt(person.ID, 10) + "/child/new"
	if siblingMode {
		title = s.tr(r, "Add sibling of %s", person.DisplayName())
		action = "/person/" + strconv.FormatInt(person.ID, 10) + "/sibling/new"
	}

	q := qOf(s, r)
	ctx := r.Context()

	// Determine the "current couple" for pre-filling the parent fields.
	fatherGiven, fatherSurname, fatherID, fatherBirth := "", "", int64(0), ""
	motherGiven, motherSurname, motherID, motherBirth := "", "", int64(0), ""

	// Prefer an existing family of the context person (their own family).
	fams := s.targetFamilies(ctx, q, person, siblingMode)
	// For child mode targetFamilies are person's own families; for sibling mode
	// they are parents' families. Only pre-fill parents in child mode.
	if !siblingMode && len(fams) > 0 {
		if fam, err := q.FamilyByID(ctx, fams[0].ID); err == nil {
			var p1, p2 *models.Person
			if fam.Partner1ID != nil {
				if pp, err := q.PersonByID(ctx, *fam.Partner1ID); err == nil {
					pp2 := pp
					p1 = &pp2
				}
			}
			if fam.Partner2ID != nil {
				if pp, err := q.PersonByID(ctx, *fam.Partner2ID); err == nil {
					pp2 := pp
					p2 = &pp2
				}
			}
			// Assign father/mother by sex, falling back to slot order.
			assign := func(pp models.Person) {
				if pp.Sex == models.SexFemale {
					motherGiven, motherSurname, motherID = pp.GivenNames, pp.Surname, pp.ID
					motherBirth = pp.BirthDate
				} else {
					fatherGiven, fatherSurname, fatherID = pp.GivenNames, pp.Surname, pp.ID
					fatherBirth = pp.BirthDate
				}
			}
			if p1 != nil {
				assign(*p1)
			}
			if p2 != nil {
				assign(*p2)
			}
		}
	} else if !siblingMode {
		// No existing family: pre-fill the context person as one parent.
		if person.Sex == models.SexFemale {
			motherGiven, motherSurname, motherID = person.GivenNames, person.Surname, person.ID
			motherBirth = person.BirthDate
		} else {
			fatherGiven, fatherSurname, fatherID = person.GivenNames, person.Surname, person.ID
			fatherBirth = person.BirthDate
		}
	}

	data := s.baseData(r, title)
	data["Title"] = title
	data["Action"] = action
	data["Person"] = person
	data["Families"] = fams
	data["SiblingMode"] = siblingMode
	data["People"] = s.pickerPeople(r.Context())
	data["FatherGiven"] = fatherGiven
	data["FatherSurname"] = fatherSurname
	data["FatherID"] = fatherID
	data["FatherBirth"] = fatherBirth
	data["MotherGiven"] = motherGiven
	data["MotherSurname"] = motherSurname
	data["MotherID"] = motherID
	data["MotherBirth"] = motherBirth
	if errMsg != "" {
		data["Error"] = errMsg
	}
	s.render(w, "add_child.html", http.StatusOK, data)
}

// addChildCreate handles POST /person/{id}/child/new.
func (s *Server) addChildCreate(w http.ResponseWriter, r *http.Request) {
	s.addChildSave(w, r, false)
}

// addSiblingCreate handles POST /person/{id}/sibling/new.
func (s *Server) addSiblingCreate(w http.ResponseWriter, r *http.Request) {
	s.addChildSave(w, r, true)
}

// addChildSave links an existing person or creates a new one as a child of
// the chosen family (or of a freshly-created family for the context person).
func (s *Server) addChildSave(w http.ResponseWriter, r *http.Request, siblingMode bool) {
	ctx := r.Context()
	q := qOf(s, r)

	personID := formID(r, "person")
	person, err := q.PersonByID(ctx, personID)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, s.tr(r, "That person does not exist."))
		return
	}

	familyID := formID(r, "family_id")

	// Optional: parents may be edited/created from the form fields.
	fatherIDResolved, _ := resolveParent(ctx, q, r, "father")
	motherIDResolved, _ := resolveParent(ctx, q, r, "mother")

	var famID int64
	if familyID > 0 {
		if _, err := q.FamilyByID(ctx, familyID); err != nil {
			s.renderError(w, r, http.StatusNotFound, s.tr(r, "That family does not exist."))
			return
		}
		famID = familyID
		// Apply any parent changes submitted with the form.
		if fatherIDResolved > 0 || motherIDResolved > 0 {
			fam, err := q.FamilyByID(ctx, famID)
			if err != nil {
				s.renderError(w, r, http.StatusNotFound, s.tr(r, "That family does not exist."))
				return
			}
			if fatherIDResolved > 0 {
				fam.Partner1ID = &fatherIDResolved
			}
			if motherIDResolved > 0 {
				fam.Partner2ID = &motherIDResolved
			}
			if err := q.UpdateFamily(ctx, &fam); err != nil {
				s.renderError(w, r, http.StatusUnprocessableEntity, "Could not save family: "+err.Error())
				return
			}
		}
	} else {
		// Child mode with no existing family: create one with the submitted
		// parents, falling back to the context person as the sole parent.
		fam := models.Family{Status: models.FamilyMarried}
		if fatherIDResolved > 0 {
			fam.Partner1ID = &fatherIDResolved
		} else {
			fam.Partner1ID = &person.ID
		}
		if motherIDResolved > 0 {
			fam.Partner2ID = &motherIDResolved
		}
		if famID, err = q.InsertFamily(ctx, &fam); err != nil {
			s.renderError(w, r, http.StatusUnprocessableEntity, "Could not create family: "+err.Error())
			return
		}
	}

	var childID int64
	if id := formID(r, "person_id"); id > 0 {
		if _, err := q.PersonByID(ctx, id); err != nil {
			s.renderError(w, r, http.StatusNotFound, s.tr(r, "The chosen person does not exist."))
			return
		}
		childID = id
	} else {
		child := models.Person{
			GivenNames: formVal(r, "child_given"),
			Surname:    formVal(r, "child_surname"),
			BirthDate:  formVal(r, "child_birth"),
		}
		if sx := formVal(r, "child_sex"); sx == models.SexMale || sx == models.SexFemale || sx == models.SexOther {
			child.Sex = sx
		} else {
			child.Sex = models.SexUnknown
		}
		child.BirthYear = formYear(child.BirthDate)
		if child.GivenNames == "" && child.Surname == "" {
			s.renderAddChildForm(w, r, siblingMode, "Pick an existing person or fill in the new-person fields.")
			return
		}
		surnameFallback(ctx, q, famID, &child)
		if child.ID, err = q.InsertPerson(ctx, &child); err != nil {
			s.renderError(w, r, http.StatusUnprocessableEntity, "Could not save person: "+err.Error())
			return
		}
		childID = child.ID
	}

	rel := formVal(r, "relationship")
	if rel == "" {
		rel = models.ChildBirth
	}
	if _, err := q.AddChildLink(ctx, famID, childID, rel); err != nil {
		s.renderError(w, r, http.StatusUnprocessableEntity, "Could not link child: "+err.Error())
		return
	}
	_ = q.InsertAudit(ctx, s.St.DB, userIDof(r), "add-child", "family", &famID, person.DisplayName())

	http.Redirect(w, r, "/person/"+strconv.FormatInt(childID, 10), http.StatusFound)
}
