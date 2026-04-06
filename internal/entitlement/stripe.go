package entitlement

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// UpsertStripePro revokes active Stripe Pro rows for the user and inserts a new active Pro row (source = stripe).
func (s *Store) UpsertStripePro(ctx context.Context, tx *sql.Tx, userID uuid.UUID, expiresAt sql.NullTime) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE entitlements
		SET status = 'revoked', updated_at = now()
		WHERE user_id = $1
		  AND product_code = $2
		  AND source = 'stripe'
		  AND status = 'active'
	`, userID, ProductPro); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO entitlements (user_id, product_code, status, source, starts_at, expires_at, created_at, updated_at)
		VALUES ($1, $2, 'active', 'stripe', now(), $3, now(), now())
	`, userID, ProductPro, expiresAt)
	return err
}

// RevokeStripePro revokes active Stripe-sourced Pro entitlements for the user.
func (s *Store) RevokeStripePro(ctx context.Context, tx *sql.Tx, userID uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE entitlements
		SET status = 'revoked', updated_at = now()
		WHERE user_id = $1
		  AND product_code = $2
		  AND source = 'stripe'
		  AND status = 'active'
	`, userID, ProductPro)
	return err
}
