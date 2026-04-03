package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/config"
	"foreignreader_be/internal/entitlement"
)

func registerEntitlementRoutes(mux *http.ServeMux, cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer, ent *entitlement.Store) {
	mux.Handle("GET /api/v1/me/entitlements", bearerAuthHandler(store, issuer, handleMeEntitlements(ent)))

	mux.Handle("POST /api/v1/dev/entitlements/pro", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !cfg.MockAuthAllowed() {
			writeAPIError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		bearerAuthHandler(store, issuer, handleDevProPOST(ent)).ServeHTTP(w, r)
	}))
}

type entitlementPublic struct {
	ProductCode string     `json:"productCode"`
	Status      string     `json:"status"`
	Source      string     `json:"source"`
	ExpiresAt   *time.Time `json:"expiresAt"`
}

type entitlementUpdateResponse struct {
	Entitlement entitlementPublic `json:"entitlement"`
}

type entitlementsListResponse struct {
	Entitlements []entitlementPublic `json:"entitlements"`
}

func entitlementPublicFrom(e entitlement.Entitlement) entitlementPublic {
	out := entitlementPublic{
		ProductCode: e.ProductCode,
		Status:      e.Status,
		Source:      e.Source,
	}
	if e.ExpiresAt.Valid {
		t := e.ExpiresAt.Time.UTC()
		out.ExpiresAt = &t
	}
	return out
}

func handleMeEntitlements(ent *entitlement.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authentication context")
			return
		}
		list, err := ent.ListByUser(r.Context(), u.ID)
		if err != nil {
			log.Printf("entitlements: list: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load entitlements")
			return
		}
		out := make([]entitlementPublic, 0, len(list))
		for _, e := range list {
			out = append(out, entitlementPublicFrom(e))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(entitlementsListResponse{Entitlements: out})
	}
}

type devProBody struct {
	Active *bool `json:"active"`
}

func handleDevProPOST(ent *entitlement.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authentication context")
			return
		}

		if rejectUnlessJSONContentType(w, r) {
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "could not read body")
			return
		}
		var req devProBody
		if err := json.Unmarshal(body, &req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		if req.Active == nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "active is required")
			return
		}

		row, err := ent.SetDevPro(r.Context(), u.ID, *req.Active)
		if err != nil {
			log.Printf("entitlements: set dev pro: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not update entitlement")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(entitlementUpdateResponse{
			Entitlement: entitlementPublicFrom(row),
		})
	}
}

func requireProMiddleware(ent *entitlement.Store, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authentication context")
			return
		}
		okPro, err := ent.HasActivePro(r.Context(), u.ID)
		if err != nil {
			log.Printf("entitlement: pro check: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not verify entitlement")
			return
		}
		if !okPro {
			writeAPIError(w, http.StatusForbidden, "forbidden", "active Pro entitlement required")
			return
		}
		next(w, r)
	}
}
