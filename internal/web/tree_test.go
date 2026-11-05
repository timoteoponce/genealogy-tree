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

// TestTreeOrientationToggle: full tree defaults horizontal (data-orient="h"),
// per-person tree defaults vertical, and ?orient=h flips it.
func TestTreeOrientationToggle(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	_, body := get(t, e.admin, e.ts.URL+"/tree")
	if !strings.Contains(body, `data-orient="h"`) {
		t.Errorf("full tree should default horizontal: %s", body)
	}

	_, body = get(t, e.admin, e.ts.URL+"/person/1/tree")
	if !strings.Contains(body, `data-orient="v"`) {
		t.Errorf("per-person tree should default vertical")
	}
	if !strings.Contains(body, "Horizontal") {
		t.Errorf("per-person tree should offer a horizontal toggle")
	}

	_, body = get(t, e.admin, e.ts.URL+"/person/1/tree?orient=h")
	if !strings.Contains(body, `data-orient="h"`) {
		t.Errorf("?orient=h should flip to horizontal")
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

// TestFanAndGraphEndpoints covers /fan.json (ancestor tree) and /graph.json
// (everyone + links).
func TestFanAndGraphEndpoints(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	// Fan: Samuel (id 3) has parents John + Mary.
	status, body := get(t, e.admin, e.ts.URL+"/fan.json?person=3")
	if status != http.StatusOK {
		t.Fatalf("fan.json: %d", status)
	}
	if !strings.Contains(body, `"id":"3"`) || !strings.Contains(body, `"id":"1"`) || !strings.Contains(body, `"id":"2"`) {
		t.Fatalf("fan.json should nest parents under focus: %s", body)
	}

	// Graph: 3 nodes, spouse link (1-2) + parent links (1-3, 2-3).
	status, body = get(t, e.admin, e.ts.URL+"/graph.json")
	if status != http.StatusOK {
		t.Fatalf("graph.json: %d", status)
	}
	if !strings.Contains(body, `"type":"spouse"`) || !strings.Contains(body, `"type":"parent"`) {
		t.Fatalf("graph.json missing link types: %s", body)
	}
	for _, want := range []string{`"id":"1"`, `"id":"2"`, `"id":"3"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("graph.json missing node %s: %s", want, body)
		}
	}
}

// TestViewSwitcherRendersEachView: ?view=fan and ?view=graph render their
// own containers and scripts.
func TestViewSwitcherRendersEachView(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	_, body := get(t, e.admin, e.ts.URL+"/person/3/tree?view=fan")
	if !strings.Contains(body, `id="fan"`) || !strings.Contains(body, "fan.js") {
		t.Fatalf("fan view markup missing: %s", body)
	}

	_, body = get(t, e.admin, e.ts.URL+"/person/3/tree?view=graph")
	if !strings.Contains(body, `id="graph"`) || !strings.Contains(body, "graph.js") {
		t.Fatalf("graph view markup missing: %s", body)
	}

	// Default view for a tiny tree is still the standard chart.
	_, body = get(t, e.admin, e.ts.URL+"/person/3/tree")
	if !strings.Contains(body, `id="chart" class="f3"`) {
		t.Fatalf("small tree should default to the family-chart: %s", body)
	}
}
