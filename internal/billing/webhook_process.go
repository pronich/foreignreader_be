package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"foreignreader_be/internal/entitlement"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v81"
)

// ProcessStripeEvent applies entitlement changes for supported event types.
// It runs inside an existing transaction (tx). The caller records idempotency.
func ProcessStripeEvent(ctx context.Context, tx *sql.Tx, db *sql.DB, ent *entitlement.Store, evt stripe.Event) error {
	switch evt.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		return handleCheckoutSessionCompleted(ctx, tx, db, ent, evt)
	case stripe.EventTypeCustomerSubscriptionUpdated,
		stripe.EventTypeCustomerSubscriptionCreated:
		return handleCustomerSubscriptionUpdated(ctx, tx, db, ent, evt)
	case stripe.EventTypeCustomerSubscriptionDeleted:
		return handleCustomerSubscriptionDeleted(ctx, tx, db, ent, evt)
	default:
		log.Printf("billing: webhook event_id=%s type=%s action=ignored_unsupported", evt.ID, evt.Type)
		return nil
	}
}

func handleCheckoutSessionCompleted(ctx context.Context, tx *sql.Tx, db *sql.DB, ent *entitlement.Store, evt stripe.Event) error {
	var sess stripe.CheckoutSession
	if err := json.Unmarshal(evt.Data.Raw, &sess); err != nil {
		return fmt.Errorf("decode checkout session: %w", err)
	}

	if sess.Mode != stripe.CheckoutSessionModeSubscription {
		log.Printf("billing: webhook event_id=%s type=%s action=skipped reason=mode_not_subscription", evt.ID, evt.Type)
		return nil
	}

	if sess.Metadata == nil {
		log.Printf("billing: webhook event_id=%s type=%s action=skipped reason=missing_metadata", evt.ID, evt.Type)
		return nil
	}
	if strings.TrimSpace(sess.Metadata[MetaKeyPlan]) != PlanPro {
		log.Printf("billing: webhook event_id=%s type=%s action=skipped reason=plan_not_pro", evt.ID, evt.Type)
		return nil
	}
	if strings.TrimSpace(sess.Metadata[MetaKeySource]) != SourceStripeWeb {
		log.Printf("billing: webhook event_id=%s type=%s action=skipped reason=source_not_stripe_web", evt.ID, evt.Type)
		return nil
	}

	uid, ok := resolveUserForCheckout(ctx, db, &sess)
	if !ok {
		log.Printf("billing: webhook event_id=%s type=%s action=skipped reason=user_unresolved", evt.ID, evt.Type)
		return nil
	}

	if err := ent.UpsertStripePro(ctx, tx, uid, sql.NullTime{}); err != nil {
		return fmt.Errorf("upsert stripe pro (checkout): %w", err)
	}
	log.Printf("billing: webhook event_id=%s type=%s user_id=%s action=stripe_pro_activated reason=checkout_completed", evt.ID, evt.Type, uid)
	return nil
}

func resolveUserForCheckout(ctx context.Context, db *sql.DB, sess *stripe.CheckoutSession) (uuid.UUID, bool) {
	if u, ok := UserIDFromMetadata(sess.Metadata); ok {
		return u, true
	}
	if ref := strings.TrimSpace(sess.ClientReferenceID); ref != "" {
		if u, err := uuid.Parse(ref); err == nil {
			return u, true
		}
	}
	cid := stripeCustomerIDFromCheckoutSession(sess)
	if cid == "" {
		return uuid.Nil, false
	}
	u, err := UserIDFromStripeCustomer(ctx, db, cid)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("billing: stripe_customers lookup err=%v", err)
		}
		return uuid.Nil, false
	}
	return u, true
}

func handleCustomerSubscriptionUpdated(ctx context.Context, tx *sql.Tx, db *sql.DB, ent *entitlement.Store, evt stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(evt.Data.Raw, &sub); err != nil {
		return fmt.Errorf("decode subscription: %w", err)
	}

	uid, ok := resolveUserForSubscription(ctx, db, &sub)
	if !ok {
		log.Printf("billing: webhook event_id=%s type=%s action=skipped reason=user_unresolved subscription_id=%s", evt.ID, evt.Type, sub.ID)
		return nil
	}

	grant, exp := subscriptionProAccess(&sub)
	if grant {
		if err := ent.UpsertStripePro(ctx, tx, uid, exp); err != nil {
			return fmt.Errorf("upsert stripe pro (subscription): %w", err)
		}
		log.Printf("billing: webhook event_id=%s type=%s user_id=%s action=stripe_pro_upsert status=%s cancel_at_period_end=%v",
			evt.ID, evt.Type, uid, sub.Status, sub.CancelAtPeriodEnd)
		return nil
	}

	if err := ent.RevokeStripePro(ctx, tx, uid); err != nil {
		return fmt.Errorf("revoke stripe pro (subscription): %w", err)
	}
	log.Printf("billing: webhook event_id=%s type=%s user_id=%s action=stripe_pro_revoked status=%s", evt.ID, evt.Type, uid, sub.Status)
	return nil
}

func handleCustomerSubscriptionDeleted(ctx context.Context, tx *sql.Tx, db *sql.DB, ent *entitlement.Store, evt stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(evt.Data.Raw, &sub); err != nil {
		return fmt.Errorf("decode subscription: %w", err)
	}

	uid, ok := resolveUserForSubscription(ctx, db, &sub)
	if !ok {
		log.Printf("billing: webhook event_id=%s type=%s action=skipped reason=user_unresolved subscription_id=%s", evt.ID, evt.Type, sub.ID)
		return nil
	}

	if err := ent.RevokeStripePro(ctx, tx, uid); err != nil {
		return fmt.Errorf("revoke stripe pro (deleted): %w", err)
	}
	log.Printf("billing: webhook event_id=%s type=%s user_id=%s action=stripe_pro_revoked reason=subscription_deleted", evt.ID, evt.Type, uid)
	return nil
}

func resolveUserForSubscription(ctx context.Context, db *sql.DB, sub *stripe.Subscription) (uuid.UUID, bool) {
	if u, ok := UserIDFromMetadata(sub.Metadata); ok {
		return u, true
	}
	cid := stripeCustomerIDFromSubscription(sub)
	if cid == "" {
		return uuid.Nil, false
	}
	u, err := UserIDFromStripeCustomer(ctx, db, cid)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("billing: stripe_customers lookup err=%v", err)
		}
		return uuid.Nil, false
	}
	return u, true
}

func subscriptionProAccess(sub *stripe.Subscription) (grant bool, expiresAt sql.NullTime) {
	switch sub.Status {
	case stripe.SubscriptionStatusActive, stripe.SubscriptionStatusTrialing:
		if sub.CancelAtPeriodEnd && sub.CurrentPeriodEnd > 0 {
			t := time.Unix(sub.CurrentPeriodEnd, 0).UTC()
			return true, sql.NullTime{Time: t, Valid: true}
		}
		return true, sql.NullTime{}
	case stripe.SubscriptionStatusPastDue:
		return true, sql.NullTime{}
	case stripe.SubscriptionStatusCanceled,
		stripe.SubscriptionStatusUnpaid,
		stripe.SubscriptionStatusIncompleteExpired,
		stripe.SubscriptionStatusPaused,
		stripe.SubscriptionStatusIncomplete:
		return false, sql.NullTime{}
	default:
		return false, sql.NullTime{}
	}
}
