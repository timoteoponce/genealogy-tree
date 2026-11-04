package web_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// testPNGBytes renders a small PNG for uploads.
func testPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: 90, B: uint8(y * 3), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// uploadMultipart builds a multipart/form-data POST body for uploads.
func uploadMultipart(t *testing.T, e *env, client *http.Client, path string, fields url.Values, filename string, filedata []byte) (int, string) {
	t.Helper()
	// Build multipart manually (stdlib mime/multipart).
	var body bytes.Buffer
	boundary := "----testjsonboundary42"
	body.WriteString("--" + boundary + "\r\n")
	for k, v := range fields {
		body.WriteString("Content-Disposition: form-data; name=\"" + k + "\"\r\n\r\n" + v[0] + "\r\n")
		body.WriteString("--" + boundary + "\r\n")
	}
	body.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"" + filename + "\"\r\n")
	body.WriteString("Content-Type: image/png\r\n\r\n")
	body.Write(filedata)
	body.WriteString("\r\n--" + boundary + "--\r\n")

	req, err := http.NewRequest(http.MethodPost, e.ts.URL+path, bytes.NewReader(body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("upload %s: %v", path, err)
	}
	defer resp.Body.Close()
	b2, err := ioReadAll(t, resp)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, b2
}

// ioReadAll mirrors get()'s body reading for responses.
func ioReadAll(t *testing.T, resp *http.Response) (string, error) {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			return sb.String(), nil // ignore EOF
		}
	}
}

func TestPhotoUploadAndProfile(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")

	pngData := testPNGBytes(t)
	form := url.Values{"csrf": {csrf}, "caption": {"At the forge"}}
	status, body := uploadMultipart(t, e, e.admin, "/person/1/media", form, "portrait.png", pngData)
	if status != http.StatusFound {
		t.Fatalf("upload: %d %s", status, body)
	}

	media, err := e.St.Queries().ListMediaForPerson(context.Background(), 1)
	if err != nil || len(media) != 1 {
		t.Fatalf("media list: %v %v", media, err)
	}
	m := media[0]
	if m.Mime != "image/png" || m.Size != int64(len(pngData)) || !m.IsProfile {
		t.Fatalf("media row: %+v", m)
	}
	if m.Caption != "At the forge" {
		t.Errorf("caption: %q", m.Caption)
	}

	// Thumb: generated as JPEG, small dimensions.
	d, err := e.St.Queries().MediaData(context.Background(), m.ID)
	if err != nil || len(d.Thumb) == 0 || !bytes.HasPrefix(d.Thumb, []byte{0xFF, 0xD8}) {
		t.Fatalf("thumb: %d bytes err %v", len(d.Thumb), err)
	}

	// Serving endpoints.
	status, _ = get(t, e.admin, e.ts.URL+"/media/"+itoa64(m.ID)+"/thumb")
	if status != http.StatusOK {
		t.Fatalf("thumb serve: %d", status)
	}
	status, _ = get(t, e.admin, e.ts.URL+"/media/"+itoa64(m.ID)+"/full")
	if status != http.StatusOK {
		t.Fatalf("full serve: %d", status)
	}

	// Person page shows the gallery with caption.
	status, onPage := get(t, e.admin, e.ts.URL+"/person/1")
	if status != http.StatusOK || !strings.Contains(onPage, "At the forge") ||
		!strings.Contains(onPage, "portrait") {
		t.Fatalf("gallery missing: %d", status)
	}

	// Second upload: not auto-profile.
	form2 := url.Values{"csrf": {csrf}}
	status, _ = uploadMultipart(t, e, e.admin, "/person/1/media", form2, "second.png", pngData)
	if status != http.StatusFound {
		t.Fatalf("second upload: %d", status)
	}
	media2, _ := e.St.Queries().ListMediaForPerson(context.Background(), 1)
	if len(media2) != 2 || !media2[0].IsProfile || media2[0].ID != m.ID {
		// profile photo must stay first
		t.Fatalf("profile ordering broken: %+v (want first %d)", media2, m.ID)
	}
}

