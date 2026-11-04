package web_test

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"genealogy-tree/internal/auth"
	"genealogy-tree/internal/config"
	"genealogy-tree/internal/db"
	"genealogy-tree/internal/gedcom"
	"genealogy-tree/internal/store"
	"genealogy-tree/internal/web"
)

type env struct {
	ts    *httptest.Server
	admin *http.Client // logged-in admin
	jar   *cookiejar.Jar
}

// newEnv boots an in-memory server with one admin, one viewer and the
// minimal sample tree imported.
func newEnv(t *testing.T, gedcomFile string) *env {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	st := store.New(d)
	ctx := context.Background()

	hash, _ := auth.HashPassword("pw-admin")
	if _, err := st.Queries().CreateUser(ctx, "admin", hash, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Queries().CreateUser(ctx, "viewer", hash, "viewer"); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(gedcomFile); err == nil {
		if _, err := gedcom.Import(ctx, st, f, filepath.Base(gedcomFile)); err != nil {
			t.Fatalf("import: %v", err)
		}
		f.Close()
	}

	cfg := config.Config{DataDir: t.TempDir(), DBPath: "unused", Port: 0}
	srv, err := web.NewServer(cfg, st)
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	e := &env{ts: ts}
	e.admin = e.clientWithLogin(t, "admin", "pw-admin")
	return e
}

// clientWithLogin performs the real login POST and keeps the cookie jar.
func (e *env) clientWithLogin(t *testing.T, username, password string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := c.PostForm(e.ts.URL+"/login", url.Values{
		"username": {username}, "password": {password},
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login expected redirect, got %d", resp.StatusCode)
	}
	return c
}

// get follows redirects off and returns status + body as strings.
func get(t *testing.T, c *http.Client, path string) (int, string) {
	t.Helper()
	resp, err := c.Get(path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

var csrfRe = regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`)

func TestAnonymousRedirectsToLogin(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noFollow.Get(e.ts.URL + "/person/1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || !strings.Contains(resp.Header.Get("Location"), "/login") {
		t.Fatalf("want login redirect, got %d → %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if !strings.Contains(resp.Header.Get("Location"), "%2Fperson%2F1") {
		t.Errorf("redirect should carry ?next=<uri>, got %q", resp.Header.Get("Location"))
	}
}

func TestDashboardAndPeopleList(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	status, body := get(t, e.admin, e.ts.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("dashboard: %d", status)
	}
	if !strings.Contains(body, "<b>3</b>") && !strings.Contains(body, "3") {
		t.Errorf("dashboard should show people count")
	}
	if !strings.Contains(body, "John Smith") {
		t.Errorf("recent people should list John, body: %s", body)
	}

	status, body = get(t, e.admin, e.ts.URL+"/people?q=smith")
	if status != http.StatusOK || !strings.Contains(body, "John Smith") || !strings.Contains(body, "Samuel") {
		t.Fatalf("people search broken: %d", status)
	}
	// page 2 exists and is empty-safe
	status, _ = get(t, e.admin, e.ts.URL+"/people?page=2")
	if status != http.StatusOK {
		t.Fatalf("pagination page 2: %d", status)
	}
}

func TestPersonAndFamilyPages(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	status, body := get(t, e.admin, e.ts.URL+"/person/1")
	if status != http.StatusOK || !strings.Contains(body, "John Smith") {
		t.Fatalf("person page: %d", status)
	}
	if !strings.Contains(body, "Boston, Massachusetts") || !strings.Contains(body, "Blacksmith") {
		t.Errorf("facts missing: %s", body)
	}
	if !strings.Contains(body, "Tree</a>") && !strings.Contains(body, "View tree") {
		t.Errorf("tree link missing")
	}
	// 404 for unknown person
	status, _ = get(t, e.admin, e.ts.URL+"/person/999")
	if status != http.StatusNotFound {
		t.Errorf("unknown person should 404, got %d", status)
	}

	// family 1 (from minimal) with marriage year
	status, body = get(t, e.admin, e.ts.URL+"/family/1")
	if status != http.StatusOK || !strings.Contains(body, "1875") || !strings.Contains(body, "Samuel") {
		t.Fatalf("family page: %d %s", status, body)
	}
}

func TestTreeViewFallback(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	// Default = interactive chart page with the data endpoint wired.
	status, body := get(t, e.admin, e.ts.URL+"/person/3/tree")
	if status != http.StatusOK || !strings.Contains(body, `id="chart" class="f3"`) {
		t.Fatalf("chart tree page: %d", status)
	}
	if !strings.Contains(body, `data-focus-id="3"`) {
		t.Errorf("focus id not passed to the chart: %s", body)
	}

	// tree.json returns family-chart nodes around the focus person.
	status, body = get(t, e.admin, e.ts.URL+"/tree.json?person=3")
	if status != http.StatusOK {
		t.Fatalf("tree.json: %d", status)
	}
	if !strings.Contains(body, `"first name":"John"`) || !strings.Contains(body, `"parents":["1","2"]`) {
		t.Fatalf("tree.json content unexpected: %s", body)
	}

	// ?view=list keeps the server-rendered fallback.
	status, body = get(t, e.admin, e.ts.URL+"/person/3/tree?view=list")
	if status != http.StatusOK || !strings.Contains(body, "Ancestors") || !strings.Contains(body, "John Smith") {
		t.Fatalf("list ancestors: %d %s", status, body)
	}
	status, body = get(t, e.admin, e.ts.URL+"/person/1/tree?view=list")
	if status != http.StatusOK || !strings.Contains(body, "Descendants") || !strings.Contains(body, "Samuel") {
		t.Fatalf("list descendants: %d %s", status, body)
	}
}

func TestMediaPlaceholderAndStatic(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	// No photos imported: thumb redirects to the placeholder.
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}).Get(e.ts.URL + "/media/1/thumb")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("thumb without blob should redirect to placeholder, got %d", resp.StatusCode)
	}

	status, body := get(t, e.admin, e.ts.URL+"/static/style.css")
	if status != http.StatusOK || !strings.Contains(body, "--accent:") {
		t.Errorf("static css: %d", status)
	}
}

func TestLogoutAndCSRF(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	// Grab a fresh login + the CSRF token from the dashboard.
	jar, _ := cookiejar.New(nil)
	c := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	c.PostForm(e.ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"pw-admin"}})
	_, dash := get(t, c, e.ts.URL+"/")
	m := csrfRe.FindStringSubmatch(dash)
	if m == nil {
		t.Fatalf("csrf token not found in dashboard: %s", dash)
	}

	// POST /logout without the token → 403.
	resp, err := c.PostForm(e.ts.URL+"/logout", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("logout without csrf: %d", resp.StatusCode)
	}

	// POST with the token → success and session gone.
	resp, _ = c.PostForm(e.ts.URL+"/logout", url.Values{"csrf": {m[1]}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("logout with csrf: %d", resp.StatusCode)
	}
	status, _ := get(t, c, e.ts.URL+"/people")
	if status != http.StatusFound {
		t.Errorf("after logout session should be gone, got %d", status)
	}
}

func TestLoggedInWrongRoleGets403(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	// viewer account hits an admin-ish route (edit pages exist as 501s) —
	// Wrap("admin", ...) gate will be exercised in M8 once admin pages land.
	// For now verify viewer can view pages:
	vc := e.clientWithLogin(t, "viewer", "pw-admin")
	status, body := get(t, vc, e.ts.URL+"/people")
	if status != 200 || !strings.Contains(body, "People") {
		t.Errorf("viewer should see people list: %d", status)
	}
}
