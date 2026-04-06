package billing

import (
	"context"
	"database/sql"
	"strings"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v81"
)

// UserIDFromMetadata returns the internal user id from Stripe metadata when present and valid.
func UserIDFromMetadata(meta map[string]string) (uuid.UUID, bool) {
	if meta == nil {
		return uuid.Nil, false
	}
	raw := strings.TrimSpace(meta[MetaKeyUserID])
	if raw == "" {
		return uuid.Nil, false
	}
	u, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return u, true
}

// UserIDFromStripeCustomer looks up the internal user id from stripe_customers.
func UserIDFromStripeCustomer(ctx context.Context, db *sql.DB, stripeCustomerID string) (uuid.UUID, error) {
	stripeCustomerID = strings.TrimSpace(stripeCustomerID)
	if stripeCustomerID == "" {
		return uuid.Nil, sql.ErrNoRows
	}
	var uid uuid.UUID
	err := db.QueryRowContext(ctx, `
		SELECT user_id FROM stripe_customers WHERE stripe_customer_id = $1
	`, stripeCustomerID).Scan(&uid)
	return uid, err
}

func stripeCustomerIDFromSubscription(sub *stripe.Subscription) string {
	if sub == nil || sub.Customer == nil {
		return ""
	}
	return strings.TrimSpace(sub.Customer.ID)
}

func stripeCustomerIDFromCheckoutSession(sess *stripe.CheckoutSession) string {
	if sess == nil || sess.Customer == nil {
		return ""
	}
	return strings.TrimSpace(sess.Customer.ID)
}
