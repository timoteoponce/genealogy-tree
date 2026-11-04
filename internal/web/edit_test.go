package web_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"genealogy-tree/internal/models"
)

// csrfFrom fetches a page and extracts its CSRF token via the env client.
func csrfFrom(t *testing.T, e *env, client *http.Client, path string) string {
	t.Helper()
	status, body := get(t, client, e.ts.URL+path)
	if status != http.StatusOK {
		t.Fatalf("csrf fetch %s: %d", path, status)
	}
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("csrf token not found on %s", path)
	}
	return m[1]
}

// post sends a form and returns status + body (redirects not followed).
func post(t *testing.T, client *http.Client, path string, form url.Values) (int, string) {
	status, loc, body := postLocation(t, client, path, form)
	_ = loc
	return status, body
}

// postLocation additionally returns the Location header of redirects.
func postLocation(t *testing.T, client *http.Client, path string, form url.Values) (int, string, string) {
	t.Helper()
	resp, err := client.PostForm(path, form)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header.Get("Location"), string(b)
}

func TestPersonEditFlow(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	// 1. Viewer gets 403 on the edit page.
	status, _ := get(t, e.viewer, e.ts.URL+"/person/1/edit")
	if status != http.StatusForbidden {
		t.Fatalf("viewer should be 403 on edit, got %d", status)
	}

	// 2. Admin opens the form, edits, and the change is visible.
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")
	form := url.Values{
		"csrf":        {csrf},
		"given_names": {"Johnny"},
		"surname":     {"Smith III"},
		"sex":         {"M"},
		"birth_date":  {"Mar 1847"},
		"birth_place": {"Boston, MA"},
	}
	status, _ = post(t, e.admin, e.ts.URL+"/person/1/edit", form)
	if status != http.StatusFound {
		t.Fatalf("edit post expected redirect, got %d", status)
	}

	status, body := get(t, e.admin, e.ts.URL+"/person/1")
	if status != http.StatusOK || !strings.Contains(body, "Johnny Smith III") {
		t.Fatalf("edited person not visible: %d", status)
	}

	// Store reflects the change and the living flag recomputes (born 1847).
	p, err := e.St.Queries().PersonByID(context.Background(), 1)
	if err != nil || p.GivenNames != "Johnny" || p.Living {
		t.Fatalf("db state: %+v %v", p, err)
	}
}

func TestPersonCreateAndRelativesFlow(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/people")

	// 1. Create a plain new person.
	form := url.Values{
		"csrf":        {csrf},
		"given_names": {"Martha"},
		"surname":     {"Long"},
		"sex":         {"F"},
	}
	status, loc, _ := postLocation(t, e.admin, e.ts.URL+"/person/new", form)
	if status != http.StatusFound || !strings.Contains(loc, "/person/4") {
		t.Fatalf("create person: %d %q", status, loc)
	}
	if _, err := e.St.Queries().PersonByID(context.Background(), 4); err != nil {
		t.Fatalf("person 4 should exist: %v", err)
	}

	// 2. Add parents to person 1 (John).
	form = url.Values{
		"csrf":           {"x"},
		"child":          {"1"},
		"father_given":   {"Peter"},
		"father_surname": {"Smith"},
		"mother_given":   {"Ilsa"},
		"marriage_date":  {"1844"},
	}
	// need real csrf from the add-parents page
	csrf = csrfFrom(t, e, e.admin, "/family/add?child=1")
	form.Set("csrf", csrf)
	status2, loc2, _ := postLocation(t, e.admin, e.ts.URL+"/family/add", form)
	if status2 != http.StatusFound || !strings.Contains(loc2, "/family/") {
		t.Fatalf("add parents: %d %q", status2, loc2)
	}
	var body string
	_, body = get(t, e.admin, e.ts.URL+"/person/1")
	if !strings.Contains(body, "Peter") || !strings.Contains(body, "Ilsa") {
		t.Fatalf("parents missing on person page")
	}

	// 3. Add spouse to person 1.
	csrf = csrfFrom(t, e, e.admin, "/family/new?partner=1")
	form = url.Values{
		"csrf":           {csrf},
		"partner":        {"1"},
		"spouse_given":   {"Rebecca"},
		"spouse_surname": {"Howe"},
		"marriage_date":  {"1877"},
	}
	status3, loc3, _ := postLocation(t, e.admin, e.ts.URL+"/family/new", form)
	if status3 != http.StatusFound || !strings.Contains(loc3, "/family/") {
		t.Fatalf("add spouse: %d %q", status3, loc3)
	}
	_, body = get(t, e.admin, e.ts.URL+"/person/1")
	if !strings.Contains(body, "Rebecca") {
		t.Fatalf("spouse missing on person page")
	}

	// 4. Add an existing person as child of family 1, then unlink.
	csrf = csrfFrom(t, e, e.admin, "/family/1/edit")
	form = url.Values{"csrf": {csrf}, "person_id": {"4"}}
	status, body = post(t, e.admin, e.ts.URL+"/family/1/children/add", form)
	if status != http.StatusFound {
		t.Fatalf("add child: %d", status)
	}
	kids, _ := e.St.Queries().ChildrenOfFamily(context.Background(), 1)
	if len(kids) != 2 || kids[0].ID != 3 || kids[1].ID != 4 {
		t.Fatalf("children of family 1: %+v", kids)
	}

	form = url.Values{"csrf": {csrf}, "person_id": {"4"}}
	status, _ = post(t, e.admin, e.ts.URL+"/family/1/children/remove", form)
	if status != http.StatusFound {
		t.Fatalf("remove child: %d", status)
	}

	// 5. Role gate: viewer must not POST edits.
	form = url.Values{"csrf": {csrf}, "person_id": {"4"}}
	status, _ = post(t, e.viewer, e.ts.URL+"/family/1/children/add", form)
	if status != http.StatusForbidden {
		t.Fatalf("viewer family edit should 403, got %d", status)
	}
}

