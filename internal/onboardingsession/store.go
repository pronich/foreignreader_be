package onboardingsession

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

const ScopeOnboardingTranslate = "onboarding_translate"

var (
	ErrInvalidToken      = errors.New("onboarding token invalid")
	ErrExpiredToken      = errors.New("onboarding token expired")
	ErrRevokedToken      = errors.New("onboarding token revoked")
	ErrInsufficientScope = errors.New("onboarding token scope insufficient")
)

// Store persists opaque onboarding access tokens (hash only).
type Store struct {
	DB *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{DB: db}
}

// HashToken returns the hex-encoded SHA-256 of the raw token string.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Issue inserts a new active token row and returns the raw opaque token (client-only) and expiry.
func (s *Store) Issue(ctx context.Context, appVersion, deviceSessionID, platform, createdIP string, ttl time.Duration) (rawToken string, expiresAt time.Time, err error) {
	if ttl <= 0 {
		return "", time.Time{}, fmt.Errorf("ttl must be positive")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	rawToken = hex.EncodeToString(raw)
	hash := HashToken(rawToken)
	expiresAt = time.Now().UTC().Add(ttl)

	var dev, plat, cip sql.NullString
	if strings.TrimSpace(deviceSessionID) != "" {
		dev = sql.NullString{String: strings.TrimSpace(deviceSessionID), Valid: true}
	}
	if strings.TrimSpace(platform) != "" {
		plat = sql.NullString{String: strings.TrimSpace(platform), Valid: true}
	}
	if strings.TrimSpace(createdIP) != "" {
		cip = sql.NullString{String: strings.TrimSpace(createdIP), Valid: true}
	}

	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO onboarding_access_tokens (
			token_hash, device_session_id, platform, app_version, scope, status,
			issued_at, expires_at, created_ip
		) VALUES ($1, $2, $3, $4, $5, 'active', now(), $6, $7)
	`, hash, dev, plat, strings.TrimSpace(appVersion), ScopeOnboardingTranslate, expiresAt, cip)
	if err != nil {
		return "", time.Time{}, err
	}
	return rawToken, expiresAt, nil
}

// ValidateAndTouch checks the raw token, updates last-used metadata, and returns the row id.
func (s *Store) ValidateAndTouch(ctx context.Context, rawToken, clientIP string) (uuid.UUID, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return uuid.Nil, ErrInvalidToken
	}
	hash := HashToken(rawToken)

	var id uuid.UUID
	var status, scope string
	var expiresAt time.Time
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, status, scope, expires_at
		FROM onboarding_access_tokens
		WHERE token_hash = $1
	`, hash).Scan(&id, &status, &scope, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, ErrInvalidToken
		}
		return uuid.Nil, err
	}

	now := time.Now()
	if status == "revoked" {
		return uuid.Nil, ErrRevokedToken
	}
	if status == "expired" || !expiresAt.After(now) {
		return uuid.Nil, ErrExpiredToken
	}
	if scope != ScopeOnboardingTranslate {
		return uuid.Nil, ErrInsufficientScope
	}

	cip := strings.TrimSpace(clientIP)
	_, err = s.DB.ExecContext(ctx, `
		UPDATE onboarding_access_tokens
		SET last_used_at = now(), last_used_ip = $2
		WHERE id = $1
	`, id, nullableString(cip))
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// CleanupOld deletes expired/revoked rows older than retention.
func (s *Store) CleanupOld(ctx context.Context, retention time.Duration) (int64, error) {
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	cutoff := time.Now().UTC().Add(-retention)
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM onboarding_access_tokens
		WHERE expires_at < $1
		   OR (revoked_at IS NOT NULL AND revoked_at < $1)
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RunPeriodicCleanup starts a goroutine that deletes old rows periodically (runs once immediately, then every interval).
func (s *Store) RunPeriodicCleanup(interval, retention time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		run := func() {
			n, err := s.CleanupOld(context.Background(), retention)
			if err != nil {
				log.Printf("onboarding_tokens: cleanup err=%v", err)
				return
			}
			if n > 0 {
				log.Printf("onboarding_tokens: cleanup deleted=%d", n)
			}
		}
		run()
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			run()
		}
	}()
}
