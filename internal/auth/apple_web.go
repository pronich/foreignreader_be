package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const appleTokenEndpoint = "https://appleid.apple.com/auth/token"

// AppleWebTokenResponse is the JSON body from Apple's token endpoint.
type AppleWebTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
}

// GenerateAppleWebClientSecret builds the ES256 JWT Apple requires as client_secret
// for Sign in with Apple (web / Services ID). See:
// https://developer.apple.com/documentation/sign_in_with_apple/generate_and_validate_tokens
func GenerateAppleWebClientSecret(teamID, servicesID, keyID string, privateKeyPEM string) (string, error) {
	teamID = strings.TrimSpace(teamID)
	servicesID = strings.TrimSpace(servicesID)
	keyID = strings.TrimSpace(keyID)
	if teamID == "" || servicesID == "" || keyID == "" {
		return "", errors.New("missing team id, client id, or key id")
	}
	pk, err := parseAppleECPrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"iss": teamID,
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
		"aud": "https://appleid.apple.com",
		"sub": servicesID,
	}
	t := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	t.Header["kid"] = keyID
	t.Header["alg"] = "ES256"
	return t.SignedString(pk)
}

func parseAppleECPrivateKeyFromPEM(pemText string) (*ecdsa.PrivateKey, error) {
	s := strings.TrimSpace(pemText)
	if s == "" {
		return nil, errors.New("empty private key")
	}
	s = strings.ReplaceAll(s, `\n`, "\n")
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, errors.New("invalid apple sign-in private key pem")
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	pk, ok := keyAny.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("apple sign-in private key is not ECDSA")
	}
	return pk, nil
}

// ExchangeAppleAuthorizationCode calls Apple's token endpoint (authorization_code grant).
func ExchangeAppleAuthorizationCode(ctx context.Context, clientID, clientSecret, redirectURI, code string) (*AppleWebTokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(clientID))
	form.Set("client_secret", strings.TrimSpace(clientSecret))
	form.Set("code", strings.TrimSpace(code))
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", strings.TrimSpace(redirectURI))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, appleTokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apple token endpoint status=%d", resp.StatusCode)
	}
	var out AppleWebTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if strings.TrimSpace(out.IDToken) == "" {
		return nil, errors.New("apple token response missing id_token")
	}
	return &out, nil
}

// AppleWebUserParam is the optional `user` query value on first Sign in with Apple (JSON).
type AppleWebUserParam struct {
	Name *struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	} `json:"name"`
	Email string `json:"email"`
}

// ParseAppleWebUserQuery parses the optional `user` parameter from the OAuth redirect (first sign-in only).
func ParseAppleWebUserQuery(userQuery string) (*AppleWebUserParam, error) {
	s := strings.TrimSpace(userQuery)
	if s == "" {
		return nil, nil
	}
	// Apple may send JSON as a single query param; sometimes URL-encoded.
	decoded, err := url.QueryUnescape(s)
	if err != nil {
		decoded = s
	}
	var u AppleWebUserParam
	if err := json.Unmarshal([]byte(decoded), &u); err != nil {
		return nil, err
	}
	return &u, nil
}
