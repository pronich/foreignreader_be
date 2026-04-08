package auth

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	appleIssuer  = "https://appleid.apple.com"
	appleJWKSURL = "https://appleid.apple.com/auth/keys"
)

var (
	ErrAppleTokenInvalid = errors.New("invalid apple identity token")
)

type AppleVerifier struct {
	Audiences []string
	JWKSCache *appleJWKSCache
}

func NewAppleVerifier(audienceCSV string, ttl time.Duration) (*AppleVerifier, error) {
	auds := splitCSVNonEmpty(audienceCSV)
	if len(auds) == 0 {
		return nil, errors.New("missing apple audiences")
	}
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	return &AppleVerifier{
		Audiences: auds,
		JWKSCache: &appleJWKSCache{
			ttl: ttl,
			c: &http.Client{
				Timeout: 8 * time.Second,
			},
		},
	}, nil
}

// VerifyIdentityToken verifies Apple identity token signature and core claims.
// If nonce is provided, verifies it against the token's nonce claim (supports raw nonce or pre-hashed nonce).
func (v *AppleVerifier) VerifyIdentityToken(ctx context.Context, identityToken string, nonce *string) (*MockClaimsInput, error) {
	tok := strings.TrimSpace(identityToken)
	if tok == "" {
		return nil, fmt.Errorf("%w: missing token", ErrAppleTokenInvalid)
	}

	claims := &appleIDTokenClaims{}

	// First pass: use cached keys.
	in, err := v.verifyWithCache(ctx, tok, claims, nonce, false)
	if err == nil {
		return in, nil
	}

	// If verification fails, refresh keys and retry once (to handle rotation).
	in2, err2 := v.verifyWithCache(ctx, tok, claims, nonce, true)
	if err2 == nil {
		return in2, nil
	}
	return nil, err2
}

func (v *AppleVerifier) verifyWithCache(ctx context.Context, identityToken string, claims *appleIDTokenClaims, nonce *string, forceRefresh bool) (*MockClaimsInput, error) {
	keyFunc := func(token *jwt.Token) (any, error) {
		alg, _ := token.Header["alg"].(string)
		if alg != "RS256" {
			return nil, fmt.Errorf("%w: unsupported alg %q", ErrAppleTokenInvalid, alg)
		}
		kid, _ := token.Header["kid"].(string)
		if strings.TrimSpace(kid) == "" {
			return nil, fmt.Errorf("%w: missing kid", ErrAppleTokenInvalid)
		}
		keys, err := v.JWKSCache.keys(ctx, forceRefresh)
		if err != nil {
			return nil, err
		}
		pub := keys[kid]
		if pub == nil {
			if !forceRefresh {
				// Trigger retry path by returning a recognizable error.
				return nil, fmt.Errorf("%w: unknown kid", ErrAppleTokenInvalid)
			}
			return nil, fmt.Errorf("%w: unknown kid", ErrAppleTokenInvalid)
		}
		return pub, nil
	}

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(appleIssuer),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(30*time.Second),
	)

	parsed, err := parser.ParseWithClaims(identityToken, claims, keyFunc)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAppleTokenInvalid, err)
	}
	if !parsed.Valid {
		return nil, fmt.Errorf("%w: invalid token", ErrAppleTokenInvalid)
	}

	if !audienceMatches(claims.Audience, v.Audiences) {
		return nil, fmt.Errorf("%w: audience mismatch", ErrAppleTokenInvalid)
	}

	sub := strings.TrimSpace(claims.Subject)
	if sub == "" {
		return nil, fmt.Errorf("%w: missing sub", ErrAppleTokenInvalid)
	}

	if nonce != nil && strings.TrimSpace(*nonce) != "" {
		if err := verifyAppleNonce(*nonce, claims.Nonce); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrAppleTokenInvalid, err)
		}
	}

	in := &MockClaimsInput{Sub: sub}

	if s := strings.TrimSpace(claims.Email); s != "" {
		in.Email = &s
	}
	if b, ok := claims.EmailVerifiedBool(); ok {
		in.EmailVerified = &b
	}

	return in, nil
}

