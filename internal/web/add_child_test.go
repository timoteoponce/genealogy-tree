package web_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestAddChildFlow covers /person/{id}/child/new: create a new child and link
// it into the parent's family.
func TestAddChildFlow(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	status, body := get(t, e.admin, e.ts.URL+"/person/1/child/new")
	if status != http.StatusOK || !strings.Contains(body, "Add child of John Smith") {
		t.Fatalf("add child form: %d %s", status, body)
	}
	if !strings.Contains(body, `data-picker="person_id"`) || !strings.Contains(body, "people-datalist") {
		t.Fatalf("add child form missing name search: %s", body)
	}

	csrf := csrfFrom(t, e, e.admin, "/person/1/child/new")
	form := url.Values{
		"csrf":          {csrf},
		"person":        {"1"},
		"family_id":     {"1"},
		"child_given":   {"Lucy"},
		"child_surname": {"Smith"},
		"relationship":  {"birth"},
	}
	status, loc, _ := postLocation(t, e.admin, e.ts.URL+"/person/1/child/new", form)
	if status != http.StatusFound || !strings.Contains(loc, "/person/4") {
		t.Fatalf("add child post: %d %q", status, loc)
	}

	kids, _ := e.St.Queries().ChildrenOfFamily(context.Background(), 1)
	found := false
	for _, k := range kids {
		if k.ID == 4 && k.GivenNames == "Lucy" {
			found = true
		}
	}
	if !found {
		t.Fatalf("new child not linked to family 1: %+v", kids)
	}
}

// TestSetParentAttachFlow: a child with only one parent gets the missing
// parent attached into the existing family (not a duplicate family).
func TestSetParentAttachFlow(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	// First create the person to attach a parent to.
	create := url.Values{"csrf": {csrfFrom(t, e, e.admin, "/people")}, "given_names": {"Martha"}, "surname": {"Long"}, "sex": {"F"}}
	status, loc, _ := postLocation(t, e.admin, e.ts.URL+"/person/new", create)
	if status != http.StatusFound || !strings.Contains(loc, "/person/4") {
		t.Fatalf("create person: %d %q", status, loc)
	}

	// Add only a mother to Martha.
	csrf := csrfFrom(t, e, e.admin, "/family/add?child=4")
	form := url.Values{"csrf": {csrf}, "child": {"4"}, "mother_given": {"Ilsa"}}
	status, loc, _ = postLocation(t, e.admin, e.ts.URL+"/family/add", form)
	if status != http.StatusFound || !strings.Contains(loc, "/family/") {
		t.Fatalf("add mother: %d %q", status, loc)
	}

	// Now "add parent" should offer attach mode (free father slot).
	_, body := get(t, e.admin, e.ts.URL+"/family/add?child=4")
	if !strings.Contains(body, "Add father of Martha Long") {
		t.Fatalf("expected attach-mode form (add father), got: %s", body)
	}

	// Post a father into the free slot using the role-based fields.
	csrf = csrfFrom(t, e, e.admin, "/family/add?child=4")
	attach := url.Values{
		"csrf":           {csrf},
		"child":          {"4"},
		"family_id":      {extractHidden(body, "family_id")},
		"slot":           {extractHidden(body, "slot")},
		"role":           {extractHidden(body, "role")},
		"father_given":   {"Peter"},
		"father_surname": {"Long"},
	}
	status, _, _ = postLocation(t, e.admin, e.ts.URL+"/family/add", attach)
	if status != http.StatusFound {
		t.Fatalf("attach father: %d", status)
	}

	// The child's parent family now has both parents, and there is no
	// duplicate family (still one family for Martha as child).
	fams, _ := e.St.Queries().FamiliesOfChild(context.Background(), 4)
	if len(fams) != 1 {
		t.Fatalf("expected one parent family, got %d", len(fams))
	}
	if fams[0].Partner1ID == nil || fams[0].Partner2ID == nil {
		t.Fatalf("expected both parents filled, got %+v", fams[0])
	}
}

func extractHidden(body, name string) string {
	i := strings.Index(body, `name="`+name+`" value="`)
	if i < 0 {
		return ""
	}
	rest := body[i+len(`name="`+name+`" value="`):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// TestAddSiblingFlow: /person/{id}/sibling/new adds a child to the parent's
// family.
func TestAddSiblingFlow(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	// Samuel (3) has parents in family 1.
	status, body := get(t, e.admin, e.ts.URL+"/person/3/sibling/new")
	if status != http.StatusOK || !strings.Contains(body, "Add sibling of Samuel Smith") {
		t.Fatalf("add sibling form: %d %s", status, body)
	}

	csrf := csrfFrom(t, e, e.admin, "/person/3/sibling/new")
	form := url.Values{
		"csrf":          {csrf},
		"person":        {"3"},
		"family_id":     {"1"},
		"child_given":   {"Ann"},
		"child_surname": {"Smith"},
		"relationship":  {"birth"},
	}
	status, loc, _ := postLocation(t, e.admin, e.ts.URL+"/person/3/sibling/new", form)
	if status != http.StatusFound || !strings.Contains(loc, "/person/4") {
		t.Fatalf("add sibling post: %d %q", status, loc)
	}

	kids, _ := e.St.Queries().ChildrenOfFamily(context.Background(), 1)
	found := false
	for _, k := range kids {
		if k.ID == 4 && k.GivenNames == "Ann" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sibling not linked to family 1: %+v", kids)
	}
}
