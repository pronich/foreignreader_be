package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"foreignreader_be/internal/appleiap"
	"foreignreader_be/internal/config"
	"foreignreader_be/internal/entitlement"

	"github.com/google/uuid"
)

func handleAppleIAPWebhook(cfg config.Config, db *sql.DB, ent *entitlement.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rejectUnlessJSONContentType(w, r) {
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "could not read body")
			return
		}

		var env appleiap.SignedPayloadEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		if strings.TrimSpace(env.SignedPayload) == "" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "signedPayload is required")
			return
		}

		rid := requestIDFromContext(r.Context())
		log.Printf("iap/apple_webhook: request_id=%s action=received signed_payload_len=%d", rid, len(env.SignedPayload))

		np, err := appleiap.VerifyAndDecodeNotification(env.SignedPayload)
		if err != nil {
			log.Printf("iap/apple_webhook: request_id=%s action=verify_failed err=%v", rid, err)
			writeAPIError(w, http.StatusBadRequest, "invalid_signature", "invalid signed payload")
			return
		}

		// Basic sanity: ensure notification is for our bundle (if present).
		if bid := strings.TrimSpace(np.Data.BundleID); bid != "" && strings.TrimSpace(cfg.AppleIAPBundleID) != "" {
			if bid != strings.TrimSpace(cfg.AppleIAPBundleID) {
				log.Printf("iap/apple_webhook: request_id=%s action=rejected reason=bundle_id_mismatch bundle_id=%q", rid, bid)
				writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid notification target")
				return
			}
		}

		// Nested signedTransactionInfo is the most actionable for subscription state.
		var txp appleiap.TransactionPayload
		if strings.TrimSpace(np.Data.SignedTransactionInfo) != "" {
			if err := appleiap.VerifyAndDecodeJWSPayload(np.Data.SignedTransactionInfo, &txp); err != nil {
				log.Printf("iap/apple_webhook: request_id=%s action=tx_verify_failed err=%v", rid, err)
				writeAPIError(w, http.StatusBadRequest, "invalid_signature", "invalid transaction payload")
				return
			}
		}

		origTx := strings.TrimSpace(txp.OriginalTransactionID)
		txID := strings.TrimSpace(txp.TransactionID)
		notificationType := strings.TrimSpace(np.NotificationType)
		subtype := strings.TrimSpace(np.Subtype)

		ctx := r.Context()
		store := appleiap.NewStore(db)

		dbtx, err := db.BeginTx(ctx, nil)
		if err != nil {
			log.Printf("iap/apple_webhook: request_id=%s action=db_begin_failed err=%v", rid, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not process webhook")
			return
		}
		defer func() { _ = dbtx.Rollback() }()

		inserted, err := store.InsertAppleEvent(ctx, dbtx, appleiap.AppleEventInsert{
			NotificationUUID:      np.NotificationUUID,
			NotificationType:      notificationType,
			Subtype:               subtype,
			OriginalTransactionID: origTx,
			TransactionID:         txID,
			SignedPayload:         env.SignedPayload,
		})
		if err != nil {
			log.Printf("iap/apple_webhook: request_id=%s action=event_insert_failed err=%v", rid, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not process webhook")
			return
		}
		if !inserted {
			_ = dbtx.Rollback()
			log.Printf("iap/apple_webhook: request_id=%s action=ignored_duplicate notification_uuid=%s type=%s", rid, np.NotificationUUID, notificationType)
			writeWebhookOK(w)
			return
		}

		log.Printf("iap/apple_webhook: request_id=%s action=verified notification_uuid=%s type=%s subtype=%s orig_tx=%s",
			rid, np.NotificationUUID, notificationType, subtype, origTx)

		// If we can't resolve a subscription lineage, we cannot update user entitlements yet.
		if origTx == "" {
			_ = store.MarkAppleEventProcessed(ctx, dbtx, np.NotificationUUID)
			if err := dbtx.Commit(); err != nil {
				log.Printf("iap/apple_webhook: request_id=%s action=commit_failed err=%v", rid, err)
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not process webhook")
				return
			}
			writeWebhookOK(w)
			return
		}

		subRow, err := store.SubscriptionByOriginalTransactionID(ctx, dbtx, origTx)
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("iap/apple_webhook: request_id=%s action=no_subscription_row orig_tx=%s", rid, origTx)
			_ = store.MarkAppleEventProcessed(ctx, dbtx, np.NotificationUUID)
			if err := dbtx.Commit(); err != nil {
				log.Printf("iap/apple_webhook: request_id=%s action=commit_failed err=%v", rid, err)
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not process webhook")
				return
			}
			// No client-initiated /validate has linked this transaction to a user yet — normally
			// because the app never made that call (crash, backgrounded purchase, dropped request).
			// Self-heal using appAccountToken (set at purchase time on the client) so the sale isn't
			// silently lost: re-verify server-to-server and create the subscription/entitlement here.
			selfHealAppleTransactionByAccountToken(ctx, cfg, db, ent, rid, txID, txp.AppAccountToken)
			writeWebhookOK(w)
			return
		}
		if err != nil {
			log.Printf("iap/apple_webhook: request_id=%s action=sub_lookup_failed err=%v", rid, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not process webhook")
			return
		}

		status, expiresAt := deriveAppleSubscriptionState(notificationType, subtype, txp)
		envNorm := deriveAppleEnv(np, txp, cfg)

		if err := store.UpdateSubscriptionStateByOriginalTransactionID(ctx, dbtx, origTx, txID, status, envNorm, expiresAt); err != nil {
			log.Printf("iap/apple_webhook: request_id=%s action=sub_update_failed err=%v", rid, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not process webhook")
			return
		}

		grant := status == "active" || status == "grace_period" || status == "billing_retry"
		if subRow.ProductCode == entitlement.ProductPro {
			if grant {
				var exp sql.NullTime
				if expiresAt != nil {
					exp = sql.NullTime{Time: expiresAt.UTC(), Valid: true}
				}
				if err := ent.UpsertAppleIAPPro(ctx, dbtx, subRow.UserID, exp); err != nil {
					log.Printf("iap/apple_webhook: request_id=%s action=entitlement_upsert_failed err=%v", rid, err)
					writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not process webhook")
					return
				}
				log.Printf("iap/apple_webhook: request_id=%s action=entitlement_granted user_id=%s status=%s", rid, subRow.UserID, status)
			} else {
				if err := ent.RevokeAppleIAPPro(ctx, dbtx, subRow.UserID); err != nil {
					log.Printf("iap/apple_webhook: request_id=%s action=entitlement_revoke_failed err=%v", rid, err)
					writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not process webhook")
					return
				}
				log.Printf("iap/apple_webhook: request_id=%s action=entitlement_revoked user_id=%s status=%s", rid, subRow.UserID, status)
			}
		}

		_ = store.MarkAppleEventProcessed(ctx, dbtx, np.NotificationUUID)
		if err := dbtx.Commit(); err != nil {
			log.Printf("iap/apple_webhook: request_id=%s action=commit_failed err=%v", rid, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not process webhook")
			return
		}

		writeWebhookOK(w)
	}
}

// selfHealAppleTransactionByAccountToken links an otherwise-orphaned Apple notification to its
// user via appAccountToken, then runs it through the same server-to-server validation the
// client's own /iap/apple/validate call would have. Best-effort: errors are logged, never
// surfaced to Apple, since this is a fallback path and Apple's webhook must still get a 200.
func selfHealAppleTransactionByAccountToken(ctx context.Context, cfg config.Config, db *sql.DB, ent *entitlement.Store, rid, transactionID, appAccountToken string) {
	token := strings.TrimSpace(appAccountToken)
	if token == "" || strings.TrimSpace(transactionID) == "" {
		return
	}
	userID, err := uuid.Parse(token)
	if err != nil {
		log.Printf("iap/apple_webhook: request_id=%s action=self_heal_skipped reason=invalid_app_account_token", rid)
		return
	}
	if !cfg.AppleIAPConfigured() {
		return
	}

	env := appleiap.EnvProduction
	if strings.EqualFold(strings.TrimSpace(cfg.AppleIAPEnvironment), "sandbox") {
		env = appleiap.EnvSandbox
	}
	client, err := appleiap.NewClient(env, cfg.AppleIAPIssuerID, cfg.AppleIAPKeyID, cfg.AppleIAPBundleID, cfg.AppleIAPPrivateKey)
	if err != nil {
		log.Printf("iap/apple_webhook: request_id=%s action=self_heal_client_init_failed err=%v", rid, err)
		return
	}
	svc, err := appleiap.NewService(client, appleiap.NewStore(db), ent, cfg.AppleIAPProProductID)
	if err != nil {
		log.Printf("iap/apple_webhook: request_id=%s action=self_heal_service_init_failed err=%v", rid, err)
		return
	}

	if _, err := svc.ValidateTransaction(ctx, userID, transactionID); err != nil {
		log.Printf("iap/apple_webhook: request_id=%s action=self_heal_failed user_id=%s err=%v", rid, userID, err)
		return
	}
	log.Printf("iap/apple_webhook: request_id=%s action=self_heal_succeeded user_id=%s", rid, userID)
}

func deriveAppleEnv(np *appleiap.NotificationPayload, txp appleiap.TransactionPayload, cfg config.Config) string {
	// Prefer explicit values from payloads, fallback to configured environment.
	if v := strings.TrimSpace(txp.Environment); v != "" {
		return strings.ToLower(v)
	}
	if v := strings.TrimSpace(np.Data.Environment); v != "" {
		return strings.ToLower(v)
	}
	return strings.ToLower(strings.TrimSpace(cfg.AppleIAPEnvironment))
}

func deriveAppleSubscriptionState(notificationType, subtype string, txp appleiap.TransactionPayload) (status string, expiresAt *time.Time) {
	now := time.Now().UTC()
	expiresAt = txp.ExpiresAt()

	nt := strings.ToUpper(strings.TrimSpace(notificationType))
	st := strings.ToUpper(strings.TrimSpace(subtype))

	// Strong signals.
	if txp.RevocationDate > 0 || nt == "REVOKE" || nt == "REFUND" {
		return "revoked", expiresAt
	}
	if nt == "EXPIRED" {
		return "expired", expiresAt
	}
	if expiresAt != nil && !expiresAt.After(now) {
		return "expired", expiresAt
	}

	// Billing/grace states.
	if nt == "DID_FAIL_TO_RENEW" {
		return "billing_retry", expiresAt
	}
	if nt == "DID_ENTER_GRACE_PERIOD" || st == "GRACE_PERIOD" {
		return "grace_period", expiresAt
	}
	if nt == "GRACE_PERIOD_EXPIRED" {
		return "expired", expiresAt
	}

	// Default active for purchase/renew/update notifications.
	switch nt {
	case "SUBSCRIBED", "DID_RENEW", "DID_CHANGE_RENEWAL_STATUS", "DID_CHANGE_RENEWAL_PREF", "DID_RECOVER", "RENEWAL_EXTENDED":
		return "active", expiresAt
	default:
		// Unknown types: be conservative but non-destructive if still unexpired.
		return "active", expiresAt
	}
}
