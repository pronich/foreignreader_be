package entitlement

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

const ProductPro = "pro"

// Store reads and writes entitlement rows.
type Store struct {
	DB *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{DB: db}
}

// Entitlement is a row from entitlements.
type Entitlement struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	ProductCode string
	Status      string
	Source      string
	StartsAt    time.Time
	ExpiresAt   sql.NullTime
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// HasActivePro returns true when the user has an active Pro entitlement per server rules.
func (s *Store) HasActivePro(ctx context.Context, userID uuid.UUID) (bool, error) {
	var ok bool
	err := s.DB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM entitlements
			WHERE user_id = $1
			  AND product_code = $2
			  AND status = 'active'
			  AND (expires_at IS NULL OR expires_at > now())
		)
	`, userID, ProductPro).Scan(&ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}

// ListByUser returns all entitlements for a user, newest first.
func (s *Store) ListByUser(ctx context.Context, userID uuid.UUID) ([]Entitlement, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, product_code, status, source, starts_at, expires_at, created_at, updated_at
		FROM entitlements
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Entitlement
	for rows.Next() {
		var e Entitlement
		if err := rows.Scan(&e.ID, &e.UserID, &e.ProductCode, &e.Status, &e.Source, &e.StartsAt, &e.ExpiresAt, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetDevPro toggles a development-managed Pro entitlement for the user (source = dev).
// When active is true, existing active dev Pro rows are revoked and a new active row is inserted.
// When active is false, active dev Pro rows are revoked.
func (s *Store) SetDevPro(ctx context.Context, userID uuid.UUID, active bool) (Entitlement, error) {
	if s.DB == nil {
		return Entitlement{}, errors.New("database not configured")
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Entitlement{}, err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		UPDATE entitlements
		SET status = 'revoked', updated_at = now()
		WHERE user_id = $1
		  AND product_code = $2
		  AND source = 'dev'
		  AND status = 'active'
	`, userID, ProductPro)
	if err != nil {
		return Entitlement{}, err
	}

	if !active {
		if err := tx.Commit(); err != nil {
			return Entitlement{}, err
		}
		return Entitlement{
			ProductCode: ProductPro,
			Status:      "revoked",
			Source:      "dev",
		}, nil
	}

	var id uuid.UUID
	err = tx.QueryRowContext(ctx, `
		INSERT INTO entitlements (user_id, product_code, status, source, starts_at, expires_at, created_at, updated_at)
		VALUES ($1, $2, 'active', 'dev', now(), NULL, now(), now())
		RETURNING id
	`, userID, ProductPro).Scan(&id)
	if err != nil {
		return Entitlement{}, err
	}

	var e Entitlement
	err = tx.QueryRowContext(ctx, `
		SELECT id, user_id, product_code, status, source, starts_at, expires_at, created_at, updated_at
		FROM entitlements
		WHERE id = $1
	`, id).Scan(&e.ID, &e.UserID, &e.ProductCode, &e.Status, &e.Source, &e.StartsAt, &e.ExpiresAt, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return Entitlement{}, err
	}

	if err := tx.Commit(); err != nil {
		return Entitlement{}, err
	}
	return e, nil
}
