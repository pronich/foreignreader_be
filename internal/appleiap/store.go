package appleiap

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Store struct {
	DB *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{DB: db}
}

var ErrUnknownProduct = errors.New("unknown apple product id")

func (s *Store) EnsureProductMapping(ctx context.Context, tx *sql.Tx, appleProductID, productCode string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO apple_iap_products (apple_product_id, product_code, created_at, updated_at)
		VALUES ($1, $2, now(), now())
		ON CONFLICT (apple_product_id) DO UPDATE
		SET product_code = EXCLUDED.product_code,
		    updated_at = now()
	`, appleProductID, productCode)
	return err
}

func (s *Store) ProductCodeForAppleProductID(ctx context.Context, appleProductID string) (string, error) {
	var code string
	err := s.DB.QueryRowContext(ctx, `
		SELECT product_code
		FROM apple_iap_products
		WHERE apple_product_id = $1
	`, appleProductID).Scan(&code)
	if err != nil {
		return "", err
	}
	return code, nil
}

type SubscriptionUpsert struct {
	UserID                uuid.UUID
	AppleProductID         string
	ProductCode            string
	OriginalTransactionID  string
	LatestTransactionID    string
	Status                string
	Environment            string
	PurchasedAt            *time.Time
	ExpiresAt              *time.Time
}

func (s *Store) UpsertSubscription(ctx context.Context, tx *sql.Tx, in SubscriptionUpsert) error {
	var purchasedAt any = nil
	if in.PurchasedAt != nil {
		purchasedAt = in.PurchasedAt.UTC()
	}
	var expiresAt any = nil
	if in.ExpiresAt != nil {
		expiresAt = in.ExpiresAt.UTC()
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO apple_iap_subscriptions (
			user_id, apple_product_id, product_code,
			original_transaction_id, latest_transaction_id,
			status, environment, purchased_at, expires_at,
			created_at, updated_at
		) VALUES (
			$1, $2, $3,
			$4, NULLIF($5, ''),
			$6, $7, $8, $9,
			now(), now()
		)
		ON CONFLICT (original_transaction_id) DO UPDATE
		SET user_id = EXCLUDED.user_id,
		    apple_product_id = EXCLUDED.apple_product_id,
		    product_code = EXCLUDED.product_code,
		    latest_transaction_id = EXCLUDED.latest_transaction_id,
		    status = EXCLUDED.status,
		    environment = EXCLUDED.environment,
		    purchased_at = EXCLUDED.purchased_at,
		    expires_at = EXCLUDED.expires_at,
		    updated_at = now()
	`, in.UserID, in.AppleProductID, in.ProductCode, in.OriginalTransactionID, in.LatestTransactionID, in.Status, in.Environment, purchasedAt, expiresAt)
	return err
}

