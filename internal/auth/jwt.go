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
	secret []byte
}

func NewTokenIssuer(secret string) (*TokenIssuer, error) {
	s := strings.TrimSpace(secret)
	if s == "" {
		return nil, errors.New("jwt secret is empty")
	}
	return &TokenIssuer{secret: []byte(s)}, nil
}

// AccessClaims is the payload for backend-issued access tokens.
// Mobile app tokens omit token_type and channel (legacy shape). Web cabinet tokens set token_type=web and channel=web.
type AccessClaims struct {
	Provider   string `json:"provider"`
	TokenType  string `json:"token_type,omitempty"`
	Channel    string `json:"channel,omitempty"`
	jwt.RegisteredClaims
}

const (
	TokenTypeApp = "app"
	TokenTypeWeb = "web"
)

// IssueAccessToken creates a signed JWT with subject = internal user id and provider claim (mobile app; 24h).
func (t *TokenIssuer) IssueAccessToken(userID uuid.UUID, provider string) (string, error) {
	now := time.Now()
	claims := AccessClaims{
		Provider: provider,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(t.secret)
}

// IssueWebAccessToken issues a short-lived JWT for the web cabinet (8h), distinct from app tokens via claims.
func (t *TokenIssuer) IssueWebAccessToken(userID uuid.UUID, provider string) (string, error) {
	now := time.Now()
	claims := AccessClaims{
		Provider:  provider,
		TokenType: TokenTypeWeb,
		Channel:   TokenTypeWeb,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(8 * time.Hour)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(t.secret)
}

// ParseAccessToken validates the token and returns internal user id and provider.
func (t *TokenIssuer) ParseAccessToken(tokenString string) (userID uuid.UUID, provider string, err error) {
	tok, err := jwt.ParseWithClaims(tokenString, &AccessClaims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return t.secret, nil
	})
	if err != nil || !tok.Valid {
		return uuid.Nil, "", err
	}
	claims, ok := tok.Claims.(*AccessClaims)
	if !ok {
		return uuid.Nil, "", errors.New("invalid claims type")
	}
	if strings.TrimSpace(claims.Subject) == "" || strings.TrimSpace(claims.Provider) == "" {
		return uuid.Nil, "", errors.New("missing subject or provider")
	}
	tt := strings.TrimSpace(claims.TokenType)
	switch tt {
	case "", TokenTypeApp:
		// Legacy app tokens have no token_type; treat empty as app.
	case TokenTypeWeb:
		if strings.TrimSpace(claims.Channel) != TokenTypeWeb {
			return uuid.Nil, "", errors.New("invalid web token channel")
		}
	default:
		return uuid.Nil, "", fmt.Errorf("unsupported token_type: %s", tt)
	}
	uid, err := uuid.Parse(strings.TrimSpace(claims.Subject))
	if err != nil {
		return uuid.Nil, "", err
	}
	return uid, strings.TrimSpace(claims.Provider), nil
}
