package web_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestAdminRoleGate(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	// Viewer blocked from all admin pages.
	for _, path := range []string{"/admin", "/admin/users", "/admin/import", "/admin/settings", "/admin/audit"} {
		status, _ := get(t, e.viewer, e.ts.URL+path)
		if status != http.StatusForbidden {
			t.Fatalf("viewer at %s got %d, want 403", path, status)
		}
	}
	// Admin pages open fine.
	for _, path := range []string{"/admin", "/admin/users", "/admin/import", "/admin/settings", "/admin/audit"} {
		status, body := get(t, e.admin, e.ts.URL+path)
		if status != http.StatusOK || len(body) < 200 {
			t.Fatalf("admin page %s: %d len=%d", path, status, len(body))
		}
	}
	// Anonymous /admin redirects to login.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noFollow.Get(e.ts.URL + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || !strings.Contains(resp.Header.Get("Location"), "/login") {
		t.Fatalf("anonymous /admin: got %d → %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestAdminUserManagement(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/admin/users")

	// Create editor + viewer accounts.
	form := url.Values{
		"csrf":     {csrf},
		"username": {"family.editor"},
		"password": {"long-enough-pw"},
		"role":     {"editor"},
	}
	status, _ := post(t, e.admin, e.ts.URL+"/admin/users", form)
	if status != http.StatusOK {
		t.Fatalf("create user: %d", status)
	}
	if _, err := e.St.Queries().UserByUsername(context.Background(), "family.editor"); err != nil {
		t.Fatalf("user should exist: %v", err)
	}

	// Wrong password rejected with a message (page stays 200 for fix flow).
	form = url.Values{"csrf": {csrf}, "username": {"bad"}, "password": {"short"}, "role": {"viewer"}}
	status, body := post(t, e.admin, e.ts.URL+"/admin/users", form)
	if status != http.StatusOK || !strings.Contains(body, "at least 8") {
		t.Fatalf("short password handling: %d", status)
	}

	// Self-demote guard: admin (id 1) cannot demote himself.
	form = url.Values{"csrf": {csrf}, "role": {"viewer"}}
	status, body = post(t, e.admin, e.ts.URL+"/admin/users/1/role", form)
	if status != http.StatusOK || !strings.Contains(body, "cannot demote yourself") {
		t.Fatalf("self demote guard: %d %s", status, body)
	}
	// Role update for the other user.
	form = url.Values{"csrf": {csrf}, "role": {"editor"}}
	status, body = post(t, e.admin, e.ts.URL+"/admin/users/2/role", form)
	if status != http.StatusOK || !strings.Contains(body, "Role updated") {
		t.Fatalf("role update: %d %s", status, body)
	}

	// Password set for user 2, then login works.
	form = url.Values{"csrf": {csrf}, "password": {"brand-new-pw"}}
	status, body = post(t, e.admin, e.ts.URL+"/admin/users/2/password", form)
	if status != http.StatusOK || !strings.Contains(body, "Password updated") {
		t.Fatalf("set password: %d %s", status, body)
	}
	c2 := e.clientWithLogin(t, "viewer", "brand-new-pw")
	status, _ = get(t, c2, e.ts.URL+"/people")
	if status != http.StatusOK {
		t.Fatalf("login with new password: %d", status)
	}

	// Delete user 2; you cannot delete yourself.
	form = url.Values{"csrf": {csrf}}
	status, body = post(t, e.admin, e.ts.URL+"/admin/users/1/delete", form)
	if status != http.StatusOK || !strings.Contains(body, "cannot delete your own") {
		t.Fatalf("self delete guard: %d %s", status, body)
	}
	status, _ = post(t, e.admin, e.ts.URL+"/admin/users/2/delete", form)
	if status != http.StatusOK {
		t.Fatalf("delete user: %d", status)
	}
	if _, err := e.St.Queries().UserByUsername(context.Background(), "viewer"); err == nil {
		t.Fatal("user should be deleted")
	}
}

func TestAdminSettingsAndSiteName(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/admin/settings")

	form := url.Values{"csrf": {csrf}, "site_name": {"Worley Family"}}
	status, body := post(t, e.admin, e.ts.URL+"/admin/settings", form)
	if status != http.StatusOK || !strings.Contains(body, "Saved.") {
		t.Fatalf("settings: %d %s", status, body)
	}
	// Layout shows the new name everywhere.
	_, dash := get(t, e.admin, e.ts.URL+"/")
	if !strings.Contains(dash, "Worley Family") {
		t.Fatalf("site name not applied: %s", dash[:min(500, len(dash))])
	}

	// Empty name rejected.
	form.Set("site_name", "")
	status, body = post(t, e.admin, e.ts.URL+"/admin/settings", form)
	if status != http.StatusOK || !strings.Contains(body, "1-60") {
		t.Fatalf("empty site name: %d %s", status, body)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestAdminWebImport(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/admin/import")

	gedBytes, err := osReadFile(t, "../../testdata/gedcom/edge.ged")
	if err != nil {
		t.Fatal(err)
	}

	// Build multipart upload (ged, not an image).
	var buf bytes.Buffer
	boundary := "----gedboundary9"
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Disposition: form-data; name=\"csrf\"\r\n\r\n" + csrf + "\r\n")
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"edge.ged\"\r\n")
	buf.WriteString("Content-Type: application/octet-stream\r\n\r\n")
	buf.Write(gedBytes)
	buf.WriteString("\r\n--" + boundary + "--\r\n")

	req, err := http.NewRequest(http.MethodPost, e.ts.URL+"/admin/import", bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	resp, err := e.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	BODY, _ := io.ReadAll(resp.Body)
	body := string(BODY)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Import report") {
		t.Fatalf("import page: %d", resp.StatusCode)
	}
	// Edge fixture has 2 people; the tree must have been replaced.
	people, total, _ := e.St.Queries().SearchPeople(context.Background(), "", 100, 0)
	if total != 2 || len(people) != 2 {
		t.Fatalf("people after import: %d", total)
	}

	// Audit page renders (system actions recorded during import).
	status, auditBody := get(t, e.admin, e.ts.URL+"/admin/audit")
	if status != http.StatusOK || len(auditBody) < 200 {
		t.Fatalf("audit page: %d", status)
	}
}

func osReadFile(t *testing.T, path string) ([]byte, error) {
	t.Helper()
	return os.ReadFile(path)
}
