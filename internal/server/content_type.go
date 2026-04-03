package server

import (
	"net/http"
	"strings"
)

func rejectUnlessJSONContentType(w http.ResponseWriter, r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeAPIError(w, http.StatusUnsupportedMediaType, "invalid_request", "expected Content-Type: application/json")
		return true
	}
	return false
}
