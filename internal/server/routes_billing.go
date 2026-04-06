package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/billing"
	"foreignreader_be/internal/config"
	"foreignreader_be/internal/entitlement"
	"foreignreader_be/internal/stripeapp"

	"github.com/stripe/stripe-go/v81"
)

func handleBillingCheckoutSession(cfg config.Config, ent *entitlement.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authentication context")
			return
		}

		api := stripeapp.Client()
		if api == nil || strings.TrimSpace(cfg.StripePriceIDPro) == "" {
			writeAPIError(w, http.StatusServiceUnavailable, "billing_unavailable", "Stripe billing is not configured")
			return
		}

		stripeCustomerID, created, err := billing.EnsureStripeCustomerID(r.Context(), ent.DB, api, u)
		if err != nil {
			log.Printf("billing: ensure stripe customer user_id=%s err=%v", u.ID, err)
			var se *stripe.Error
			if errors.As(err, &se) {
				log.Printf("billing: stripe error user_id=%s op=customer_create code=%s type=%s status=%d request_id=%s msg=%s",
					u.ID, se.Code, se.Type, se.HTTPStatusCode, se.RequestID, se.Msg)
				writeAPIError(w, http.StatusBadGateway, "stripe_error", "could not prepare billing customer")
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not prepare billing customer")
			return
		}
		if created {
			log.Printf("billing: stripe customer created user_id=%s stripe_customer_id=%s", u.ID, stripeCustomerID)
		} else {
			log.Printf("billing: stripe customer reused user_id=%s stripe_customer_id=%s", u.ID, stripeCustomerID)
		}

		checkoutURL, sessionID, err := billing.CreateProCheckoutSession(api, cfg, stripeCustomerID, u.ID)
		if err != nil {
			log.Printf("billing: checkout session user_id=%s err=%v", u.ID, err)
			var se *stripe.Error
			if errors.As(err, &se) {
				log.Printf("billing: stripe error user_id=%s op=checkout_session code=%s type=%s status=%d request_id=%s msg=%s",
					u.ID, se.Code, se.Type, se.HTTPStatusCode, se.RequestID, se.Msg)
				writeAPIError(w, http.StatusBadGateway, "stripe_error", "could not create checkout session")
				return
			}
			if errors.Is(err, billing.ErrInvalidRedirectBase) {
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "invalid billing redirect configuration")
				return
			}
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not create checkout session")
			return
		}

		log.Printf("billing: checkout session created user_id=%s session_id=%s", u.ID, sessionID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(billingCheckoutSessionResponse{
			CheckoutURL: checkoutURL,
			SessionID:   sessionID,
		})
	}
}

type billingCheckoutSessionResponse struct {
	CheckoutURL string `json:"checkoutUrl"`
	SessionID   string `json:"sessionId"`
}
