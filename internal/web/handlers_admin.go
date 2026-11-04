package web

import (
	"net/http"
	"regexp"
	"strings"

	"genealogy-tree/internal/auth"
	"genealogy-tree/internal/gedcom"
)

// requireAdmin renders a 403 page unless the current user is an admin.
// Always call `if !s.requireAdmin(w, r) { return }` at the top of handlers.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	sess, ok := auth.FromContext(r.Context())
	if !ok || sess.User.Role != "admin" {
		s.renderError(w, r, http.StatusForbidden, "This area is restricted to administrators.")
		return false
	}
	return true
}

// -- overview ---------------------------------------------------------------

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	ctx := r.Context()
	q := qOf(s, r)

	data := s.baseData(r, "Admin")
	data["PeopleCount"], _ = q.CountPeople(ctx)
	data["FamiliesCount"], _ = q.CountFamilies(ctx)
	data["MediaCount"], _ = q.CountMedia(ctx)
	data["SourcesCount"], _ = q.CountSources(ctx)
	data["UserCount"], _ = q.CountUsers(ctx)
	data["Imports"], _ = q.RecentImports(ctx, 5)
	data["Audit"], _ = q.RecentAuditWithUsers(ctx, 8)
	s.render(w, "admin.html", http.StatusOK, data)
}

// -- users ------------------------------------------------------------------

