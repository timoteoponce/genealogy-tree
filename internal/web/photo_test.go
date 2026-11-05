package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestPersonPhotoPlaceholderAndProfile covers GET /person/{id}/thumb: the
// avatar never 404s — it returns a silhouette SVG when there is no photo,
// and the profile thumbnail once one is uploaded.
func TestPersonPhotoPlaceholderAndProfile(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")

	// No photo yet → silhouette SVG (200, image/svg+xml).
	status, body := get(t, e.admin, e.ts.URL+"/person/1/thumb")
	if status != http.StatusOK || !strings.Contains(body, "<svg") {
		t.Fatalf("placeholder avatar: %d (len %d)", status, len(body))
	}

	// Unknown person → 404.
	status, _ = get(t, e.admin, e.ts.URL+"/person/999/thumb")
	if status != http.StatusNotFound {
		t.Fatalf("unknown person avatar should 404, got %d", status)
	}

	// Upload a real photo → thumbnail served as JPEG.
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")
	form := url.Values{"csrf": {csrf}}
	status, _ = uploadMultipart(t, e, e.admin, "/person/1/media", form, "p.png", testPNGBytes(t))
	if status != http.StatusFound {
		t.Fatalf("seed upload: %d", status)
	}

	resp, err := e.admin.Get(e.ts.URL + "/person/1/thumb")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/jpeg") {
		t.Fatalf("profile thumb: status %d, ctype %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

// TestTreeJSONAvatarURL ensures the chart avatar points at the person photo
// endpoint (which always yields a real image), not at a media id.
func TestTreeJSONAvatarURL(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	_, body := get(t, e.admin, e.ts.URL+"/tree.json?person=1&gens=3")
	if !strings.Contains(body, `"avatar":"/person/1/thumb"`) {
		t.Fatalf("chart avatar should use /person/{id}/thumb: %s", body)
	}
}
