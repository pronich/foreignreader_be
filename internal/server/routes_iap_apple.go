package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"foreignreader_be/internal/appleiap"
	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/config"
	"foreignreader_be/internal/entitlement"
)

func registerIAPRoutes(mux *http.ServeMux, cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer, ent *entitlement.Store) {
	mux.Handle("POST /api/v1/iap/apple/validate", bearerAuthHandler(store, issuer, handleAppleIAPValidate(cfg, ent)))
}

type appleIAPValidateRequest struct {
	Source        string `json:"source"`
	TransactionID string `json:"transactionId"`
}

type appleIAPValidateResponse struct {
	Entitlement entitlementPublic `json:"entitlement"`
	Apple       struct {
		Status      string     `json:"status"`
		ProductID   string     `json:"productId"`
		ProductCode string     `json:"productCode"`
		Environment string     `json:"environment"`
		ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	} `json:"apple"`
}

func handleAppleIAPValidate(cfg config.Config, ent *entitlement.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authentication context")
			return
		}
		if rejectUnlessJSONContentType(w, r) {
			return
		}
		if !cfg.AppleIAPConfigured() {
			writeAPIError(w, http.StatusServiceUnavailable, "iap_unavailable", "Apple IAP validation is not configured")
			return
		}

		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		var req appleIAPValidateRequest
		if err := dec.Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}

		rid := requestIDFromContext(r.Context())
		log.Printf("iap/apple/validate: request_id=%s user_id=%s action=request_payload source=%q transaction_id=%q",
			rid, u.ID.String(), strings.TrimSpace(req.Source), strings.TrimSpace(req.TransactionID))

		source, err := parseAuthRequestSourceField(req.Source)
		if err != nil || source != authRequestSourceApp {
			writeAPIError(w, http.StatusBadRequest, "invalid_source", "source must be app")
			return
		}
		if strings.TrimSpace(req.TransactionID) == "" {
			writeAPIError(w, http.StatusBadRequest, "missing_transaction_id", "transactionId is required")
			return
		}

		env := appleiap.EnvProduction
		if strings.EqualFold(strings.TrimSpace(cfg.AppleIAPEnvironment), "sandbox") {
			env = appleiap.EnvSandbox
		}
		client, err := appleiap.NewClient(env, cfg.AppleIAPIssuerID, cfg.AppleIAPKeyID, cfg.AppleIAPBundleID, cfg.AppleIAPPrivateKey)
		if err != nil {
			log.Printf("iap/apple: request_id=%s user_id=%s action=client_init_failed err=%v", rid, u.ID.String(), err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not validate purchase")
			return
		}
		svc, err := appleiap.NewService(client, appleiap.NewStore(ent.DB), ent, cfg.AppleIAPProProductID)
		if err != nil {
			log.Printf("iap/apple: request_id=%s user_id=%s action=service_init_failed err=%v", rid, u.ID.String(), err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not validate purchase")
			return
		}

		res, err := svc.ValidateTransaction(r.Context(), u.ID, strings.TrimSpace(req.TransactionID))
		if err != nil {
			if errors.Is(err, appleiap.ErrUnknownProductID) {
				writeAPIError(w, http.StatusBadRequest, "unknown_product", "unknown Apple product")
				return
			}
			if errors.Is(err, appleiap.ErrNotEntitled) {
				writeAPIError(w, http.StatusForbidden, "subscription_inactive", "subscription is not active")
				return
			}
			log.Printf("iap/apple: request_id=%s user_id=%s action=validate_failed err=%v", rid, u.ID.String(), err)
			writeAPIError(w, http.StatusBadGateway, "iap_validation_failed", "Apple IAP validation failed")
			return
		}

		log.Printf("iap/apple: request_id=%s user_id=%s action=validate_ok status=%s expires_at=%v", rid, u.ID.String(), res.Status, res.ExpiresAt)

		// Response mirrors entitlement patterns already used.
		out := appleIAPValidateResponse{
			Entitlement: entitlementPublic{
				ProductCode: res.ProductCode,
				Status:      "active",
				Source:      "apple_iap",
				ExpiresAt:   res.ExpiresAt,
			},
		}
		out.Apple.Status = res.Status
		out.Apple.ProductCode = res.ProductCode
		out.Apple.Environment = res.Environment
		out.Apple.ExpiresAt = res.ExpiresAt
		out.Apple.ProductID = cfg.AppleIAPProProductID

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(out)
	}
}

