package web_test

import (
	"context"
	"net/http"
	cookiejar "net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"genealogy-tree/internal/config"
	"genealogy-tree/internal/db"
	"genealogy-tree/internal/store"
	"genealogy-tree/internal/web"
)

// emptyEnv boots a server with ZERO users (fresh deployment).
func emptyEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	st := store.New(d)

	cfg := config.Config{DataDir: t.TempDir(), DBPath: "unused", Port: 0}
	srv, err := web.NewServer(cfg, st)
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	return &env{ts: ts, St: st}
}

func TestFirstRunSetupFlow(t *testing.T) {
	e := emptyEnv(t)

	// 1. Public browsing works before setup; login still directs to setup.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noFollow.Get(e.ts.URL + "/people")
	if err != nil {
		t.Fatal(err)
	}
	loc := resp.Header.Get("Location")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous /people before setup: got %d", resp.StatusCode)
	}
	resp, _ = noFollow.Get(e.ts.URL + "/login")
	loc = resp.Header.Get("Location")
	resp.Body.Close()
	if loc != "/setup" {
		t.Fatalf("fresh login should redirect to /setup, got %q", loc)
	}
	// Direct setup GET renders the form.
	status, body := get(t, noFollow, e.ts.URL+"/setup")
	if status != http.StatusOK || !strings.Contains(body, "Set up the first admin") {
		t.Fatalf("setup page: %d %s", status, body)
	}

	// 2. Validation: short password (setup POST is anonymous, like /login —
	// no CSRF token exists before the first session).
	form := url.Values{
		"username": {"root"},
		"password": {"short"},
		"confirm":  {"short"},
	}
	status, body = post(t, noFollow, e.ts.URL+"/setup", form)
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "at least 8") {
		t.Fatalf("validation: %d %s", status, body)
	}

	// 3. Mismatch (password long enough to reach the mismatch branch).
	form.Set("password", "super-secret-9")
	form.Set("confirm", "different")
	status, body = post(t, noFollow, e.ts.URL+"/setup", form)
	if !strings.Contains(body, "do not match") {
		t.Fatalf("confirm mismatch handling: %d %s", status, body)
	}

	// 4. Happy path creates the admin (role=admin) and closes /setup.
	form.Set("password", "super-secret-9")
	form.Set("confirm", "super-secret-9")
	status, loc, _ = postLocation(t, noFollow, e.ts.URL+"/setup", form)
	if status != http.StatusFound || loc != "/login" {
		t.Fatalf("setup success redirect: %d %q", status, loc)
	}
	user, err := e.St.Queries().UserByUsername(context.Background(), "root")
	if err != nil || user.Role != "admin" {
		t.Fatalf("admin not created: %+v %v", user, err)
	}

	// /setup is gone for good.
	resp, _ = noFollow.Get(e.ts.URL + "/setup")
	loc = resp.Header.Get("Location")
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || loc != "/login" {
		t.Fatalf("setup should redirect to login after use, got %d %q", resp.StatusCode, loc)
	}

	// 5. Login with the created credentials works (chain ends on dashboard).
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	resp, _ = c.PostForm(e.ts.URL+"/login", url.Values{
		"username": {"root"}, "password": {"super-secret-9"},
	})
	resp.Body.Close()
	status, dash := get(t, c, e.ts.URL+"/")
	if status != http.StatusOK || !strings.Contains(dash, "root") {
		t.Fatalf("dashboard after setup login: %d", status)
	}
}
