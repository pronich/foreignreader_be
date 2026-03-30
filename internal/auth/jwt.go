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
type AccessClaims struct {
	Provider string `json:"provider"`
	jwt.RegisteredClaims
}

// IssueAccessToken creates a signed JWT with subject = internal user id and provider claim.
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
	uid, err := uuid.Parse(strings.TrimSpace(claims.Subject))
	if err != nil {
		return uuid.Nil, "", err
	}
	return uid, strings.TrimSpace(claims.Provider), nil
}
