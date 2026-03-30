package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/apple", handleAuthApple)
	mux.HandleFunc("POST /api/v1/auth/google", handleAuthGoogle)
	mux.HandleFunc("GET /api/v1/me", handleAuthMe)
}

type appleAuthRequest struct {
	IdentityToken string `json:"identityToken"`
}

type googleAuthRequest struct {
	IDToken string `json:"idToken"`
}

func handleAuthApple(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeAPIError(w, http.StatusUnsupportedMediaType, "invalid_request", "expected Content-Type: application/json")
		return
	}

	var req appleAuthRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if strings.TrimSpace(req.IdentityToken) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "identityToken is required")
		return
	}

	writeAPIError(w, http.StatusNotImplemented, "not_implemented", "Apple sign-in is not implemented yet")
}

func handleAuthGoogle(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeAPIError(w, http.StatusUnsupportedMediaType, "invalid_request", "expected Content-Type: application/json")
		return
	}

	var req googleAuthRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if strings.TrimSpace(req.IDToken) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "idToken is required")
		return
	}

	writeAPIError(w, http.StatusNotImplemented, "not_implemented", "Google sign-in is not implemented yet")
}

func handleAuthMe(w http.ResponseWriter, r *http.Request) {
	writeAPIError(w, http.StatusNotImplemented, "not_implemented", "authenticated user profile is not implemented yet")
}
