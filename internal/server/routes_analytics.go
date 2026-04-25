package server

import (
	"bytes"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"foreignreader_be/internal/analytics"
	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/config"
	"foreignreader_be/internal/ratelimit"
)

func registerAnalyticsRoutes(mux *http.ServeMux, cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer, a *analytics.Store, ipWL, anonWL *ratelimit.Window) {
	mux.Handle("POST /api/v1/analytics/events", handleAnalyticsEvents(cfg, store, issuer, a, ipWL, anonWL))
}

type analyticsEventRequest struct {
	EventName   string         `json:"eventName"`
	AnonymousID string         `json:"anonymousId"`
	SessionID   string         `json:"sessionId"`
	AppVersion  string         `json:"appVersion"`
	Platform    string         `json:"platform"`
	OccurredAt  string         `json:"occurredAt"`
	Properties  map[string]any `json:"properties"`
}

type analyticsEventResponse struct {
	OK bool `json:"ok"`
}

var allowedAnalyticsEventNames = map[string]struct{}{
	"app_opened":                    {},
	"app_backgrounded":              {},
	"book_import_succeeded":         {},
	"book_opened":                   {},
	"book_deleted":                  {},
	"reader_opened":                 {},
	"reader_page_changed":           {},
	"reader_closed":                 {},
	"word_translation_requested":    {},
	"context_translation_requested": {},
}

var allowedAnalyticsPropertyKeys = map[string]struct{}{
	"isPro":                  {},
	"sourceLanguage":         {},
	"targetLanguage":         {},
	"bookLanguage":           {},
	"translationType":        {},
	"failureReasonCode":      {},
	"pageDelta":              {},
	"readerSessionPageCount": {},
}

var forbiddenAnalyticsPropertyKeys = map[string]struct{}{
	"selectedWord":      {},
	"sentence":          {},
	"bookText":          {},
	"translationResult": {},
	"email":             {},
}

var allowedAnalyticsPlatforms = map[string]struct{}{
	"iphone": {},
	"ipad":   {},
}

func secureEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func handleAnalyticsEvents(cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer, a *analytics.Store, ipWL, anonWL *ratelimit.Window) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientHdr := strings.ToLower(strings.TrimSpace(r.Header.Get("X-ForeignReader-Client")))
		if clientHdr != "ios" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "missing or invalid X-ForeignReader-Client header")
			return
		}

		if strings.TrimSpace(cfg.AnalyticsIngestionKey) != "" {
			got := strings.TrimSpace(r.Header.Get("X-ForeignReader-Analytics-Key"))
			if !secureEqual(got, cfg.AnalyticsIngestionKey) {
				writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid analytics ingestion key")
				return
			}
		}

		// Body max 16KB.
		body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024+1))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "could not read body")
			return
		}
		if len(body) > 16*1024 {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body too large")
			return
		}

		var req analyticsEventRequest
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}

		ev := strings.TrimSpace(req.EventName)
		if ev == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "eventName is required")
			return
		}
		if _, ok := allowedAnalyticsEventNames[ev]; !ok {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "unknown eventName")
			return
		}

		anon := strings.TrimSpace(req.AnonymousID)
		if anon == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "anonymousId is required")
			return
		}
		sess := strings.TrimSpace(req.SessionID)
		if sess == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "sessionId is required")
			return
		}
		appV := strings.TrimSpace(req.AppVersion)
		if appV == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "appVersion is required")
			return
		}

		platform := strings.ToLower(strings.TrimSpace(req.Platform))
		if platform == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "platform is required")
			return
		}
		if _, ok := allowedAnalyticsPlatforms[platform]; !ok {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "platform must be iphone or ipad")
			return
		}

		if req.Properties == nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "properties is required and must be an object")
			return
		}
		if len(req.Properties) == 0 {
			// keep as empty object
		}

		for k := range req.Properties {
			if _, bad := forbiddenAnalyticsPropertyKeys[k]; bad {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "properties contains forbidden field")
				return
			}
			if _, ok := allowedAnalyticsPropertyKeys[k]; !ok {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "properties contains unknown field")
				return
			}
		}

		// Type checks for known properties (strict v1).
		if v, ok := req.Properties["isPro"]; ok {
			if _, ok := v.(bool); !ok {
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "properties.isPro must be a boolean")
				return
			}
		}
		for _, k := range []string{"sourceLanguage", "targetLanguage", "bookLanguage", "translationType", "failureReasonCode"} {
			if v, ok := req.Properties[k]; ok {
				s, ok := v.(string)
				if !ok || strings.TrimSpace(s) == "" {
					writeAPIError(w, http.StatusBadRequest, "invalid_request", "properties."+k+" must be a non-empty string")
					return
				}
			}
		}
		for _, k := range []string{"pageDelta", "readerSessionPageCount"} {
			if v, ok := req.Properties[k]; ok {
				if _, ok := v.(float64); !ok { // encoding/json decodes numbers as float64
					writeAPIError(w, http.StatusBadRequest, "invalid_request", "properties."+k+" must be a number")
					return
				}
			}
		}

		propsBytes, err := json.Marshal(req.Properties)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid properties")
			return
		}
		if len(propsBytes) > 4*1024 {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "properties too large")
			return
		}

		occurredAt, err := time.Parse(time.RFC3339, strings.TrimSpace(req.OccurredAt))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "occurredAt must be RFC3339 timestamp")
			return
		}
		occurredAt = occurredAt.UTC()
		now := time.Now().UTC()
		if occurredAt.After(now.Add(24 * time.Hour)) {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "occurredAt is too far in the future")
			return
		}
		if occurredAt.Before(now.Add(-7 * 24 * time.Hour)) {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "occurredAt is too old")
			return
		}

		// Lightweight rate limiting.
		if ipWL != nil && !ipWL.Allow("analytics_ip:"+clientIP(r), 120, time.Minute) {
			writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "too many analytics events from this IP")
			return
		}
		if anonWL != nil && !anonWL.Allow("analytics_anon:"+anon, 60, time.Minute) {
			writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "too many analytics events for this device")
			return
		}

		// Optional auth: attach user_id when token is valid.
		var userID *uuid.UUID
		if raw, ok := parseBearer(r.Header.Get("Authorization")); ok {
			uid, _, _, err := issuer.ParseAccessToken(raw)
			if err == nil {
				// Only attach if the user still exists; otherwise treat as anonymous.
				if _, err := store.UserByID(r.Context(), uid); err == nil {
					u := uid
					userID = &u
				} else if !errors.Is(err, sql.ErrNoRows) {
					log.Printf("analytics: user load err=%v", err)
				}
			}
		}

		if cfg.AnalyticsEnabled && a != nil && a.DB != nil {
			err := a.Insert(r.Context(), analytics.EventInsert{
				EventName:   ev,
				AnonymousID: anon,
				UserID:      userID,
				SessionID:   sess,
				AppVersion:  appV,
				Platform:    platform,
				Properties:  req.Properties,
				OccurredAt:  occurredAt,
				ReceivedAt:  now,
			})
			if err != nil {
				log.Printf("analytics: insert event_name=%s anonymous_id=%s err=%v", ev, anon, err)
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not record event")
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(analyticsEventResponse{OK: true})
	})
}
