package web

import (
	"encoding/json"
	"net/http"
)

// WriteJSON is a tiny helper for JSON endpoints.
func WriteJSON(w http.ResponseWriter, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
