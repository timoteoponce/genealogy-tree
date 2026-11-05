package web

import (
	"net/http"

	"genealogy-tree/internal/auth"
	"genealogy-tree/internal/store"
)

// First-run setup: when the database has NO users at all, every anonymous
// request is funneled here so the operator can create the initial admin
// with a password of their choosing. No users exist silently in the app.
//
// The endpoint closes itself permanently as soon as one admin exists —
// verified again inside the INSERT transaction, so racing first-time
// POSTs cannot mint a second admin.

// setupPage handles GET /setup.
func (s *Server) setupPage(w http.ResponseWriter, r *http.Request) {
	if n, _ := qOf(s, r).CountUsers(r.Context()); n > 0 {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	data := s.baseData(r, "Welcome")
	s.render(w, "setup.html", http.StatusOK, data)
}

// setupSubmit handles POST /setup.
func (s *Server) setupSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := qOf(s, r)
	if n, _ := q.CountUsers(ctx); n > 0 {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	username := formVal(r, "username")
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("confirm")

	redisplay := func(msg string) {
		data := s.baseData(r, "Welcome")
		data["Error"] = msg
		data["Username"] = username
		s.render(w, "setup.html", http.StatusUnprocessableEntity, data)
	}

	if !usernameRe.MatchString(username) {
		redisplay("Username must be 2-40 letters, digits or . _ -")
		return
	}
	if len(password) < 8 {
		redisplay("Password must be at least 8 characters.")
		return
	}
	if password != confirm {
		redisplay("Passwords do not match.")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		redisplay("Could not hash password.")
		return
	}

	tx, err := s.St.Begin(ctx)
	if err != nil {
		redisplay("Database unavailable.")
		return
	}
	defer tx.Rollback()
	tq := store.NewQueries(tx)

	if _, err := tq.CreateUser(ctx, username, hash, "admin"); err != nil {
		redisplay("Could not create account: " + err.Error())
		return
	}
	// Post-insert guard: a concurrent first-time POST already claimed setup.
	var total int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&total); err == nil && total > 1 {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if err := tq.InsertAudit(ctx, tx, nil, "first-setup", "user", nil, username); err != nil {
		redisplay("Could not record setup.")
		return
	}
	if err := tx.Commit(); err != nil {
		redisplay("Could not save.")
		return
	}

	http.Redirect(w, r, "/login", http.StatusFound)
}
