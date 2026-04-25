package appleiap

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// We intentionally parse Apple-signed JWS payloads (returned by Apple server API).
// This step does not yet validate the JWS signature chain; the server-to-server call
// is the source of truth. Signature verification can be added later if needed.

type TransactionPayload struct {
	TransactionID         string `json:"transactionId"`
	OriginalTransactionID string `json:"originalTransactionId"`
	ProductID             string `json:"productId"`
	PurchaseDate          int64  `json:"purchaseDate"` // ms since epoch
	OriginalPurchaseDate  int64  `json:"originalPurchaseDate,omitempty"`
	ExpiresDate           int64  `json:"expiresDate,omitempty"` // ms since epoch
	Environment           string `json:"environment,omitempty"` // Sandbox|Production
	RevocationDate        int64  `json:"revocationDate,omitempty"`
}

func DecodeJWSPayload(jwsCompact string, out any) error {
	parts := strings.Split(strings.TrimSpace(jwsCompact), ".")
	if len(parts) != 3 {
		return errors.New("invalid jws format")
	}
	payloadB64 := parts[1]
	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return errors.New("invalid jws payload encoding")
	}
	if err := json.Unmarshal(payloadBytes, out); err != nil {
		return err
	}
	return nil
}

func VerifyAndDecodeJWSPayload(jwsCompact string, out any) error {
	b, err := VerifyJWS(jwsCompact)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (p TransactionPayload) ExpiresAt() *time.Time {
	if p.ExpiresDate <= 0 {
		return nil
	}
	t := time.UnixMilli(p.ExpiresDate).UTC()
	return &t
}

func (p TransactionPayload) PurchasedAt() *time.Time {
	if p.PurchaseDate <= 0 {
		return nil
	}
	t := time.UnixMilli(p.PurchaseDate).UTC()
	return &t
}
