package web_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
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
	if m.Mime != "image/jpeg" || !m.IsProfile {
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

func TestPhotoControlsWithExistingPhotos(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")
	for range 2 {
		status, _ := uploadMultipart(t, e, e.admin, "/person/1/media", url.Values{"csrf": {csrf}}, "p.png", testPNGBytes(t))
		if status != http.StatusFound {
			t.Fatalf("upload: %d", status)
		}
	}
	media, err := e.St.Queries().ListMediaForPerson(context.Background(), 1)
	if err != nil || len(media) != 2 {
		t.Fatalf("media list: %v %v", media, err)
	}
	_, onPage := get(t, e.admin, e.ts.URL+"/person/1")
	if !strings.Contains(onPage, `action="/person/1/media"`) {
		t.Fatal("upload form missing when a photo already exists")
	}
	if !strings.Contains(onPage, `action="/media/`+itoa64(media[1].ID)+`/profile"`) {
		t.Fatal("set portrait action missing for second photo")
	}
	_, viewerPage := get(t, e.viewer, e.ts.URL+"/person/1")
	if strings.Contains(viewerPage, `action="/person/1/media"`) {
		t.Fatal("upload form visible to viewer")
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
	if err != nil || upgraded.ImportNote || upgraded.Mime != "image/jpeg" {
		t.Fatalf("upgrade failed: %+v %v", upgraded, err)
	}

	// Blob now serves and profile auto-css assigned.
	d, _ := e.St.Queries().MediaData(context.Background(), placeholderID)
	if len(d.Data) == 0 || len(d.Thumb) == 0 {
		t.Fatal("upgraded row must carry blobs")
	}
	if upgraded.Size != int64(len(d.Data)) || !bytes.HasPrefix(d.Data, []byte{0xff, 0xd8}) {
		t.Fatalf("upgraded photo metadata or data invalid: %+v", upgraded)
	}
	if !upgraded.IsProfile {
		t.Error("first attached photo should become the profile")
	}
}

func TestPhotoProcessing(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")
	img := image.NewRGBA(image.Rect(0, 0, 2200, 400))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 31)
	}
	var original bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&original, img); err != nil {
		t.Fatal(err)
	}
	status, body := uploadMultipart(t, e, e.admin, "/person/1/media", url.Values{"csrf": {csrf}}, "large.png", original.Bytes())
	if status != http.StatusFound {
		t.Fatalf("upload: %d %s", status, body)
	}
	media, err := e.St.Queries().ListMediaForPerson(context.Background(), 1)
	if err != nil || len(media) != 1 {
		t.Fatalf("media list: %v %v", media, err)
	}
	d, err := e.St.Queries().MediaData(context.Background(), media[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(d.Data))
	if err != nil || config.Width != 2048 || config.Height > 2048 || media[0].Size != int64(len(d.Data)) {
		t.Fatalf("processed photo: %+v size=%d err=%v", config, media[0].Size, err)
	}
	if len(d.Data) >= original.Len() {
		t.Fatalf("processed photo not smaller: %d vs %d", len(d.Data), original.Len())
	}
	thumb, err := jpeg.DecodeConfig(bytes.NewReader(d.Thumb))
	if err != nil || thumb.Width != 256 || thumb.Height != 256 {
		t.Fatalf("thumbnail: %+v %v", thumb, err)
	}
	resp, err := e.admin.Get(e.ts.URL + "/media/" + itoa64(media[0].ID) + "/full")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("full photo content type: %s", resp.Header.Get("Content-Type"))
	}
}

