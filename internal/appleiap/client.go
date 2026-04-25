package appleiap

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
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Environment string

const (
	EnvProduction Environment = "production"
	EnvSandbox    Environment = "sandbox"
)

func (e Environment) BaseURL() string {
	if e == EnvSandbox {
		return "https://api.storekit-sandbox.itunes.apple.com"
	}
	return "https://api.storekit.itunes.apple.com"
}

type Client struct {
	Env      Environment
	IssuerID string
	KeyID    string
	BundleID string

	privateKey *ecdsa.PrivateKey
	httpClient *http.Client
}

func NewClient(env Environment, issuerID, keyID, bundleID, privateKeyPEM string) (*Client, error) {
	if strings.TrimSpace(issuerID) == "" || strings.TrimSpace(keyID) == "" || strings.TrimSpace(bundleID) == "" {
		return nil, errors.New("missing apple iap credentials")
	}
	pk, err := parseApplePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	if env != EnvSandbox && env != EnvProduction {
		return nil, fmt.Errorf("invalid apple iap environment %q", env)
	}
	return &Client{
		Env:        env,
		IssuerID:   issuerID,
		KeyID:      keyID,
		BundleID:   bundleID,
		privateKey: pk,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func parseApplePrivateKey(pemText string) (*ecdsa.PrivateKey, error) {
	s := strings.TrimSpace(pemText)
	if s == "" {
		return nil, errors.New("missing apple iap private key")
	}
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, errors.New("invalid apple iap private key pem")
	}
	// App Store Connect .p8 keys are PKCS8.
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse apple iap private key: %w", err)
	}
	pk, ok := keyAny.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("apple iap private key is not ECDSA")
	}
	return pk, nil
}

func (c *Client) authToken(now time.Time) (string, error) {
	iat := now.UTC()
	exp := iat.Add(20 * time.Minute) // keep short-lived
	claims := jwt.MapClaims{
		"iss": c.IssuerID,
		"iat": iat.Unix(),
		"exp": exp.Unix(),
		"aud": "appstoreconnect-v1",
		"bid": c.BundleID,
	}
	t := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	t.Header["kid"] = c.KeyID
	t.Header["typ"] = "JWT"
	return t.SignedString(c.privateKey)
}

type TransactionInfoResponse struct {
	SignedTransactionInfo string `json:"signedTransactionInfo"`
}

// GetTransactionInfo calls Apple App Store Server API:
// GET /inApps/v1/transactions/{transactionId}
func (c *Client) GetTransactionInfo(ctx context.Context, transactionID string) (*TransactionInfoResponse, error) {
	tid := strings.TrimSpace(transactionID)
	if tid == "" {
		return nil, errors.New("missing transactionId")
	}
	token, err := c.authToken(time.Now())
	if err != nil {
		return nil, err
	}

	url := c.Env.BaseURL() + "/inApps/v1/transactions/" + tid
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// Do not return raw body (may contain details). Caller should map to API error.
		return nil, fmt.Errorf("apple server api status=%d", resp.StatusCode)
	}

	var out TransactionInfoResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode apple response: %w", err)
	}
	if strings.TrimSpace(out.SignedTransactionInfo) == "" {
		return nil, errors.New("apple response missing signedTransactionInfo")
	}
	return &out, nil
}
