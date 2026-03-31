package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Store performs minimal auth-related persistence.
type Store struct {
	DB *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{DB: db}
}

// HasIdentity reports whether an auth_identity row exists for provider + provider user id.
func (s *Store) HasIdentity(ctx context.Context, provider, providerUserID string) (bool, error) {
	var ok bool
	err := s.DB.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM auth_identities WHERE provider = $1 AND provider_user_id = $2
		)
	`, provider, providerUserID).Scan(&ok)
	return ok, err
}

// MockClaimsInput is normalized mock provider data (Apple / Google dev login).
type MockClaimsInput struct {
	Sub           string
	Email         *string
	EmailVerified *bool
	DisplayName   *string
	AvatarURL     *string
}

// UserByID loads a user by primary key.
func (s *Store) UserByID(ctx context.Context, id uuid.UUID) (User, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, display_name, avatar_url, email, email_verified
		FROM users
		WHERE id = $1
	`, id)
	var u User
	var dn, av, em sql.NullString
	err := row.Scan(&u.ID, &dn, &av, &em, &u.EmailVerified)
	if err != nil {
		return User{}, err
	}
	u.DisplayName = dn
	u.AvatarURL = av
	u.Email = em
	return u, nil
}

// LoginOrRegisterMock finds or creates auth_identity + user, applies merge rules, returns the user row.
func (s *Store) LoginOrRegisterMock(ctx context.Context, provider string, in *MockClaimsInput) (User, error) {
	if s.DB == nil {
		return User{}, errors.New("database not configured")
	}
	sub := strings.TrimSpace(in.Sub)
	if sub == "" {
		return User{}, errors.New("empty provider subject")
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var userID uuid.UUID
	err = tx.QueryRowContext(ctx, `
		SELECT u.id
		FROM auth_identities ai
		JOIN users u ON u.id = ai.user_id
		WHERE ai.provider = $1 AND ai.provider_user_id = $2
	`, provider, sub).Scan(&userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return User{}, err
	}

	if errors.Is(err, sql.ErrNoRows) {
		userID, err = s.insertUserAndIdentity(ctx, tx, provider, sub, in)
		if err != nil {
			return User{}, err
		}
	} else {
		if err := s.mergeUser(ctx, tx, userID, in); err != nil {
			return User{}, err
		}
		if err := s.mergeIdentityEmail(ctx, tx, provider, sub, in); err != nil {
			return User{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return s.UserByID(ctx, userID)
}

func (s *Store) insertUserAndIdentity(ctx context.Context, tx *sql.Tx, provider, providerUserID string, in *MockClaimsInput) (uuid.UUID, error) {
	dn, av, em := sqlNullStringsFromMock(in)
	ev := emailVerifiedForInsert(in)

	var userID uuid.UUID
	err := tx.QueryRowContext(ctx, `
		INSERT INTO users (display_name, avatar_url, email, email_verified, created_at, updated_at)
		VALUES ($1, $2, $3, $4, now(), now())
		RETURNING id
	`, dn, av, em, ev).Scan(&userID)
	if err != nil {
		return uuid.Nil, err
	}

	pe := sqlNullStringPtr(providerEmailFromMock(in))
	_, err = tx.ExecContext(ctx, `
		INSERT INTO auth_identities (user_id, provider, provider_user_id, provider_email, created_at, updated_at)
		VALUES ($1, $2, $3, $4, now(), now())
	`, userID, provider, providerUserID, pe)
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

func sqlNullStringPtr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: t, Valid: true}
}

func sqlNullStringsFromMock(in *MockClaimsInput) (dn, av, em sql.NullString) {
	if in.DisplayName != nil && strings.TrimSpace(*in.DisplayName) != "" {
		dn = sql.NullString{String: strings.TrimSpace(*in.DisplayName), Valid: true}
	}
	if in.AvatarURL != nil && strings.TrimSpace(*in.AvatarURL) != "" {
		av = sql.NullString{String: strings.TrimSpace(*in.AvatarURL), Valid: true}
	}
	if in.Email != nil && strings.TrimSpace(*in.Email) != "" {
		em = sql.NullString{String: strings.TrimSpace(*in.Email), Valid: true}
	}
	return dn, av, em
}

func emailVerifiedForInsert(in *MockClaimsInput) bool {
	if in.EmailVerified != nil {
		return *in.EmailVerified
	}
	return false
}

func (s *Store) mergeUser(ctx context.Context, tx *sql.Tx, userID uuid.UUID, in *MockClaimsInput) error {
	var parts []string
	var args []any
	n := 1

	if in.DisplayName != nil && strings.TrimSpace(*in.DisplayName) != "" {
		parts = append(parts, "display_name = $"+strconv.Itoa(n))
		args = append(args, strings.TrimSpace(*in.DisplayName))
		n++
	}
	if in.AvatarURL != nil && strings.TrimSpace(*in.AvatarURL) != "" {
		parts = append(parts, "avatar_url = $"+strconv.Itoa(n))
		args = append(args, strings.TrimSpace(*in.AvatarURL))
		n++
	}
	if in.Email != nil && strings.TrimSpace(*in.Email) != "" {
		parts = append(parts, "email = $"+strconv.Itoa(n))
		args = append(args, strings.TrimSpace(*in.Email))
		n++
	}
	if in.EmailVerified != nil {
		parts = append(parts, "email_verified = $"+strconv.Itoa(n))
		args = append(args, *in.EmailVerified)
		n++
	}
	if len(parts) == 0 {
		return nil
	}

	parts = append(parts, "updated_at = now()")
	args = append(args, userID)
	q := fmt.Sprintf(`UPDATE users SET %s WHERE id = $%d`, strings.Join(parts, ", "), n)
	_, err := tx.ExecContext(ctx, q, args...)
	return err
}

func (s *Store) mergeIdentityEmail(ctx context.Context, tx *sql.Tx, provider, providerUserID string, in *MockClaimsInput) error {
	pe := providerEmailFromMock(in)
	if pe == nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE auth_identities
		SET provider_email = $3, updated_at = now()
		WHERE provider = $1 AND provider_user_id = $2
	`, provider, providerUserID, *pe)
	return err
}

func providerEmailFromMock(in *MockClaimsInput) *string {
	if in.Email != nil && strings.TrimSpace(*in.Email) != "" {
		s := strings.TrimSpace(*in.Email)
		return &s
	}
	return nil
}
