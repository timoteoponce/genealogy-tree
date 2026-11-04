package web

import (
	"net/http"
	"strconv"
	"strings"

	"genealogy-tree/internal/gedcom"
	"genealogy-tree/internal/models"
)

// formVal fetches a trimmed form value ("mvc" style forms: every POST form
// uses plain content-type application/x-www-form-urlencoded).
func formVal(r *http.Request, key string) string {
	return strings.TrimSpace(r.PostFormValue(key))
}

// formYear parses a free-text year or GEDCOM date prefix into *int64.
// Empty input -> nil.
func formYear(raw string) *int64 {
	if raw == "" {
		return nil
	}
	if y := gedcom.ParseYear(raw); y > 0 {
		return &y
	}
	return nil
}

// personFromForm builds a Person from the standard person form fields.
func personFromForm(r *http.Request, id int64) (models.Person, string) {
	p := models.Person{
		ID:          id,
		GivenNames:  formVal(r, "given_names"),
		Surname:     formVal(r, "surname"),
		MarriedName: formVal(r, "married_name"),
		Nickname:    formVal(r, "nickname"),
		BirthDate:   formVal(r, "birth_date"),
		BirthPlace:  formVal(r, "birth_place"),
		DeathDate:   formVal(r, "death_date"),
		DeathPlace:  formVal(r, "death_place"),
		Notes:       strings.ReplaceAll(r.PostFormValue("notes"), "\r", ""),
	}
	p.BirthYear = formYear(p.BirthDate)
	p.DeathYear = formYear(p.DeathDate)

	switch s := formVal(r, "sex"); s {
	case models.SexMale, models.SexFemale, models.SexOther, models.SexUnknown:
		p.Sex = s
	default:
		p.Sex = models.SexUnknown
	}

	if p.GivenNames == "" && p.Surname == "" && p.MarriedName == "" {
		return p, "Give at least a first name or a surname."
	}
	return p, ""
}

// familyFromForm builds a Family from the family form fields.
func familyFromForm(r *http.Request, id int64) (models.Family, string) {
	f := models.Family{
		ID:            id,
		MarriageDate:  formVal(r, "marriage_date"),
		MarriagePlace: formVal(r, "marriage_place"),
		Notes:         strings.ReplaceAll(r.PostFormValue("notes"), "\r", ""),
	}
	f.MarriageYear = formYear(f.MarriageDate)

	switch s := formVal(r, "status"); s {
	case models.FamilyMarried, models.FamilyUnmarried, models.FamilyDivorced,
		models.FamilySeparated, models.FamilyOther:
		f.Status = s
	default:
		f.Status = models.FamilyMarried
	}
	return f, ""
}

// formID parses a positive int64 form field; 0 when absent/invalid.
func formID(r *http.Request, key string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(r.PostFormValue(key)), 10, 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}
