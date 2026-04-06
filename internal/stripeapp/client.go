package stripeapp

import (
	"log"
	"strings"
	"sync"

	"github.com/stripe/stripe-go/v81/client"
)

var (
	mu       sync.RWMutex
	stripeAPI *client.API
)

// Init configures the shared Stripe API client from the secret key.
// If secretKey is empty, the client is left nil (typical in local dev).
func Init(secretKey string) {
	mu.Lock()
	defer mu.Unlock()

	key := strings.TrimSpace(secretKey)
	if key == "" {
		stripeAPI = nil
		log.Printf("stripe: API client not initialized (STRIPE_SECRET_KEY empty)")
		return
	}

	stripeAPI = client.New(key, nil)
	log.Printf("stripe: API client initialized")
}

// Client returns the shared Stripe client, or nil when STRIPE_SECRET_KEY was not set at Init.
func Client() *client.API {
	mu.RLock()
	defer mu.RUnlock()
	return stripeAPI
}