func TestFamilyEditAndDelete(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/family/1/edit")

	// Update family details.
	form := url.Values{
		"csrf":          {csrf},
		"partner1_id":   {"1"},
		"partner2_id":   {"2"},
		"status":        {"divorced"},
		"marriage_date": {"1875"},
	}
	status, _ := post(t, e.admin, e.ts.URL+"/family/1/edit", form)
	if status != http.StatusFound {
		t.Fatalf("family update: %d", status)
	}
	f, err := e.St.Queries().FamilyByID(context.Background(), 1)
	if err != nil || f.Status != models.FamilyDivorced || f.GedcomXref != "@F1@" {
		t.Fatalf("family after edit: %+v %v", f, err)
	}

	// Delete (admin, must confirm).
	form = url.Values{"csrf": {csrf}, "confirm": {"no"}}
	status, _ = post(t, e.admin, e.ts.URL+"/family/1/delete", form)
	if status != http.StatusBadRequest {
		t.Fatalf("delete without confirm should 400, got %d", status)
	}
	form.Set("confirm", "yes")
	status, body := post(t, e.admin, e.ts.URL+"/family/1/delete", form)
	if status != http.StatusFound {
		t.Fatalf("delete confirmed: %d %s", status, body)
	}
	if _, err := e.St.Queries().FamilyByID(context.Background(), 1); err == nil {
		t.Fatal("family should be gone")
	}

	// Viewer cannot delete (role gate — plain post).
	e2 := newEnv(t, "../../testdata/gedcom/minimal.ged")
	form = url.Values{"confirm": {"yes"}}
	status, _ = post(t, e2.viewer, e2.ts.URL+"/family/1/delete", form)
	if status != http.StatusForbidden {
		t.Fatalf("viewer delete should 403, got %d", status)
	}
}

func TestPersonDeleteAdminOnly(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")

	form := url.Values{"csrf": {csrf}, "confirm": {"yes"}}
	status, _ := post(t, e.admin, e.ts.URL+"/person/1/delete", form)
	if status != http.StatusFound {
		t.Fatalf("admin delete person: %d", status)
	}
	if _, err := e.St.Queries().PersonByID(context.Background(), 1); err == nil {
		t.Fatal("person 1 should be deleted")
	}

	// viewer denied (role gate hits before csrf, so a plain post suffices)
	e2 := newEnv(t, "../../testdata/gedcom/minimal.ged")
	form = url.Values{"confirm": {"yes"}}
	status, _ = post(t, e2.viewer, e2.ts.URL+"/person/1/delete", form)
	if status != http.StatusForbidden {
		t.Fatalf("viewer delete should 403, got %d", status)
	}
}
