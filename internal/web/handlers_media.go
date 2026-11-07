package web

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // register decoders for image.Decode
	_ "image/jpeg" // (upload sniffing covers these types)
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/disintegration/imaging"

	"genealogy-tree/internal/models"
)

// maxUploadBytes caps one photo upload.
const maxUploadBytes int64 = 10 << 20 // 10 MB

// thumbSize side of the generated square-cropped JPEG.
const thumbSize = 256

// personMediaUpload handles POST /person/{id}/media (multipart upload).
// An optional "media_id" field upgrades an import placeholder row (GEDCOM
// OBJE reference) instead of inserting a new one.
func (s *Server) personMediaUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	personID, err := parseID(r)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, s.tr(r, "Unknown person."))
		return
	}
	q := qOf(s, r)
	if _, err := q.PersonByID(ctx, personID); err != nil {
		s.renderError(w, r, http.StatusNotFound, s.tr(r, "That person does not exist."))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		s.renderError(w, r, http.StatusRequestEntityTooLarge,
			"Upload too large (limit 10 MB).")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.renderError(w, r, http.StatusUnprocessableEntity, s.tr(r, "No file in the upload."))
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, s.tr(r, "Could not read the upload."))
		return
	}
	mediaID := formID(r, "media_id")

	m, thumb, msg, ok := processUpload(data, header.Filename, formVal(r, "caption"), mediaID != 0)
	if !ok {
		s.renderError(w, r, http.StatusUnprocessableEntity, msg)
		return
	}

	if mediaID > 0 {
		// Upgrade an object-placeholder row (keeps its linkage/caption).
		if _, err := q.MediaByID(ctx, mediaID); err != nil {
			s.renderError(w, r, http.StatusNotFound, s.tr(r, "That media row does not exist."))
			return
		}
		if err := q.UpgradeMedia(ctx, mediaID, m.Filename, m.Mime, len(data), thumb, data); err != nil {
			s.renderError(w, r, http.StatusInternalServerError, "Could not attach photo: "+err.Error())
			return
		}
		if err := q.SetProfilePhoto(ctx, personID, mediaID); err != nil {
			s.renderError(w, r, http.StatusInternalServerError, s.tr(r, "Photo attached but profile marking failed."))
			return
		}
		q.InsertAuditLog(ctx, s.St.DB, userIDof(r), "attach-photo", "media", &mediaID, m.Filename)
		http.Redirect(w, r, "/person/"+strconv.FormatInt(personID, 10), http.StatusFound)
		return
	}

	m.PersonID = &personID
	if cnt, _ := q.ListMediaForPerson(ctx, personID); len(cnt) == 0 {
		m.IsProfile = true // first photo becomes the profile automatically
	}
	id, err := q.InsertMedia(ctx, &m, data, thumb)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not save photo: "+err.Error())
		return
	}
	q.InsertAuditLog(ctx, s.St.DB, userIDof(r), "upload-photo", "person", &personID, fmt.Sprintf("media %d", id))
	http.Redirect(w, r, "/person/"+strconv.FormatInt(personID, 10), http.StatusFound)
}

// processUpload validates and decodes an upload into a media row payload.
func processUpload(data []byte, filename, caption string, isPlaceholderUpgrade bool) (models.Media, []byte, string, bool) {
	if len(data) == 0 {
		return models.Media{}, nil, "Empty file.", false
	}
	sniff := http.DetectContentType(data)
	var mime string
	switch {
	case strings.HasPrefix(sniff, "image/jpeg"):
		mime = "image/jpeg"
	case strings.HasPrefix(sniff, "image/png"):
		mime = "image/png"
	case strings.HasPrefix(sniff, "image/gif"):
		mime = "image/gif"
	case strings.HasPrefix(sniff, "image/webp"):
		return models.Media{}, nil, "WebP photos are not supported yet — use JPEG/PNG.", false
	default:
		return models.Media{}, nil, "Not an image (JPEG/PNG/GIF only).", false
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return models.Media{}, nil, "Corrupt image: " + err.Error(), false
	}
	thumbImage := imaging.Thumbnail(img, thumbSize, thumbSize, imaging.Lanczos)
	var thumbBuf bytes.Buffer
	if err := imaging.Encode(&thumbBuf, thumbImage, imaging.JPEG); err != nil {
		return models.Media{}, nil, "Thumbnail generation failed.", false
	}

	m := models.Media{
		Filename: sanitizeFilename(filename),
		Mime:     mime,
		Size:     int64(len(data)),
		Caption:  strings.TrimSpace(caption),
	}
	return m, thumbBuf.Bytes(), "", true
}

// sanitizeFilename keeps the basename and strips paths/control chars.
func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(c rune) rune {
		if c < 32 || c > 126 {
			return -1
		}
		return c
	}, name)
	if name == "" {
		return "upload"
	}
	return name
}

// mediaSetProfile handles POST /media/{id}/profile (editor).
func (s *Server) mediaSetProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	m, err := qOf(s, r).MediaByID(ctx, id)
	if err != nil || m.PersonID == nil {
		s.renderError(w, r, http.StatusNotFound, s.tr(r, "That photo does not exist."))
		return
	}
	if err := qOf(s, r).SetProfilePhoto(ctx, *m.PersonID, id); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	qOf(s, r).InsertAuditLog(ctx, s.St.DB, userIDof(r), "profile-photo", "media", &id, "")
	http.Redirect(w, r, "/person/"+strconv.FormatInt(*m.PersonID, 10), http.StatusFound)
}

// mediaCaption handles POST /media/{id}/caption (editor).
func (s *Server) mediaCaption(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	m, err := qOf(s, r).MediaByID(ctx, id)
	if err != nil || m.PersonID == nil {
		s.renderError(w, r, http.StatusNotFound, s.tr(r, "That photo does not exist."))
		return
	}
	if err := qOf(s, r).UpdateMediaCaption(ctx, id, formVal(r, "caption")); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/person/"+strconv.FormatInt(*m.PersonID, 10), http.StatusFound)
}

// mediaDelete handles POST /media/{id}/delete (editor).
func (s *Server) mediaDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	m, err := qOf(s, r).MediaByID(ctx, id)
	if err != nil || m.PersonID == nil {
		s.renderError(w, r, http.StatusNotFound, s.tr(r, "That photo does not exist."))
		return
	}
	if err := qOf(s, r).DeleteMedia(ctx, id); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	qOf(s, r).InsertAuditLog(ctx, s.St.DB, userIDof(r), "delete-photo", "media", &id, m.Filename)
	http.Redirect(w, r, "/person/"+strconv.FormatInt(*m.PersonID, 10), http.StatusFound)
}
