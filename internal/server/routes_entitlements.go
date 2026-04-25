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
	"foreignreader_be/internal/monthlycontexttranslation"
)

func registerEntitlementRoutes(mux *http.ServeMux, cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer, ent *entitlement.Store) {
	mux.Handle("GET /api/v1/me/entitlements", bearerAuthHandler(store, issuer, handleMeEntitlements(cfg, ent)))

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
	Entitlements    []entitlementPublic   `json:"entitlements"`
	EffectiveAccess effectiveAccessPublic `json:"effectiveAccess"`
}

type effectiveAccessPublic struct {
	IsPro        bool                `json:"isPro"`
	Plan         string              `json:"plan"`
	Source       string              `json:"source,omitempty"`    // active Pro source when isPro (e.g. stripe, dev, apple_iap)
	ExpiresAt    *time.Time          `json:"expiresAt,omitempty"` // when Pro access ends, if known (e.g. billing period end)
	ContextQuota *contextQuotaPublic `json:"contextQuota,omitempty"`
}

type contextQuotaPublic struct {
	MonthlyLimit int    `json:"monthlyLimit"`
	UsedCount    int    `json:"usedCount"`
	Remaining    int    `json:"remaining"`
	PeriodKey    string `json:"periodKey"`
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

func handleMeEntitlements(cfg config.Config, ent *entitlement.Store) http.HandlerFunc {
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

		isPro, err := ent.HasActivePro(r.Context(), u.ID)
		if err != nil {
			log.Printf("entitlements: pro check: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load entitlements")
			return
		}

		ea := effectiveAccessPublic{IsPro: isPro}
		if isPro {
			ea.Plan = "pro"
			if src, exp, has, err := ent.ActiveProAccess(r.Context(), u.ID); err != nil {
				log.Printf("entitlements: active pro access: %v", err)
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load entitlements")
				return
			} else if has {
				ea.Source = src
				if exp.Valid {
					t := exp.Time.UTC()
					ea.ExpiresAt = &t
				}
			}
		} else {
			ea.Plan = "free"
			pk, ml, uc, err := monthlycontexttranslation.EnsureCurrentMonthRow(r.Context(), ent.DB, u.ID, cfg.FreeContextTranslationsPerMonth)
			if err != nil {
				log.Printf("entitlements: quota ensure: %v", err)
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not load entitlements")
				return
			}
			rem := ml - uc
			if rem < 0 {
				rem = 0
			}
			ea.ContextQuota = &contextQuotaPublic{
				MonthlyLimit: ml,
				UsedCount:    uc,
				Remaining:    rem,
				PeriodKey:    pk,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(entitlementsListResponse{Entitlements: out, EffectiveAccess: ea})
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
