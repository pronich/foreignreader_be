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

type SubscriptionRow struct {
	UserID             uuid.UUID
	AppleProductID     string
	ProductCode        string
	OriginalTransactionID string
	LatestTransactionID   sql.NullString
	Status             string
	Environment        string
	ExpiresAt          sql.NullTime
}

func (s *Store) SubscriptionByOriginalTransactionID(ctx context.Context, tx *sql.Tx, originalTx string) (SubscriptionRow, error) {
	var row SubscriptionRow
	err := tx.QueryRowContext(ctx, `
		SELECT user_id, apple_product_id, product_code, original_transaction_id, latest_transaction_id, status, environment, expires_at
		FROM apple_iap_subscriptions
		WHERE original_transaction_id = $1
	`, originalTx).Scan(
		&row.UserID,
		&row.AppleProductID,
		&row.ProductCode,
		&row.OriginalTransactionID,
		&row.LatestTransactionID,
		&row.Status,
		&row.Environment,
		&row.ExpiresAt,
	)
	return row, err
}

func (s *Store) UpdateSubscriptionStateByOriginalTransactionID(
	ctx context.Context,
	tx *sql.Tx,
	originalTx string,
	latestTx string,
	status string,
	environment string,
	expiresAt *time.Time,
) error {
	var exp any = nil
	if expiresAt != nil {
		exp = expiresAt.UTC()
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE apple_iap_subscriptions
		SET latest_transaction_id = NULLIF($2, ''),
		    status = $3,
		    environment = $4,
		    expires_at = $5,
		    updated_at = now()
		WHERE original_transaction_id = $1
	`, originalTx, latestTx, status, environment, exp)
	return err
}

func (s *Store) InsertAppleEvent(ctx context.Context, tx *sql.Tx, e AppleEventInsert) (inserted bool, err error) {
	var id uuid.UUID
	err = tx.QueryRowContext(ctx, `
		INSERT INTO apple_iap_events (notification_uuid, notification_type, subtype, original_transaction_id, transaction_id, signed_payload, created_at)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, now())
		ON CONFLICT (notification_uuid) DO NOTHING
		RETURNING id
	`, e.NotificationUUID, e.NotificationType, e.Subtype, e.OriginalTransactionID, e.TransactionID, e.SignedPayload).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) MarkAppleEventProcessed(ctx context.Context, tx *sql.Tx, notificationUUID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE apple_iap_events
		SET processed_at = now()
		WHERE notification_uuid = $1 AND processed_at IS NULL
	`, notificationUUID)
	return err
}

type AppleEventInsert struct {
	NotificationUUID     string
	NotificationType     string
	Subtype              string
	OriginalTransactionID string
	TransactionID        string
	SignedPayload        string
}

