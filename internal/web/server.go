// Package web serves the HTML UI. All templates and static assets are
// embedded in the binary.
package web

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"net/http"

	"genealogy-tree/internal/auth"
	"genealogy-tree/internal/config"
	"genealogy-tree/internal/models"
	"genealogy-tree/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Server carries everything the handlers need.
type Server struct {
	Cfg  config.Config
	St   *store.Store
	Auth *auth.Auth
	tmpl *template.Template
	site string
}

// NewServer parses templates once and returns a ready server.
func NewServer(cfg config.Config, st *store.Store) (*Server, error) {
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	s := &Server{
		Cfg:  cfg,
		St:   st,
		Auth: &auth.Auth{St: st, SecureCookie: cfg.CookieSecure},
		tmpl: tmpl,
	}
	if name, ok, _ := st.Queries().GetSetting(context.Background(), "site_name"); ok && name != "" {
		s.site = name
	} else {
		s.site = "Family Tree"
	}
	return s, nil
}

// Router builds the full route table.
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mux.Handle("GET /static/", http.FileServerFS(staticFS))

	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.loginSubmit)
	mux.HandleFunc("POST /logout", s.wrap("viewer", s.logout))

	mux.HandleFunc("GET /{$}", s.wrap("viewer", s.dashboard))
	mux.HandleFunc("GET /people", s.wrap("viewer", s.peopleList))
	mux.HandleFunc("GET /person/{id}", s.wrap("viewer", s.personPage))
	mux.HandleFunc("GET /family/{id}", s.wrap("viewer", s.familyPage))
	mux.HandleFunc("GET /media/{id}/thumb", s.wrap("viewer", s.mediaThumb))
	mux.HandleFunc("GET /media/{id}/full", s.wrap("viewer", s.mediaFull))
	mux.HandleFunc("GET /person/{id}/tree", s.wrap("viewer", s.treePage))
	mux.HandleFunc("GET /tree.json", s.wrap("viewer", s.treeData))

	// Editor forms (M6): person/family editing + guided add-relative flows.
	// Deleting people/families is admin-only.
	mux.HandleFunc("GET /person/new", s.wrap("editor", s.personNewPage))
	mux.HandleFunc("POST /person/new", s.wrap("editor", s.personCreate))
	mux.HandleFunc("GET /person/{id}/edit", s.wrap("editor", s.personEditPage))
	mux.HandleFunc("POST /person/{id}/edit", s.wrap("editor", s.personUpdate))
	mux.HandleFunc("POST /person/{id}/delete", s.wrap("admin", s.personDelete))
	mux.HandleFunc("GET /family/add", s.wrap("editor", s.familyAddParents))
	mux.HandleFunc("POST /family/add", s.wrap("editor", s.familyAddParents))
	mux.HandleFunc("GET /family/new", s.wrap("editor", s.familyNew))
	mux.HandleFunc("POST /family/new", s.wrap("editor", s.familyNew))
	mux.HandleFunc("GET /family/{id}/edit", s.wrap("editor", s.familyEditPage))
	mux.HandleFunc("POST /family/{id}/edit", s.wrap("editor", s.familyUpdate))
	mux.HandleFunc("POST /family/{id}/delete", s.wrap("admin", s.familyDelete))
	mux.HandleFunc("POST /family/{id}/partner/attach", s.wrap("editor", s.familyAttachPartner))
	mux.HandleFunc("POST /family/{id}/partner/detach", s.wrap("editor", s.familyDetachPartner))
	mux.HandleFunc("POST /family/{id}/children/add", s.wrap("editor", s.familyAddChild))
	mux.HandleFunc("POST /family/{id}/children/remove", s.wrap("editor", s.familyRemoveChild))

	return mux
}

func (s *Server) wrap(minRole string, h http.HandlerFunc) http.HandlerFunc {
	return s.Auth.Wrap(minRole, h)
}

// -- template data ----------------------------------------------------------

func canEdit(role string) bool {
	return models.RoleWeights[role] >= models.RoleWeights["editor"]
}

func isAdmin(role string) bool { return role == "admin" }

// baseData composes the per-page template data.
func (s *Server) baseData(r *http.Request, title string) map[string]any {
	data := map[string]any{
		"Title":    title,
		"SiteName": s.site,
		"CanEdit":  false,
		"CanAdmin": false,
	}
	if sess, ok := auth.FromContext(r.Context()); ok {
		data["Session"] = sess
		data["CSRF"] = sess.CSRF
		data["CanEdit"] = canEdit(sess.User.Role)
		data["CanAdmin"] = isAdmin(sess.User.Role)
	}
	return data
}

// render executes a page template; template errors are logged loudly.
func (s *Server) render(w http.ResponseWriter, page string, status int, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.tmpl.ExecuteTemplate(w, page, data); err != nil {
		fmt.Println("template render error:", page, err)
	}
}

// renderError shows the friendly error page.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	data := s.baseData(r, "Error")
	data["Code"] = status
	data["Message"] = msg
	s.render(w, "error.html", status, data)
}

// parseID converts the {id} path value to int64.
func parseID(r *http.Request) (int64, error) {
	var id int64
	if _, err := fmt.Sscanf(r.PathValue("id"), "%d", &id); err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid id %q", r.PathValue("id"))
	}
	return id, nil
}
