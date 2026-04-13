package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"foreignreader_be/internal/auth"
)

func parseBearer(header string) (token string, ok bool) {
	h := strings.TrimSpace(header)
	const prefix = "Bearer "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	t := strings.TrimSpace(h[len(prefix):])
	if t == "" {
		return "", false
	}
	return t, true
}

func bearerAuthHandler(store *auth.Store, issuer *auth.TokenIssuer, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := parseBearer(r.Header.Get("Authorization"))
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
			return
		}
		uid, _, sid, err := issuer.ParseAccessToken(raw)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
			return
		}
		user, err := store.UserByID(r.Context(), uid)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeAPIError(w, http.StatusUnauthorized, "user_not_found", "user no longer exists")
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load user")
			return
		}
		ctx := auth.ContextWithUser(r.Context(), user)
		ctx = auth.ContextWithSessionID(ctx, sid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
