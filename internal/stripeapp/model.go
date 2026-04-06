package stripeapp

import (
	"time"

	"github.com/google/uuid"
)

// StripeCustomer is a row in stripe_customers (user ↔ Stripe customer mapping).
type StripeCustomer struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	StripeCustomerID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
