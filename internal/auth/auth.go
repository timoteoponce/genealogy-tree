// Package auth implements password hashing, session cookies, role-based
// access and CSRF protection.
//
// Model:
//   - Session rows store the SHA-256 of the cookie token, never the token.
//   - Every request passes through (Auth).Wrap(minRole, handler): anonymous
//     users are redirected to /login, under-privileged ones get a 403 page.
//   - Every POST an authenticated user sends must include the per-session
//     CSRF token as form field "csrf" (checked inside Wrap).
//   - The /login POST is the only form without CSRF (no session exists yet);
//     it only accepts username+password — a documented, low-risk simplification.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"genealogy-tree/internal/models"
	"genealogy-tree/internal/store"
)

// SessionCookie is the browser cookie name.
const SessionCookie = "ft_session"

// SessionTTL is how long a session lives (sliding: refreshed on activity).
const SessionTTL = 30 * 24 * time.Hour

// ErrLogin is returned (unwrapped) on failed logins: one message for both
// unknown users and wrong passwords, to prevent user enumeration.
var ErrLogin = errors.New("wrong username or password")

// Session bundles the logged-in user with their CSRF token for handlers.
type Session struct {
	User models.User
	CSRF string
}

// Auth wires the session machinery to the store.
type Auth struct {
	St           *store.Store
	SecureCookie bool // set when running behind HTTPS
}

// HashPassword bcrypts a plaintext password for storage.
func HashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(h), nil
}

// CheckPassword reports whether plain matches a stored bcrypt hash.
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// randomHex returns n cryptographically random bytes as hex.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error()) // unrecoverable
	}
	return hex.EncodeToString(b)
}

// hashToken is the one-way transform cookie token -> sessions id (hash).
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// hashForTiming is a valid bcrypt hash used to equalize failed-login timing.
const hashForTiming = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// Login verifies credentials and creates a session. The returned token must
// be placed in the session cookie via SetSessionCookie.
func (a *Auth) Login(ctx context.Context, username, password string) (string, error) {
	user, err := a.St.Queries().UserByUsername(ctx, strings.TrimSpace(username))
	if err == nil && CheckPassword(user.PasswordHash, password) {
		token := randomHex(32)
		csrf := randomHex(32)
		expires := time.Now().Add(SessionTTL).UTC().Format("2006-01-02 15:04:05") // SQLite datetime-comparable
		q := a.St.Queries()
		_, _ = q.DeleteExpiredSessions(ctx) // opportunistic cleanup
		if err := q.InsertSession(ctx, hashToken(token), user.ID, csrf, expires); err != nil {
			return "", err
		}
		return token, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("login lookup: %w", err)
	}
	// Burn comparable time so unknown-user failures cost like wrong-password.
	_ = bcrypt.CompareHashAndPassword([]byte(hashForTiming), []byte(password))
	return "", ErrLogin
}

// Logout deletes the session behind the request's cookie.
func (a *Auth) Logout(r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
		_ = a.St.Queries().DeleteSession(r.Context(), hashToken(c.Value))
	}
}

// SetSessionCookie writes the login cookie.
func (a *Auth) SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(SessionTTL / time.Second),
		HttpOnly: true,
		Secure:   a.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie invalidates the browser cookie (after Logout).
func (a *Auth) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

// -- request context ------------------------------------------------------

var ctxKeySession = new(int)

// FromContext returns the session bound by Wrap, if any.
func FromContext(ctx context.Context) (Session, bool) {
	s, _ := ctx.Value(ctxKeySession).(Session)
	return s, s.User.ID != 0
}

// Session reads the cookie, resolves the user, and slides the expiry.
func (a *Auth) Session(r *http.Request) (Session, bool) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return Session{}, false
	}
	user, csrf, expires, err := a.St.Queries().LookupSession(r.Context(), hashToken(c.Value))
	if err != nil {
		return Session{}, false
	}
	// Sliding expiry: refresh sessions that are more than half spent.
	if exp, perr := time.Parse("2006-01-02 15:04:05", expires); perr == nil &&
		time.Until(exp) < SessionTTL/2 {
		_ = a.St.Queries().UpdateSessionExpiry(r.Context(), hashToken(c.Value),
			time.Now().Add(SessionTTL).UTC().Format("2006-01-02 15:04:05"))
	}
	return Session{User: user, CSRF: csrf}, true
}

// Wrap guards a handler for users with at least minRole
// ("viewer", "editor" or "admin"; empty string = any logged-in user).
//
// It 302s anonymous users to /login (with ?next= to return), blocks
// under-privileged users with a 403 page, and validates the CSRF form field
// on POST. The session is injected into the request context.
func (a *Auth) Wrap(minRole string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := a.Session(r)
		if !ok {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		if minRole != "" && models.RoleWeights[sess.User.Role] < models.RoleWeights[minRole] {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, "<h1>403 Forbidden</h1><p>Your account (%s) lacks the %s role for this action.</p>",
				html.EscapeString(sess.User.Username), minRole)
			return
		}
		if r.Method == http.MethodPost {
			if token := r.PostFormValue("csrf"); token == "" || token != sess.CSRF {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, "<h1>403 Forbidden</h1><p>Missing or invalid CSRF token — reload the page and try again.</p>")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKeySession, sess)))
	}
}
