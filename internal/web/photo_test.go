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
	resp, err := e.admin.Get(e.ts.URL + "/person/1/thumb?v=2")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := ioReadAll(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "<svg") {
		t.Fatalf("placeholder avatar: %d (len %d)", resp.StatusCode, len(body))
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("placeholder cache policy: %q", resp.Header.Get("Cache-Control"))
	}

	// Unknown person → 404.
	status, _ := get(t, e.admin, e.ts.URL+"/person/999/thumb")
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

	resp, err = e.admin.Get(e.ts.URL + "/person/1/thumb?v=2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/jpeg") {
		t.Fatalf("profile thumb: status %d, ctype %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("profile cache policy: %q", resp.Header.Get("Cache-Control"))
	}
}

// TestTreeJSONAvatarURL ensures the chart avatar points at the person photo
// endpoint (which always yields a real image), not at a media id.
func TestTreeJSONAvatarURL(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	_, body := get(t, e.admin, e.ts.URL+"/tree.json?person=1&gens=3")
	if !strings.Contains(body, `"avatar":"/person/1/thumb?v=2"`) {
		t.Fatalf("chart avatar should use /person/{id}/thumb: %s", body)
	}
}

func TestThumbnailURLsBypassLegacyCache(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	for _, path := range []string{"/", "/people", "/person/1", "/person/1/tree", "/fan.json?person=1", "/graph.json"} {
		t.Run(path, func(t *testing.T) {
			status, body := get(t, e.admin, e.ts.URL+path)
			if status != http.StatusOK || !strings.Contains(body, "/thumb?v=2") {
				t.Fatalf("missing cache-bypassing thumbnail URL: %d", status)
			}
			if strings.Contains(body, `/thumb"`) {
				t.Fatal("legacy thumbnail URL still rendered")
			}
		})
	}
}

func TestUpgradedThumbnailNotCached(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/myheritage.ged")
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")
	media, err := e.St.Queries().ListMediaForPerson(t.Context(), 1)
	if err != nil || len(media) == 0 {
		t.Fatalf("placeholder media: %v %v", media, err)
	}
	id := itoa64(media[0].ID)
	path := e.ts.URL + "/media/" + id + "/thumb?v=2"
	status, _ := get(t, e.admin, path)
	if status != http.StatusFound {
		t.Fatalf("placeholder thumbnail: %d", status)
	}
	status, body := uploadMultipart(t, e, e.admin, "/person/1/media",
		url.Values{"csrf": {csrf}, "media_id": {id}}, "photo.png", testPNGBytes(t))
	if status != http.StatusFound {
		t.Fatalf("upload: %d %s", status, body)
	}
	resp, err := e.admin.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("upgraded thumbnail: %d cache=%q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	_, body = get(t, e.admin, e.ts.URL+"/person/1")
	if !strings.Contains(body, "/media/"+id+"/thumb?v=2") {
		t.Fatal("gallery uses legacy thumbnail URL")
	}
}
