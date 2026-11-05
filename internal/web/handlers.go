package web

import (
	"net/http"
	"net/url"
	"strconv"

	"genealogy-tree/internal/auth"
	"genealogy-tree/internal/models"
)

// -- login / logout ---------------------------------------------------------

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	data := s.baseData(r, "Log in")
	// Already logged in? Straight to the dashboard.
	if _, ok := auth.FromContext(r.Context()); ok {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	// Fresh deployment? The login page doesn't exist until an admin does.
	if n, _ := qOf(s, r).CountUsers(r.Context()); n == 0 {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	if next := r.URL.Query().Get("next"); next != "" {
		data["Next"] = next
	}
	s.render(w, "login.html", http.StatusOK, data)
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	username := r.PostFormValue("username")
	password := r.PostFormValue("password")
	next := r.PostFormValue("next")

	token, err := s.Auth.Login(r.Context(), username, password)
	if err != nil {
		data := s.baseData(r, "Log in")
		data["Error"] = "Wrong username or password."
		data["Next"] = next
		s.render(w, "login.html", http.StatusUnauthorized, data)
		return
	}

	s.Auth.SetSessionCookie(w, token)
	target := safeNext(next)
	http.Redirect(w, r, target, http.StatusFound)
}

// safeNext only allows same-site absolute paths (open-redirect protection).
func safeNext(next string) string {
	if next == "" {
		return "/"
	}
	if len(next) == 0 || next[0] != '/' {
		return "/"
	}
	if len(next) >= 2 && (next[1] == '/' || next[1] == '\\') {
		return "/"
	}
	return next
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.Auth.Logout(r)
	s.Auth.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// -- dashboard ---------------------------------------------------------------

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.St.Queries()

	data := s.baseData(r, "Home")
	data["PeopleCount"], _ = q.CountPeople(ctx)
	data["FamiliesCount"], _ = q.CountFamilies(ctx)
	data["MediaCount"], _ = q.CountMedia(ctx)
	data["SourcesCount"], _ = q.CountSources(ctx)

	recent, _, _ := q.SearchPeople(ctx, "", 6, 0)
	data["Recent"] = decoratePeople(ctx, q, recent)

	s.render(w, "dashboard.html", http.StatusOK, data)
}

// -- people list ---------------------------------------------------------------

const pageSize = 50

func (s *Server) peopleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.St.Queries()

	query := r.URL.Query().Get("q")
	page := clampInt(atoiDefault(r.URL.Query().Get("page"), 1), 1, 100000)

	people, total, _ := q.SearchPeople(ctx, query, pageSize, (page-1)*pageSize)
	pages := (int(total) + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}

	data := s.baseData(r, "People")
	data["Query"] = query
	data["QueryP"] = url.QueryEscape(query)
	data["People"] = decoratePeople(ctx, q, people)
	data["Total"] = total
	data["Page"] = page
	data["Pages"] = pages
	data["HasPrev"] = page > 1
	data["HasNext"] = page < pages
	data["PrevPage"] = page - 1
	data["NextPage"] = page + 1
	s.render(w, "people.html", http.StatusOK, data)
}

// -- person page --------------------------------------------------------------

func (s *Server) personPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.St.Queries()

	id, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown person.")
		return
	}
	person, err := q.PersonByID(ctx, id)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That person does not exist (or was removed).")
		return
	}

	data := s.baseData(r, person.DisplayName())
	data["Person"] = person
	data["SexLabel"] = sexLabel(person)
	data["Events"], _ = q.ListEventsForPerson(ctx, person.ID)
	data["Citations"], _ = q.ListCitationsForPerson(ctx, person.ID)
	data["Marriages"] = s.marriagesOf(ctx, person)
	parents, siblings := s.parentsAndSiblingsOf(ctx, person)
	data["Parents"] = parents
	data["Siblings"] = siblings
	data["ProfilePhotoID"], _ = q.ProfilePhotoID(ctx, person.ID)
	media, _ := q.ListMediaForPerson(ctx, person.ID)
	data["Media"] = media

	s.render(w, "person.html", http.StatusOK, data)
}

// -- family page --------------------------------------------------------------

func (s *Server) familyPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.St.Queries()

	famID, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown family.")
		return
	}
	family, err := q.FamilyByID(ctx, famID)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "That family does not exist (or was removed).")
		return
	}

	// Partners as people, for the template.
	var partners []models.Person
	for _, pid := range []*int64{family.Partner1ID, family.Partner2ID} {
		if pid != nil {
			if p, err := q.PersonByID(ctx, *pid); err == nil {
				partners = append(partners, p)
			}
		}
	}

	children, _ := q.ChildrenOfFamily(ctx, family.ID)
	var childView []map[string]any
	for _, c := range children {
		rel := childRelationship(ctx, q, family.ID, c.ID)
		childView = append(childView, map[string]any{"Person": c, "Adopted": rel != models.ChildBirth})
	}

	data := s.baseData(r, "Family")
	data["Family"] = family
	data["Partners"] = partners
	data["Children"] = childView
	data["Events"], _ = q.ListEventsForFamily(ctx, family.ID)
	data["Citations"], _ = q.ListCitationsForFamily(ctx, family.ID)
	s.render(w, "family.html", http.StatusOK, data)
}

// -- media serving -------------------------------------------------------------

func (s *Server) mediaThumb(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := s.St.Queries().MediaData(r.Context(), id)
	if err != nil || len(d.Thumb) == 0 {
		http.Redirect(w, r, "/static/placeholder.svg", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", contentTypeOrDefault(d.Mime, "image/jpeg"))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write(d.Thumb)
}

func (s *Server) mediaFull(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := s.St.Queries().MediaData(r.Context(), id)
	if err != nil || len(d.Data) == 0 {
		s.renderError(w, r, http.StatusNotFound,
			"This photo is a placeholder from the GEDCOM import — the original media file ships in a separate archive and must be uploaded manually.")
		return
	}
	w.Header().Set("Content-Type", contentTypeOrDefault(d.Mime, "application/octet-stream"))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write(d.Data)
}

// -- small helpers -------------------------------------------------------------

func sexLabel(p models.Person) string {
	switch p.Sex {
	case models.SexMale:
		return "male"
	case models.SexFemale:
		return "female"
	case models.SexOther:
		return "other"
	default:
		return "sex unknown"
	}
}

func atoiDefault(s string, fallback int) int {
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func nonNegative(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func contentTypeOrDefault(mime, fallback string) string {
	if mime == "" {
		mime = fallback
	}
	return mime
}