type appleIDTokenClaims struct {
	jwt.RegisteredClaims

	Email         string `json:"email,omitempty"`
	EmailVerified any    `json:"email_verified,omitempty"` // can be bool or string
	Nonce         string `json:"nonce,omitempty"`
}

func (c *appleIDTokenClaims) EmailVerifiedBool() (bool, bool) {
	switch t := c.EmailVerified.(type) {
	case bool:
		return t, true
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		if s == "true" {
			return true, true
		}
		if s == "false" {
			return false, true
		}
		return false, false
	default:
		return false, false
	}
}

func verifyAppleNonce(rawOrHashedNonce string, tokenNonce string) error {
	expected := strings.TrimSpace(tokenNonce)
	if expected == "" {
		return errors.New("missing nonce in token")
	}
	got := strings.TrimSpace(rawOrHashedNonce)
	if got == "" {
		return errors.New("missing nonce")
	}
	if got == expected {
		return nil
	}
	h := sha256.Sum256([]byte(got))
	// Apple commonly sets the `nonce` claim to the SHA-256 hash of the raw nonce as a hex string
	// (because apps often set request.nonce to a SHA-256 hex string). We accept both hex and base64url
	// to be tolerant to client implementations.
	hashedHexLower := fmt.Sprintf("%x", h[:])
	if hashedHexLower == expected || strings.EqualFold(hashedHexLower, expected) {
		return nil
	}
	hashedB64URL := base64.RawURLEncoding.EncodeToString(h[:])
	if hashedB64URL == expected {
		return nil
	}
	return errors.New("nonce mismatch")
}

type appleJWKSCache struct {
	mu        sync.Mutex
	expiresAt time.Time
	keysByKID map[string]*rsa.PublicKey
	ttl       time.Duration
	c         *http.Client
}

func (c *appleJWKSCache) keys(ctx context.Context, forceRefresh bool) (map[string]*rsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if !forceRefresh && c.keysByKID != nil && now.Before(c.expiresAt) {
		return c.keysByKID, nil
	}
	if c.c == nil {
		c.c = &http.Client{Timeout: 8 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appleJWKSURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: jwks fetch failed status=%d", ErrAppleTokenInvalid, resp.StatusCode)
	}

	var jwks appleJWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, err
	}
	out := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if strings.TrimSpace(k.KID) == "" || k.Kty != "RSA" {
			continue
		}
		pub, err := k.rsaPublicKey()
		if err != nil {
			continue
		}
		out[k.KID] = pub
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: jwks contained no usable keys", ErrAppleTokenInvalid)
	}
	c.keysByKID = out
	c.expiresAt = now.Add(c.ttl)
	return c.keysByKID, nil
}

type appleJWKS struct {
	Keys []appleJWK `json:"keys"`
}

type appleJWK struct {
	Kty string `json:"kty"`
	KID string `json:"kid"`
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (k appleJWK) rsaPublicKey() (*rsa.PublicKey, error) {
	if k.Kty != "RSA" {
		return nil, errors.New("non-rsa jwk")
	}
	nb, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	eb, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, err
	}
	e := 0
	for _, b := range eb {
		e = e<<8 + int(b)
	}
	if e == 0 {
		return nil, errors.New("invalid exponent")
	}
	pub := &rsa.PublicKey{
		N: new(big.Int).SetBytes(nb),
		E: e,
	}
	return pub, nil
}

// splitCSVNonEmpty splits a comma-separated string into trimmed, non-empty items.
func splitCSVNonEmpty(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		t := strings.TrimSpace(part)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func audienceMatches(tokenAud jwt.ClaimStrings, allowed []string) bool {
	if len(tokenAud) == 0 || len(allowed) == 0 {
		return false
	}
	for _, ta := range tokenAud {
		ta = strings.TrimSpace(ta)
		if ta == "" {
			continue
		}
		for _, a := range allowed {
			if ta == a {
				return true
			}
		}
	}
	return false
}

