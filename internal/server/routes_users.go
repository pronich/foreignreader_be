package server

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"foreignreader_be/internal/auth"
)

func registerUserRoutes(mux *http.ServeMux, store *auth.Store, issuer *auth.TokenIssuer) {
	mux.Handle("PUT /api/v1/users/me/storefront", bearerAuthHandler(store, issuer, handlePutUserStorefront(store)))
}

type putStorefrontRequest struct {
	AppStorefront string `json:"appStorefront"`
}

type putStorefrontResponse struct {
	Success       bool      `json:"success"`
	AppStorefront string    `json:"appStorefront"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func handlePutUserStorefront(store *auth.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authentication context")
			return
		}

		if rejectUnlessJSONContentType(w, r) {
			return
		}

		var req putStorefrontRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<14))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}

		sf, valid := auth.NormalizeAppStorefront(req.AppStorefront)
		if !valid {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "appStorefront is required and must be a non-empty string up to 16 characters")
			return
		}

		updatedAt, err := store.UpdateAppStorefront(r.Context(), u.ID, sf)
		if err != nil {
			if err == sql.ErrNoRows {
				writeAPIError(w, http.StatusNotFound, "user_not_found", "user no longer exists")
				return
			}
			log.Printf("users: storefront update user_id=%s err=%v", u.ID, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not update storefront")
			return
		}

		log.Printf("users: storefront user_id=%s storefront=%s updated_at=%s", u.ID, sf, updatedAt.UTC().Format(time.RFC3339Nano))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(putStorefrontResponse{
			Success:       true,
			AppStorefront: sf,
			UpdatedAt:     updatedAt.UTC(),
		})
	}
}
