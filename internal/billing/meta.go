package billing

import (
	"github.com/google/uuid"
)

// Stripe metadata keys for customers and checkout sessions (webhooks / reconciliation).
const (
	MetaKeyUserID = "user_id"
	MetaKeySource = "source"
	MetaKeyPlan   = "plan"
)

const (
	SourceStripeWeb = "stripe_web"
	PlanPro         = "pro"
)

func stripeMetadata(userID uuid.UUID) map[string]string {
	return map[string]string{
		MetaKeyUserID: userID.String(),
		MetaKeySource: SourceStripeWeb,
		MetaKeyPlan:   PlanPro,
	}
}
