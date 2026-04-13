package auth

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	revokeReasonRotated = "rotated"
	revokeReasonLogout  = "logout"
)

// InsertAuthSession stores a new session; raw refresh token is never persisted.
func (s *Store) InsertAuthSession(ctx context.Context, userID uuid.UUID, provider, refreshTokenHash string, refreshExpiresAt, now time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO auth_sessions (
			user_id, provider, refresh_token_hash, expires_at,
			last_used_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $5, $5)
		RETURNING id
	`, userID, provider, refreshTokenHash, refreshExpiresAt.UTC(), now.UTC()).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// DeleteAuthSession removes a session row (e.g. rollback after failed JWT issue).
func (s *Store) DeleteAuthSession(ctx context.Context, id uuid.UUID) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM auth_sessions WHERE id = $1`, id)
	return err
}

// RefreshRotationResult is returned after a successful refresh.
type RefreshRotationResult struct {
	AccessToken          string
	AccessTokenExpiresAt time.Time
	RefreshToken         string
}

// RotateRefreshToken validates the incoming refresh token, rotates it atomically, and issues a new access token.
func (s *Store) RotateRefreshToken(
	ctx context.Context,
	incomingRefresh string,
	refreshSessionTTL time.Duration,
	issuer *TokenIssuer,
) (*RefreshRotationResult, error) {
	if s.DB == nil {
		return nil, errors.New("database not configured")
	}
	incoming := incomingRefresh
	if incoming == "" {
		return nil, errRefreshInvalid
	}
	incomingHash := HashRefreshToken(incoming)

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
		SELECT id, user_id, provider, expires_at, revoked_at, replaced_by_session_id
		FROM auth_sessions
		WHERE refresh_token_hash = $1
		FOR UPDATE
	`, incomingHash)

	var (
		id, userID uuid.UUID
		provider   string
		expiresAt  time.Time
		revokedAt  sql.NullTime
		replacedBy sql.NullString
	)
	if err := row.Scan(&id, &userID, &provider, &expiresAt, &revokedAt, &replacedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errRefreshInvalid
		}
		return nil, err
	}

	now := time.Now().UTC()

	if revokedAt.Valid {
		if replacedBy.Valid && strings.TrimSpace(replacedBy.String) != "" {
			log.Printf("auth: session action=refresh_token_reuse_after_rotation old_session_id=%s user_id=%s", id.String(), userID.String())
			return nil, errRefreshReuse
		}
		log.Printf("auth: session action=refresh_rejected reason=revoked session_id=%s user_id=%s", id.String(), userID.String())
		return nil, errRefreshRevoked
	}

	if !expiresAt.After(now) {
		log.Printf("auth: session action=refresh_rejected reason=expired session_id=%s user_id=%s", id.String(), userID.String())
		return nil, errRefreshExpired
	}

	newRaw, newHash, err := GenerateRefreshToken()
	if err != nil {
		return nil, err
	}
	newExpires := now.Add(refreshSessionTTL)

	var newID uuid.UUID
	err = tx.QueryRowContext(ctx, `
		INSERT INTO auth_sessions (
			user_id, provider, refresh_token_hash, expires_at,
			last_used_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $5, $5)
		RETURNING id
	`, userID, provider, newHash, newExpires, now).Scan(&newID)
	if err != nil {
		return nil, err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE auth_sessions
		SET revoked_at = $2,
		    revoke_reason = $3,
		    replaced_by_session_id = $4,
		    last_used_at = $2,
		    updated_at = $2
		WHERE id = $1
	`, id, now, revokeReasonRotated, newID)
	if err != nil {
		return nil, err
	}

	access, accessExp, err := issuer.IssueAccessToken(userID, provider, newID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &RefreshRotationResult{
		AccessToken:          access,
		AccessTokenExpiresAt: accessExp,
		RefreshToken:         newRaw,
	}, nil
}

var (
	errRefreshInvalid = errors.New("invalid refresh token")
	errRefreshExpired = errors.New("refresh session expired")
	errRefreshRevoked = errors.New("refresh session revoked")
	errRefreshReuse   = errors.New("refresh token reused after rotation")
)

// RefreshErrorHTTP maps refresh errors to status and API codes.
func RefreshErrorHTTP(err error) (status int, code, msg string) {
	switch {
	case errors.Is(err, errRefreshInvalid):
		return http.StatusUnauthorized, "invalid_refresh_token", "invalid refresh token"
	case errors.Is(err, errRefreshExpired):
		return http.StatusUnauthorized, "refresh_session_expired", "refresh session has expired"
	case errors.Is(err, errRefreshRevoked):
		return http.StatusUnauthorized, "refresh_session_revoked", "refresh session is no longer valid"
	case errors.Is(err, errRefreshReuse):
		return http.StatusUnauthorized, "refresh_token_reused", "refresh token was already rotated; sign in again"
	default:
		return http.StatusInternalServerError, "internal_error", "could not refresh session"
	}
}

// RevokeSessionLogout sets revoked_at and revoke_reason=logout for the session when still active.
// Wrong user, missing row, or already revoked results in no update; the caller still treats it as success.
func (s *Store) RevokeSessionLogout(ctx context.Context, userID, sessionID uuid.UUID, now time.Time) error {
	if s.DB == nil {
		return errors.New("database not configured")
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE auth_sessions
		SET revoked_at = $3,
		    revoke_reason = $4,
		    updated_at = $3
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
	`, sessionID, userID, now.UTC(), revokeReasonLogout)
	return err
}
