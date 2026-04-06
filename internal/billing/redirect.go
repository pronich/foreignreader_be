package billing

import (
	"errors"
	"fmt"
	"net/url"
)

// ErrInvalidRedirectBase means STRIPE_REDIRECT_URL (or derived URL) could not be parsed.
var ErrInvalidRedirectBase = errors.New("invalid redirect base URL")

const (
	checkoutQueryKey   = "checkout"
	checkoutSuccessVal = "success"
	checkoutCancelVal  = "cancel"
)

// CheckoutSuccessURL appends a deterministic success marker to the base redirect URL.
func CheckoutSuccessURL(base string) (string, error) {
	return withQueryParam(base, checkoutQueryKey, checkoutSuccessVal)
}

// CheckoutCancelURL appends a deterministic cancel marker to the base redirect URL.
func CheckoutCancelURL(base string) (string, error) {
	return withQueryParam(base, checkoutQueryKey, checkoutCancelVal)
}

func withQueryParam(base, key, val string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidRedirectBase, err)
	}
	q := u.Query()
	q.Set(key, val)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