func TestPlaceholderUpgrade(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/myheritage.ged")
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")

	mediaList, _ := e.St.Queries().ListMediaForPerson(context.Background(), 1)
	if len(mediaList) != 2 || !mediaList[0].ImportNote {
		t.Fatalf("placeholders expected: %+v", mediaList)
	}
	placeholderID := mediaList[0].ID

	form := url.Values{"csrf": {csrf}, "media_id": {itoa64(placeholderID)}}
	status, _ := uploadMultipart(t, e, e.admin, "/person/1/media", form, "wedding.png", testPNGBytes(t))
	if status != http.StatusFound {
		t.Fatalf("placeholder upgrade: %d", status)
	}
	upgraded, err := e.St.Queries().MediaByID(context.Background(), placeholderID)
	if err != nil || upgraded.ImportNote || upgraded.Mime != "image/png" {
		t.Fatalf("upgrade failed: %+v %v", upgraded, err)
	}

	// Blob now serves and profile auto-css assigned.
	d, _ := e.St.Queries().MediaData(context.Background(), placeholderID)
	if len(d.Data) == 0 || len(d.Thumb) == 0 {
		t.Fatal("upgraded row must carry blobs")
	}
	if !upgraded.IsProfile {
		t.Error("first attached photo should become the profile")
	}
}

func TestUploadRejections(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")

	// Not an image
	form := url.Values{"csrf": {csrf}}
	status, body := uploadMultipart(t, e, e.admin, "/person/1/media", form, "text.txt", []byte("plain text here"))
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Not an image") {
		t.Fatalf("non-image rejected: %d %s", status, body)
	}

	// Corrupt PNG with image/content-type
	corrupt := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01, 0x02}
	status, body = uploadMultipart(t, e, e.admin, "/person/1/media", form, "bad.png", corrupt)
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Corrupt") {
		t.Fatalf("corrupt image rejected: %d %s", status, body)
	}

	// Viewer denied
	status, _ = uploadMultipart(t, e, e.viewer, "/person/1/media", url.Values{"csrf": {"x"}}, "portrait.png", testPNGBytes(t))
	if status != http.StatusForbidden {
		t.Fatalf("viewer upload should 403, got %d", status)
	}
}

func TestMediaProfileCaptionDelete(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	// Seed two photos directly via the store.
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")
	data := testPNGBytes(t)
	for range []int{0, 1} {
		form := url.Values{"csrf": {csrf}}
		status, _ := uploadMultipart(t, e, e.admin, "/person/1/media", form, "p.png", data)
		if status != http.StatusFound {
			t.Fatal("seed upload failed")
		}
	}
	mediaList, _ := e.St.Queries().ListMediaForPerson(context.Background(), 1)
	if len(mediaList) != 2 {
		t.Fatalf("seed failed: %+v", mediaList)
	}
	second := mediaList[1].ID

	// Set second as profile.
	form := url.Values{"csrf": {csrf}}
	status, _ := post(t, e.admin, e.ts.URL+"/media/"+itoa64(second)+"/profile", form)
	if status != http.StatusFound {
		t.Fatalf("set profile: %d", status)
	}
	if got, _ := e.St.Queries().ProfilePhotoID(context.Background(), 1); got != second {
		t.Fatalf("profile photo id = %d want %d", got, second)
	}

	// Caption update.
	form.Set("caption", "Second portrait")
	status, _ = post(t, e.admin, e.ts.URL+"/media/"+itoa64(second)+"/caption", form)
	if status != http.StatusFound {
		t.Fatalf("caption: %d", status)
	}
	upd, _ := e.St.Queries().MediaByID(context.Background(), second)
	if upd.Caption != "Second portrait" {
		t.Fatalf("caption not saved: %+v", upd)
	}

	// Delete second.
	form.Del("caption")
	status, _ = post(t, e.admin, e.ts.URL+"/media/"+itoa64(second)+"/delete", form)
	if status != http.StatusFound {
		t.Fatalf("delete: %d", status)
	}
	after, _ := e.St.Queries().ListMediaForPerson(context.Background(), 1)
	if len(after) != 1 {
		t.Fatalf("after delete: %+v", after)
	}

	// Viewer rejections.
	status, _ = post(t, e.viewer, e.ts.URL+"/media/"+itoa64(mediaList[0].ID)+"/delete", url.Values{"csrf": {"x"}})
	if status != http.StatusForbidden {
		t.Fatalf("viewer media delete: %d", status)
	}
}

// helpers
func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}
