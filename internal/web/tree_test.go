package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestFullTreePage covers GET /tree: root picker renders, the default root
// is reachable, and /tree.json?gens=0 includes everyone reachable.
func TestFullTreePage(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	status, body := get(t, e.admin, e.ts.URL+"/tree")
	if status != http.StatusOK || !strings.Contains(body, "root") {
		t.Fatalf("full tree page: %d", status)
	}
	if !strings.Contains(body, "John Smith") || !strings.Contains(body, "people") {
		t.Fatalf("root picker missing: %s", body)
	}

	// gens=0 (full) returns every reachable person for the focus root.
	status, body = get(t, e.admin, e.ts.URL+"/tree.json?person=1&gens=0")
	if status != http.StatusOK {
		t.Fatalf("full tree.json: %d", status)
	}
	for _, want := range []string{`"id":"1"`, `"id":"2"`, `"id":"3"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("full tree missing %s: %s", want, body)
		}
	}

	// Root picker can switch roots via ?person=.
	status, body = get(t, e.admin, e.ts.URL+"/tree?person=2")
	if status != http.StatusOK || !strings.Contains(body, `value="2"`) {
		t.Fatalf("root switch: %d %s", status, body)
	}

	// Nav + hero link present on the dashboard.
	_, dash := get(t, e.admin, e.ts.URL+"/")
	if !strings.Contains(dash, `href="/tree"`) {
		t.Error("dashboard should link to /tree")
	}
}

// TestFullTreeEmptyDB renders the friendly empty state on a fresh deployment.
func TestFullTreeEmptyDB(t *testing.T) {
	e := emptyEnv(t)
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// Create the first admin via /setup, then log in.
	status, _, _ := postLocation(t, noFollow, e.ts.URL+"/setup", url.Values{
		"username": {"root"}, "password": {"super-secret-9"}, "confirm": {"super-secret-9"},
	})
	if status != http.StatusFound {
		t.Fatalf("setup: %d", status)
	}
	admin := e.clientWithLogin(t, "root", "super-secret-9")

	status, body := get(t, admin, e.ts.URL+"/tree")
	if status != http.StatusOK || !strings.Contains(body, "Nothing to show yet") {
		t.Fatalf("empty tree state: %d %s", status, body)
	}
}
