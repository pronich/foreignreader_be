package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"foreignreader_be/internal/config"
	"foreignreader_be/internal/onboardingsession"
	"foreignreader_be/internal/ratelimit"
	"foreignreader_be/internal/translate"
)

type ctxKeyOnboardingTokenID struct{}

// OnboardingTokenIDFromContext is set by onboardingOpaqueBearerMiddleware after a valid opaque token.
func OnboardingTokenIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(ctxKeyOnboardingTokenID{}).(uuid.UUID)
	return v, ok
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			s := strings.TrimSpace(parts[0])
			if s != "" {
				return s
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func secureAppTokenEqual(client, server string) bool {
	if len(client) != len(server) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(client), []byte(server)) == 1
}

func withOnboardingSessionIPRate(wl *ratelimit.Window, limit int, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if limit > 0 && !wl.Allow("onboarding_session_ip:"+clientIP(r), limit, time.Minute) {
			writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "too many onboarding session requests")
			return
		}
		next(w, r)
	}
}

func withOnboardingTranslateIPRate(wl *ratelimit.Window, limit int, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if limit > 0 && !wl.Allow("onboarding_translate_ip:"+clientIP(r), limit, time.Minute) {
			writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "too many onboarding translation requests")
			return
		}
		next(w, r)
	}
}

func withOnboardingTranslateTokenRate(wl *ratelimit.Window, limit int, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := OnboardingTokenIDFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "missing onboarding token context")
			return
		}
		if limit > 0 && !wl.Allow("onboarding_translate_tok:"+id.String(), limit, time.Minute) {
			writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "too many requests for this onboarding token")
			return
		}
		next(w, r)
	}
}

func onboardingOpaqueBearerMiddleware(obStore *onboardingsession.Store, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, ok := parseBearer(r.Header.Get("Authorization"))
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		id, err := obStore.ValidateAndTouch(r.Context(), raw, clientIP(r))
		if err != nil {
			switch {
			case errors.Is(err, onboardingsession.ErrInvalidToken):
				writeAPIError(w, http.StatusUnauthorized, "invalid_token", "invalid onboarding access token")
			case errors.Is(err, onboardingsession.ErrExpiredToken):
				writeAPIError(w, http.StatusUnauthorized, "token_expired", "onboarding access token expired")
			case errors.Is(err, onboardingsession.ErrRevokedToken):
				writeAPIError(w, http.StatusUnauthorized, "token_revoked", "onboarding access token revoked")
			case errors.Is(err, onboardingsession.ErrInsufficientScope):
				writeAPIError(w, http.StatusForbidden, "insufficient_scope", "onboarding token is not valid for this operation")
			default:
				log.Printf("onboarding: validate token: %v", err)
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not validate onboarding token")
			}
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyOnboardingTokenID{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

type onboardingSessionRequest struct {
	AppToken   string `json:"appToken"`
	AppVersion string `json:"appVersion"`
	DeviceID   string `json:"deviceId,omitempty"`
	Platform   string `json:"platform,omitempty"`
}

type onboardingSessionResponse struct {
	AccessToken string    `json:"accessToken"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func handleOnboardingSession(cfg config.Config, obStore *onboardingsession.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.OnboardingContextTranslateToken == "" {
			writeAPIError(w, http.StatusServiceUnavailable, "not_configured", "onboarding is not configured")
			return
		}

		if rejectUnlessJSONContentType(w, r) {
			return
		}

		var req onboardingSessionRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}

		if strings.TrimSpace(req.AppToken) == "" {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing app token")
			return
		}
		if !secureAppTokenEqual(strings.TrimSpace(req.AppToken), cfg.OnboardingContextTranslateToken) {
			writeAPIError(w, http.StatusForbidden, "forbidden", "invalid app token")
			return
		}
		if strings.TrimSpace(req.AppVersion) == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "appVersion is required")
			return
		}

		raw, exp, err := obStore.Issue(r.Context(), req.AppVersion, req.DeviceID, req.Platform, clientIP(r), cfg.OnboardingSessionTokenTTL)
		if err != nil {
			log.Printf("onboarding: issue token: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not issue onboarding token")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(onboardingSessionResponse{
			AccessToken: raw,
			ExpiresAt:   exp.UTC(),
		})
	}
}

func handleOnboardingTranslateContext(tr *translate.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rejectUnlessJSONContentType(w, r) {
			return
		}

		var req translateContextRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}

		serveTranslateContext(w, r, tr, req)
	}
}
