package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"foreignreader_be/internal/billing"
	"foreignreader_be/internal/config"
	"foreignreader_be/internal/entitlement"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v81/webhook"
)

func handleStripeWebhook(cfg config.Config, db *sql.DB, ent *entitlement.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret := strings.TrimSpace(cfg.StripeWebhookSecret)
		if secret == "" {
			log.Printf("billing: webhook rejected reason=webhook_secret_not_configured")
			writeAPIError(w, http.StatusServiceUnavailable, "billing_unavailable", "webhook not configured")
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "could not read body")
			return
		}

		sig := r.Header.Get("Stripe-Signature")
		if strings.TrimSpace(sig) == "" {
			log.Printf("billing: webhook rejected reason=missing_signature")
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "missing Stripe-Signature")
			return
		}

		evt, err := webhook.ConstructEventWithOptions(body, sig, secret, webhook.ConstructEventOptions{
			IgnoreAPIVersionMismatch: true,
		})
		if err != nil {
			log.Printf("billing: webhook signature verification failed err=%v", err)
			writeAPIError(w, http.StatusBadRequest, "invalid_signature", "invalid webhook signature")
			return
		}

		if evt.Data == nil || len(evt.Data.Raw) == 0 {
			log.Printf("billing: webhook event_id=%s type=%s rejected reason=empty_event_data", evt.ID, evt.Type)
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid event payload")
			return
		}

		ctx := r.Context()

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			log.Printf("billing: webhook event_id=%s type=%s db_begin err=%v", evt.ID, evt.Type, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not process webhook")
			return
		}
		defer func() { _ = tx.Rollback() }()

		var rowID uuid.UUID
		err = tx.QueryRowContext(ctx, `
			INSERT INTO stripe_webhook_events (stripe_event_id, event_type)
			VALUES ($1, $2)
			ON CONFLICT (stripe_event_id) DO NOTHING
			RETURNING id
		`, evt.ID, string(evt.Type)).Scan(&rowID)
		if errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			log.Printf("billing: webhook event_id=%s type=%s action=ignored_duplicate", evt.ID, evt.Type)
			writeWebhookOK(w)
			return
		}
		if err != nil {
			log.Printf("billing: webhook event_id=%s type=%s idempotency_insert err=%v", evt.ID, evt.Type, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not process webhook")
			return
		}

		if err := billing.ProcessStripeEvent(ctx, tx, db, ent, evt); err != nil {
			log.Printf("billing: webhook event_id=%s type=%s process err=%v", evt.ID, evt.Type, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not apply webhook")
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("billing: webhook event_id=%s type=%s commit err=%v", evt.ID, evt.Type, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not process webhook")
			return
		}

		log.Printf("billing: webhook event_id=%s type=%s action=processed_ok", evt.ID, evt.Type)
		writeWebhookOK(w)
	}
}

func writeWebhookOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]bool{"received": true})
}
