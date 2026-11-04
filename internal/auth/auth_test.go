package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"genealogy-tree/internal/db"
	"genealogy-tree/internal/store"
)

// newAuth spins up a real store (temp sqlite) with one admin and one viewer.
func newAuth(t *testing.T) *Auth {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "auth-test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	st := store.New(d)
	q := st.Queries()
	ctx := context.Background()

	hash, err := HashPassword("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.CreateUser(ctx, "admin", hash, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := q.CreateUser(ctx, "v", hash, "viewer"); err != nil {
		t.Fatal(err)
	}
	return &Auth{St: st}
}

func TestPasswordHashAndCheck(t *testing.T) {
	h, err := HashPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "pw") {
		t.Error("correct password rejected")
	}
	if CheckPassword(h, "wrong") {
		t.Error("wrong password accepted")
	}
	if strings.Contains(h, "pw") {
		t.Error("hash should not contain plaintext")
	}
}

func TestLoginSuccessAndFailure(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()

	token, err := a.Login(ctx, "admin", "  ") // wrong
	if !errors.Is(err, ErrLogin) || token != "" {
		t.Fatalf("bad-password should be ErrLogin, got %q %v", token, err)
	}

	token, err = a.Login(ctx, "nope", "x") // unknown user, same error
	if !errors.Is(err, ErrLogin) {
		t.Fatalf("unknown-user should also be ErrLogin, got %v", err)
	}

	token, err = a.Login(ctx, "admin", "hunter2")
	if err != nil || token == "" {
		t.Fatalf("login failed: %v", err)
	}

	// Token must resolve to the admin session with a CSRF value present.
	sess, ok := a.Session(reqWithCookie(t, token))
	if !ok || sess.User.Username != "admin" || sess.CSRF == "" {
		t.Fatalf("session not resolved: %+v ok=%v", sess, ok)
	}
}

// reqWithCookie builds a GET / with the session cookie attached.
func reqWithCookie(t *testing.T, token string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: token})
	return r
}

func TestSessionExpiry(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	token, err := a.Login(ctx, "admin", "hunter2")
	if err != nil {
		t.Fatal(err)
	}
	// Expire all sessions, then the cookie must no longer resolve.
	if _, err := a.St.DB.Exec(`UPDATE sessions SET expires_at='2000-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.Session(reqWithCookie(t, token)); ok {
		t.Error("expired session still valid")
	}
}

func TestLogoutDeletesSession(t *testing.T) {
	a := newAuth(t)
	token, err := a.Login(context.Background(), "admin", "hunter2")
	if err != nil {
		t.Fatal(err)
	}
	a.Logout(reqWithCookie(t, token))
	if _, ok := a.Session(reqWithCookie(t, token)); ok {
		t.Error("session survived logout")
	}
}

func TestWrapRolesAndCSRF(t *testing.T) {
	a := newAuth(t)
	adminToken, _ := a.Login(context.Background(), "admin", "hunter2")
	viewerToken, _ := a.Login(context.Background(), "v", "hunter2")

	adminSession, _ := a.Session(reqWithCookie(t, adminToken))
	adminCSRF := adminSession.CSRF
	viewerSession, _ := a.Session(reqWithCookie(t, viewerToken))
	viewerCSRF := viewerSession.CSRF

	editorPage := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "OK")
	})
	wrapped := a.Wrap("editor", editorPage)

	// 1. Anonymous → redirect to /login.
	rr := httptest.NewRecorder()
	wrapped(rr, httptest.NewRequest(http.MethodGet, "/person/7/edit", nil))
	if rr.Code != http.StatusFound {
		t.Fatalf("anonymous should redirect, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); !strings.Contains(loc, "/login") ||
		!strings.Contains(loc, url.QueryEscape("/person/7/edit")) {
		t.Errorf("redirect target missing next: %q", loc)
	}

	// 2. Viewer on editor page → 403.
	rr = httptest.NewRecorder()
	wrapped(rr, reqWithCookie2(t, viewerToken,
		http.MethodGet, "/person/7/edit", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("viewer should get 403 on editor page, got %d", rr.Code)
	}

	// 3. Admin GET on editor page → 200.
	rr = httptest.NewRecorder()
	wrapped(rr, reqWithCookie2(t, adminToken, http.MethodGet, "/person/7/edit", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("admin should pass, got %d", rr.Code)
	}

	// 4. POST without CSRF → 403.
	rr = httptest.NewRecorder()
	wrapped(rr, reqWithCookie2(t, adminToken, http.MethodPost, "/person/7/edit", &url.Values{}))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("POST without csrf should be 403, got %d", rr.Code)
	}

	// 5. POST with wrong CSRF → 403.
	rr = httptest.NewRecorder()
	form := url.Values{"csrf": {"wrong"}}
	wrapped(rr, reqWithCookie2(t, adminToken, http.MethodPost, "/person/7/edit", &form))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("POST with wrong csrf should be 403, got %d", rr.Code)
	}

	// 6. POST with correct CSRF → 200.
	rr = httptest.NewRecorder()
	form = url.Values{"csrf": {adminCSRF}}
	wrapped(rr, reqWithCookie2(t, adminToken, http.MethodPost, "/person/7/edit", &form))
	if rr.Code != http.StatusOK {
		t.Fatalf("POST with correct csrf should be 200, got %d", rr.Code)
	}

	// 7. Viewer POST with correct CSRF still blocked by role.
	rr = httptest.NewRecorder()
	form = url.Values{"csrf": {viewerCSRF}}
	wrapped(rr, reqWithCookie2(t, viewerToken, http.MethodPost, "/person/7/edit", &form))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("viewer POST should be 403 by role, got %d", rr.Code)
	}

	// 8. Session context must carry the user into the handler.
	ctxCarried := false
	checked := a.Wrap("viewer", func(w http.ResponseWriter, r *http.Request) {
		if s, ok := FromContext(r.Context()); ok && s.User.Username == "admin" {
			ctxCarried = true
		}
	})
	rr = httptest.NewRecorder()
	checked(rr, reqWithCookie2(t, adminToken, http.MethodGet, "/", nil))
	if !ctxCarried {
		t.Error("session not available via FromContext")
	}
}

// reqWithCookie2 builds a request with cookie + body form values.
func reqWithCookie2(t *testing.T, token, method, path string, form *url.Values) *http.Request {
	t.Helper()
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: token})
	return r
}

func TestSlidingExpiry(t *testing.T) {
	a := newAuth(t)
	ctx := context.Background()
	token, _ := a.Login(ctx, "admin", "hunter2")

	tok := hashToken(token)
	// Set expiry to 5 days out — still valid, but less than half the 30-day
	// TTL, so Session() must slide it forward.
	fiveDays := time.Now().Add(5 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	if _, err := a.St.DB.Exec(`UPDATE sessions SET expires_at=?`, fiveDays); err != nil {
		t.Fatal(err)
	}
	a.Session(reqWithCookie(t, token)) // should rewrite (sliding window)
	var expires string
	if err := a.St.DB.QueryRow(`SELECT expires_at FROM sessions WHERE id=?`, tok).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse("2006-01-02 15:04:05", expires); err != nil ||
		strings.HasPrefix(expires, expires[:4]) && expires[:4] == fiveDays[:4] && expires < time.Now().Add(SessionTTL/2).UTC().Format("2006-01-02 15:04:05") {
		t.Errorf("expiry not slid forward: %q", expires)
	}
}
