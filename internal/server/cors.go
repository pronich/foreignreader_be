package server

import (
	"net/http"
	"strings"
)

// CORS for browser clients calling /api/* on the API host (cross-origin JWT, JSON, etc.).
// Preflight must be handled before http.ServeMux: method-specific routes (e.g. GET /api/v1/me)
// do not match OPTIONS, so the mux responds with 405 otherwise.

const (
	corsAllowMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	corsAllowHeaders = "Authorization, Content-Type, Accept, Origin"
	corsMaxAge       = "86400"
)

func corsAPIMatch(path string) bool {
	return strings.HasPrefix(path, "/api/")
}

func newCORSOriginSet(origins []string) map[string]struct{} {
	m := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		o = strings.TrimSpace(o)
		if o != "" {
			m[o] = struct{}{}
		}
	}
	return m
}

func withCORS(allowed map[string]struct{}) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			_, ok := allowed[origin]
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			if r.Method == http.MethodOptions && corsAPIMatch(r.URL.Path) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", corsAllowMethods)
				w.Header().Set("Access-Control-Allow-Headers", corsAllowHeaders)
				w.Header().Set("Access-Control-Max-Age", corsMaxAge)
				w.Header().Add("Vary", "Origin")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			if corsAPIMatch(r.URL.Path) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}

			next.ServeHTTP(w, r)
		})
	}
}
