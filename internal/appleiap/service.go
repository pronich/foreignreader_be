package appleiap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"foreignreader_be/internal/entitlement"

	"github.com/google/uuid"
)

type Service struct {
	Client *Client
	Store  *Store
	Ent    *entitlement.Store

	ProProductID string
}

var (
	ErrNotEntitled    = errors.New("subscription not active")
	ErrUnknownProductID = errors.New("unknown apple product id")
)

type ValidateResult struct {
	ProductCode string
	Status      string
	ExpiresAt   *time.Time
	Environment string
}

func NewService(client *Client, store *Store, ent *entitlement.Store, proProductID string) (*Service, error) {
	if client == nil || store == nil || ent == nil {
		return nil, errors.New("missing dependencies")
	}
	if strings.TrimSpace(proProductID) == "" {
		return nil, errors.New("missing pro product id")
	}
	return &Service{Client: client, Store: store, Ent: ent, ProProductID: strings.TrimSpace(proProductID)}, nil
}

// ValidateTransaction performs server-to-server validation using Apple's App Store Server API and updates:
// - apple_iap_products mapping (for the configured Pro product)
// - apple_iap_subscriptions state (upsert by original_transaction_id)
// - entitlements (grant/revoke Pro via source=apple_iap)
func (s *Service) ValidateTransaction(ctx context.Context, userID uuid.UUID, transactionID string) (*ValidateResult, error) {
	resp, err := s.Client.GetTransactionInfo(ctx, transactionID)
	if err != nil {
		return nil, err
	}

	var payload TransactionPayload
	if err := DecodeJWSPayload(resp.SignedTransactionInfo, &payload); err != nil {
		return nil, fmt.Errorf("decode signedTransactionInfo: %w", err)
	}

	productID := strings.TrimSpace(payload.ProductID)
	if productID == "" {
		return nil, errors.New("apple transaction missing productId")
	}

	// Minimal product mapping: only Pro for now, via env config.
	if productID != s.ProProductID {
		return nil, ErrUnknownProductID
	}
	productCode := entitlement.ProductPro

	origTx := strings.TrimSpace(payload.OriginalTransactionID)
	if origTx == "" {
		return nil, errors.New("apple transaction missing originalTransactionId")
	}

	latestTx := strings.TrimSpace(payload.TransactionID)
	env := strings.ToLower(strings.TrimSpace(payload.Environment))
	if env == "" {
		// Default to the client environment; Apple payload should include it, but be defensive.
		env = string(s.Client.Env)
	}
	if env != "sandbox" && env != "production" {
		env = string(s.Client.Env)
	}

	now := time.Now().UTC()
	var expiresAt *time.Time = payload.ExpiresAt()
	purchasedAt := payload.PurchasedAt()

	status := "active"
	if payload.RevocationDate > 0 {
		status = "revoked"
	} else if expiresAt != nil && !expiresAt.After(now) {
		status = "expired"
	}

	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Keep mapping table populated (future: admin-managed mappings).
	if err := s.Store.EnsureProductMapping(ctx, tx, productID, productCode); err != nil {
		return nil, err
	}

	if err := s.Store.UpsertSubscription(ctx, tx, SubscriptionUpsert{
		UserID:               userID,
		AppleProductID:       productID,
		ProductCode:          productCode,
		OriginalTransactionID: origTx,
		LatestTransactionID:   latestTx,
		Status:               status,
		Environment:          env,
		PurchasedAt:          purchasedAt,
		ExpiresAt:            expiresAt,
	}); err != nil {
		return nil, err
	}

	// Update entitlement immediately.
	switch status {
	case "active":
		var exp sql.NullTime
		if expiresAt != nil {
			exp = sql.NullTime{Time: expiresAt.UTC(), Valid: true}
		}
		if err := s.Ent.UpsertAppleIAPPro(ctx, tx, userID, exp); err != nil {
			return nil, err
		}
	default:
		if err := s.Ent.RevokeAppleIAPPro(ctx, tx, userID); err != nil {
			return nil, err
		}
		return nil, ErrNotEntitled
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &ValidateResult{
		ProductCode: productCode,
		Status:      status,
		ExpiresAt:   expiresAt,
		Environment: env,
	}, nil
}

