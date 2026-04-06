package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"foreignreader_be/internal/auth"

	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/client"
)

// EnsureStripeCustomerID returns the Stripe customer id for the user, creating the
// Stripe customer and DB row on first use. Concurrent requests are safe: at most one
// mapping per user; a losing race deletes the extra Stripe customer.
func EnsureStripeCustomerID(ctx context.Context, db *sql.DB, api *client.API, u auth.User) (stripeCustomerID string, created bool, err error) {
	var existing string
	err = db.QueryRowContext(ctx, `SELECT stripe_customer_id FROM stripe_customers WHERE user_id = $1`, u.ID).Scan(&existing)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("stripe_customers lookup: %w", err)
	}

	meta := stripeMetadata(u.ID)
	cp := &stripe.CustomerParams{
		Metadata: meta,
	}
	if u.Email.Valid {
		if em := strings.TrimSpace(u.Email.String); em != "" {
			cp.Email = stripe.String(em)
		}
	}

	cus, err := api.Customers.New(cp)
	if err != nil {
		return "", false, fmt.Errorf("stripe customer create: %w", err)
	}
	newID := cus.ID

	var inserted string
	err = db.QueryRowContext(ctx, `
		INSERT INTO stripe_customers (user_id, stripe_customer_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO NOTHING
		RETURNING stripe_customer_id
	`, u.ID, newID).Scan(&inserted)
	if err == nil {
		return inserted, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		if _, delErr := api.Customers.Del(newID, nil); delErr != nil {
			log.Printf("billing: orphan stripe customer cleanup failed user_id=%s stripe_customer_id=%s err=%v", u.ID, newID, delErr)
		}
		return "", false, fmt.Errorf("stripe_customers insert: %w", err)
	}

	// Lost a race: another request inserted first.
	err = db.QueryRowContext(ctx, `SELECT stripe_customer_id FROM stripe_customers WHERE user_id = $1`, u.ID).Scan(&existing)
	if err != nil {
		if _, delErr := api.Customers.Del(newID, nil); delErr != nil {
			log.Printf("billing: orphan stripe customer cleanup failed user_id=%s stripe_customer_id=%s err=%v", u.ID, newID, delErr)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, fmt.Errorf("stripe_customers missing after conflict user_id=%s", u.ID)
		}
		return "", false, fmt.Errorf("stripe_customers re-select: %w", err)
	}
	if _, delErr := api.Customers.Del(newID, nil); delErr != nil {
		log.Printf("billing: orphan stripe customer cleanup failed user_id=%s stripe_customer_id=%s err=%v", u.ID, newID, delErr)
	}
	return existing, false, nil
}
