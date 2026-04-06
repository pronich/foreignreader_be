package billing

import (
	"fmt"
	"strings"

	"foreignreader_be/internal/config"

	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/client"
)

// CreateCustomerPortalSession creates a Stripe Customer Portal session for return to STRIPE_REDIRECT_URL.
func CreateCustomerPortalSession(api *client.API, cfg config.Config, stripeCustomerID string) (portalURL, sessionID string, err error) {
	stripeCustomerID = strings.TrimSpace(stripeCustomerID)
	if stripeCustomerID == "" {
		return "", "", fmt.Errorf("stripe customer id is empty")
	}
	returnURL := strings.TrimSpace(cfg.StripeRedirectURL)
	if returnURL == "" {
		return "", "", fmt.Errorf("STRIPE_REDIRECT_URL is not set")
	}

	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(stripeCustomerID),
		ReturnURL: stripe.String(returnURL),
	}

	sess, err := api.BillingPortalSessions.New(params)
	if err != nil {
		return "", "", fmt.Errorf("stripe billing portal session: %w", err)
	}
	if sess == nil || strings.TrimSpace(sess.URL) == "" {
		return "", "", fmt.Errorf("stripe billing portal session missing URL")
	}
	return sess.URL, sess.ID, nil
}