func TestSmallPhotoProcessing(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")
	status, body := uploadMultipart(t, e, e.admin, "/person/1/media", url.Values{"csrf": {csrf}}, "small.png", testPNGBytes(t))
	if status != http.StatusFound {
		t.Fatalf("upload: %d %s", status, body)
	}
	media, err := e.St.Queries().ListMediaForPerson(context.Background(), 1)
	if err != nil || len(media) != 1 {
		t.Fatalf("media list: %v %v", media, err)
	}
	data, err := e.St.Queries().MediaData(context.Background(), media[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(data.Data))
	if err != nil || config.Width != 64 || config.Height != 80 || media[0].Size != int64(len(data.Data)) {
		t.Fatalf("small photo: %+v size=%d err=%v", config, media[0].Size, err)
	}
}

func TestTransparentGIFUpload(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")
	img := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Transparent})
	var original bytes.Buffer
	if err := gif.Encode(&original, img, nil); err != nil {
		t.Fatal(err)
	}
	status, body := uploadMultipart(t, e, e.admin, "/person/1/media", url.Values{"csrf": {csrf}}, "transparent.gif", original.Bytes())
	if status != http.StatusFound {
		t.Fatalf("upload: %d %s", status, body)
	}
	media, _ := e.St.Queries().ListMediaForPerson(context.Background(), 1)
	d, err := e.St.Queries().MediaData(context.Background(), media[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	photo, err := jpeg.Decode(bytes.NewReader(d.Data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := photo.At(0, 0).RGBA()
	if r < 0xf000 || g < 0xf000 || b < 0xf000 {
		t.Fatalf("transparent pixels should be white: %x %x %x", r, g, b)
	}
}

func TestPhotoOrientation(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")
	var original bytes.Buffer
	if err := jpeg.Encode(&original, image.NewRGBA(image.Rect(0, 0, 4, 2)), nil); err != nil {
		t.Fatal(err)
	}
	exif := []byte{
		0xff, 0xe1, 0x00, 0x22, 'E', 'x', 'i', 'f', 0, 0,
		'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1,
		1, 0x12, 0, 3, 0, 0, 0, 1, 0, 6, 0, 0, 0, 0, 0, 0,
	}
	data := append(append(append([]byte{}, original.Bytes()[:2]...), exif...), original.Bytes()[2:]...)
	status, body := uploadMultipart(t, e, e.admin, "/person/1/media", url.Values{"csrf": {csrf}}, "rotated.jpg", data)
	if status != http.StatusFound {
		t.Fatalf("upload: %d %s", status, body)
	}
	media, _ := e.St.Queries().ListMediaForPerson(context.Background(), 1)
	stored, err := e.St.Queries().MediaData(context.Background(), media[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(stored.Data))
	if err != nil || config.Width != 2 || config.Height != 4 {
		t.Fatalf("orientation not applied: %+v %v", config, err)
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

	// A small encoded file can claim an excessive number of decoded pixels.
	huge := testPNGBytes(t)
	binary.BigEndian.PutUint32(huge[16:20], 10000)
	binary.BigEndian.PutUint32(huge[20:24], 4001)
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29]))
	status, body = uploadMultipart(t, e, e.admin, "/person/1/media", form, "huge.png", huge)
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "40 megapixels") {
		t.Fatalf("excessive image dimensions rejected: %d %s", status, body)
	}
	status, _ = uploadMultipart(t, e, e.admin, "/person/1/media", form, "huge.png", make([]byte, 10<<20))
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload rejected: %d", status)
	}

	// Viewer denied
	status, _ = uploadMultipart(t, e, e.viewer, "/person/1/media", url.Values{"csrf": {"x"}}, "portrait.png", testPNGBytes(t))
	if status != http.StatusForbidden {
		t.Fatalf("viewer upload should 403, got %d", status)
	}
}

func TestStreamingUploadLimit(t *testing.T) {
	e := newEnv(t, "../../testdata/gedcom/minimal.ged")
	csrf := csrfFrom(t, e, e.admin, "/person/1/edit")
	var body bytes.Buffer
	body.WriteString("--boundary\r\nContent-Disposition: form-data; name=\"csrf\"\r\n\r\n" + csrf + "\r\n")
	body.WriteString("--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"large.png\"\r\n\r\n")
	body.Write(make([]byte, 10<<20))
	body.WriteString("\r\n--boundary--\r\n")
	req, err := http.NewRequest(http.MethodPost, e.ts.URL+"/person/1/media", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
	req.ContentLength = -1
	resp, err := e.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("streaming oversized upload: %d", resp.StatusCode)
	}
	media, err := e.St.Queries().ListMediaForPerson(context.Background(), 1)
	if err != nil || len(media) != 0 {
		t.Fatalf("oversized upload stored media: %v %v", media, err)
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
