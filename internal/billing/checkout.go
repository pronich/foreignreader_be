package billing

import (
	"fmt"
	"strings"

	"foreignreader_be/internal/config"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/client"
)

// CreateProCheckoutSession creates a subscription-mode Checkout Session for the Pro price.
func CreateProCheckoutSession(api *client.API, cfg config.Config, stripeCustomerID string, userID uuid.UUID) (checkoutURL, sessionID string, err error) {
	priceID := strings.TrimSpace(cfg.StripePriceIDPro)
	if priceID == "" {
		return "", "", fmt.Errorf("STRIPE_PRICE_ID_PRO is not set")
	}

	successURL, err := CheckoutSuccessURL(cfg.StripeRedirectURL)
	if err != nil {
		return "", "", err
	}
	cancelURL, err := CheckoutCancelURL(cfg.StripeRedirectURL)
	if err != nil {
		return "", "", err
	}

	meta := stripeMetadata(userID)
	params := &stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		Customer:   stripe.String(stripeCustomerID),
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		Metadata:          meta,
		ClientReferenceID: stripe.String(userID.String()),
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: meta,
		},
	}

	sess, err := api.CheckoutSessions.New(params)
	if err != nil {
		return "", "", fmt.Errorf("stripe checkout session create: %w", err)
	}
	if sess == nil || strings.TrimSpace(sess.URL) == "" {
		return "", "", fmt.Errorf("stripe checkout session missing URL")
	}
	return sess.URL, sess.ID, nil
}