func (s *Server) adminUsersPage(w http.ResponseWriter, r *http.Request, formErr, notice string) {
	ctx := r.Context()
	q := qOf(s, r)

	var currentID int64
	if sess, ok := auth.FromContext(r.Context()); ok {
		currentID = sess.User.ID
	}
	data := s.baseData(r, "Users")
	data["Users"], _ = q.ListUsers(ctx)
	data["CurrentUserID"] = currentID
	data["Error"] = formErr
	data["Notice"] = notice
	s.render(w, "admin_users.html", http.StatusOK, data)
}

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{2,40}$`)

func (s *Server) adminUserCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	q := qOf(s, r)
	username := formVal(r, "username")
	password := r.PostFormValue("password")
	role := formVal(r, "role")

	fail := func(msg string) {
		s.adminUsersPage(w, r, msg, "")
	}
	if !usernameRe.MatchString(username) {
		fail("Username must be 2-40 letters/digits/._-")
		return
	}
	if len(password) < 8 {
		fail("Password must be at least 8 characters.")
		return
	}
	switch role {
	case "admin", "editor", "viewer":
	default:
		fail("Unknown role.")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		fail("Could not hash password.")
		return
	}
	if _, err := q.CreateUser(r.Context(), username, hash, role); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail("Username already exists.")
			return
		}
		fail("Create failed: " + err.Error())
		return
	}
	_ = q.InsertAudit(r.Context(), s.St.DB, userIDof(r), "admin-create", "user", nil, username)
	s.adminUsersPage(w, r, "", "User "+username+" created with role "+role+".")
}

func (s *Server) adminUserUpdateRole(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := parseID(r)
	if err != nil {
		s.adminUsersPage(w, r, "", "")
		return
	}
	role := formVal(r, "role")
	q := qOf(s, r)
	if sess, ok := auth.FromContext(r.Context()); ok && sess.User.ID == id && role != "admin" {
		s.adminUsersPage(w, r, "", "You cannot demote yourself.")
		return
	}
	if err := q.UpdateUserRole(r.Context(), id, role); err != nil {
		s.adminUsersPage(w, r, err.Error(), "")
		return
	}
	_ = q.InsertAudit(r.Context(), s.St.DB, userIDof(r), "set-role", "user", &id, role)
	s.adminUsersPage(w, r, "", "Role updated.")
}

func (s *Server) adminUserSetPassword(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown user.")
		return
	}
	password := r.PostFormValue("password")
	if len(password) < 8 {
		s.adminUsersPage(w, r, "Password must be at least 8 characters.", "")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.adminUsersPage(w, r, "Could not hash password.", "")
		return
	}
	q := qOf(s, r)
	if err := q.UpdateUserPassword(r.Context(), id, hash); err != nil {
		s.adminUsersPage(w, r, err.Error(), "")
		return
	}
	_ = q.InsertAudit(r.Context(), s.St.DB, userIDof(r), "set-password", "user", &id, "")
	s.adminUsersPage(w, r, "", "Password updated.")
}

func (s *Server) adminUserDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Unknown user.")
		return
	}
	q := qOf(s, r)
	sess, _ := auth.FromContext(r.Context())
	if sess.User.ID == id {
		s.adminUsersPage(w, r, "", "You cannot delete your own account.")
		return
	}
	if err := q.DeleteUser(r.Context(), id); err != nil {
		s.adminUsersPage(w, r, err.Error(), "")
		return
	}
	_ = q.InsertAudit(r.Context(), s.St.DB, userIDof(r), "delete-user", "user", &id, "")
	s.adminUsersPage(w, r, "", "User deleted.")
}

// -- import -----------------------------------------------------------------

func (s *Server) adminImportPage(w http.ResponseWriter, r *http.Request, report *gedcom.Report, formErr string) {
	if !s.requireAdmin(w, r) {
		return
	}
	data := s.baseData(r, "Import GEDCOM")
	data["History"], _ = qOf(s, r).RecentImports(r.Context(), 10)
	if report != nil {
		data["Report"] = report
	}
	data["Error"] = formErr
	s.render(w, "admin_import.html", http.StatusOK, data)
}

func (s *Server) adminImportForm(w http.ResponseWriter, r *http.Request) {
	s.adminImportPage(w, r, nil, "")
}

func (s *Server) adminImportSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 110<<20) // ~100 MB GEDCOM + form overhead
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		s.adminImportPage(w, r, nil, "File upload failed (too large? limit ~100 MB).")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.adminImportPage(w, r, nil, "Pick a .ged file to import.")
		return
	}
	defer file.Close()

	rep, err := gedcom.Import(r.Context(), s.St, file, header.Filename)
	if err != nil {
		s.adminImportPage(w, r, &rep, "Import failed: "+err.Error())
		return
	}
	s.adminImportPage(w, r, &rep, "")
}

// -- settings ---------------------------------------------------------------

func (s *Server) adminSettingsPage(w http.ResponseWriter, r *http.Request, notice, formErr string) {
	if !s.requireAdmin(w, r) {
		return
	}
	q := qOf(s, r)
	settings, _ := q.Settings(r.Context())
	data := s.baseData(r, "Settings")
	data["Settings"] = settings
	data["Notice"] = notice
	data["Error"] = formErr
	s.render(w, "admin_settings.html", http.StatusOK, data)
}

// settingsForm renders GET /admin/settings.
func (s *Server) adminSettingsForm(w http.ResponseWriter, r *http.Request) {
	s.adminSettingsPage(w, r, "", "")
}

// adminSettingsSubmit handles POST /admin/settings.
func (s *Server) adminSettingsSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	q := qOf(s, r)
	name := formVal(r, "site_name")
	if name == "" || len(name) > 60 {
		s.adminSettingsPage(w, r, "", "Site name must be 1-60 characters.")
		return
	}
	if err := q.SetSetting(r.Context(), "site_name", name); err != nil {
		s.adminSettingsPage(w, r, "", err.Error())
		return
	}
	_ = q.InsertAudit(r.Context(), s.St.DB, userIDof(r), "set-setting", "settings", nil, "site_name="+name)
	s.adminSettingsPage(w, r, "Saved.", "")
}

// adminAuditPage handles GET /admin/audit.
func (s *Server) adminAuditPage(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	entries, err := qOf(s, r).RecentAuditWithUsers(r.Context(), 200)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	data := s.baseData(r, "Audit log")
	data["Audit"] = entries
	s.render(w, "admin_audit.html", http.StatusOK, data)
}
