package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxAppStorefrontLen = 16

// NormalizeAppStorefront trims, uppercases, and checks length. Returns false if empty or too long.
func NormalizeAppStorefront(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	s = strings.ToUpper(s)
	if len(s) > maxAppStorefrontLen {
		return "", false
	}
	return s, true
}

// UpdateAppStorefront sets the latest known App Store storefront for the user (only those columns).
func (s *Store) UpdateAppStorefront(ctx context.Context, userID uuid.UUID, storefront string) (updatedAt time.Time, err error) {
	err = s.DB.QueryRowContext(ctx, `
		UPDATE users
		SET app_storefront = $1,
		    app_storefront_updated_at = now()
		WHERE id = $2
		RETURNING app_storefront_updated_at
	`, storefront, userID).Scan(&updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, sql.ErrNoRows
	}
	return updatedAt, err
}
