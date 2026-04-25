package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenIssuer signs and verifies HS256 access tokens.
type TokenIssuer struct {
	secret         []byte
	accessTokenTTL time.Duration
}

// NewTokenIssuer creates an issuer. accessTokenTTL applies to both app and web access tokens.
func NewTokenIssuer(secret string, accessTokenTTL time.Duration) (*TokenIssuer, error) {
	s := strings.TrimSpace(secret)
	if s == "" {
		return nil, errors.New("jwt secret is empty")
	}
	if accessTokenTTL <= 0 {
		return nil, errors.New("access token TTL must be positive")
	}
	return &TokenIssuer{secret: []byte(s), accessTokenTTL: accessTokenTTL}, nil
}

// AccessClaims is the payload for backend-issued access tokens.
// Mobile app tokens omit token_type and channel (legacy shape). Web cabinet tokens set token_type=web and channel=web.
// Sid is the auth_sessions row id for refresh-based app sessions; omitted for web-only tokens.
type AccessClaims struct {
	Provider  string `json:"provider"`
	TokenType string `json:"token_type,omitempty"`
	Channel   string `json:"channel,omitempty"`
	Sid       string `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

const (
	TokenTypeApp = "app"
	TokenTypeWeb = "web"
)

// IssueAccessToken creates a signed JWT with subject = internal user id and provider claim (mobile app).
// sessionID is the auth_sessions id; use uuid.Nil for tokens without a refresh session.
func (t *TokenIssuer) IssueAccessToken(userID uuid.UUID, provider string, sessionID uuid.UUID) (token string, expiresAt time.Time, err error) {
	now := time.Now()
	expiresAt = now.Add(t.accessTokenTTL).UTC()
	claims := AccessClaims{
		Provider: provider,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	if sessionID != uuid.Nil {
		claims.Sid = sessionID.String()
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return s, expiresAt, nil
}

// IssueWebAccessToken issues a JWT for the web cabinet, distinct from app tokens via claims.
func (t *TokenIssuer) IssueWebAccessToken(userID uuid.UUID, provider string) (token string, expiresAt time.Time, err error) {
	now := time.Now()
	expiresAt = now.Add(t.accessTokenTTL).UTC()
	claims := AccessClaims{
		Provider:  provider,
		TokenType: TokenTypeWeb,
		Channel:   TokenTypeWeb,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return s, expiresAt, nil
}

// ParseAccessToken validates the token and returns internal user id, provider, and optional auth_sessions id (sid claim).
func (t *TokenIssuer) ParseAccessToken(tokenString string) (userID uuid.UUID, provider string, sessionID uuid.UUID, err error) {
	tok, err := jwt.ParseWithClaims(tokenString, &AccessClaims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return t.secret, nil
	})
	if err != nil || !tok.Valid {
		return uuid.Nil, "", uuid.Nil, err
	}
	claims, ok := tok.Claims.(*AccessClaims)
	if !ok {
		return uuid.Nil, "", uuid.Nil, errors.New("invalid claims type")
	}
	if strings.TrimSpace(claims.Subject) == "" || strings.TrimSpace(claims.Provider) == "" {
		return uuid.Nil, "", uuid.Nil, errors.New("missing subject or provider")
	}
	tt := strings.TrimSpace(claims.TokenType)
	switch tt {
	case "", TokenTypeApp:
		// Legacy app tokens have no token_type; treat empty as app.
	case TokenTypeWeb:
		if strings.TrimSpace(claims.Channel) != TokenTypeWeb {
			return uuid.Nil, "", uuid.Nil, errors.New("invalid web token channel")
		}
	default:
		return uuid.Nil, "", uuid.Nil, fmt.Errorf("unsupported token_type: %s", tt)
	}
	uid, err := uuid.Parse(strings.TrimSpace(claims.Subject))
	if err != nil {
		return uuid.Nil, "", uuid.Nil, err
	}
	var sid uuid.UUID
	if s := strings.TrimSpace(claims.Sid); s != "" {
		sid, err = uuid.Parse(s)
		if err != nil {
			return uuid.Nil, "", uuid.Nil, errors.New("invalid sid claim")
		}
	}
	return uid, strings.TrimSpace(claims.Provider), sid, nil
}
