package appleiap

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidSignedPayload = errors.New("invalid signed payload")
)

// VerifyJWS verifies an Apple-signed JWS using the x5c certificate chain embedded in the header.
// It returns the verified payload bytes (decoded JSON).
func VerifyJWS(jwsCompact string) ([]byte, error) {
	parts := strings.Split(strings.TrimSpace(jwsCompact), ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: invalid jws format", ErrInvalidSignedPayload)
	}

	claims := jwt.MapClaims{}
	parsed, err := jwt.NewParser(
		jwt.WithValidMethods([]string{"ES256"}),
		jwt.WithLeeway(30*time.Second),
	).ParseWithClaims(jwsCompact, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodES256 {
			return nil, fmt.Errorf("%w: unexpected alg", ErrInvalidSignedPayload)
		}
		leaf, intermediates, err := certChainFromHeader(token.Header)
		if err != nil {
			return nil, err
		}
		if err := verifyAppleCertChain(leaf, intermediates); err != nil {
			return nil, err
		}
		pk, ok := leaf.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("%w: unexpected public key type", ErrInvalidSignedPayload)
		}
		return pk, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSignedPayload, err)
	}
	if parsed == nil || !parsed.Valid {
		return nil, fmt.Errorf("%w: signature invalid", ErrInvalidSignedPayload)
	}

	// Decode payload part directly to bytes (claims map loses exact types/precision).
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid payload encoding", ErrInvalidSignedPayload)
	}
	if !json.Valid(payloadBytes) {
		return nil, fmt.Errorf("%w: invalid payload json", ErrInvalidSignedPayload)
	}
	return payloadBytes, nil
}

func certChainFromHeader(h map[string]any) (leaf *x509.Certificate, intermediates []*x509.Certificate, err error) {
	v, ok := h["x5c"]
	if !ok || v == nil {
		return nil, nil, fmt.Errorf("%w: missing x5c", ErrInvalidSignedPayload)
	}
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return nil, nil, fmt.Errorf("%w: invalid x5c", ErrInvalidSignedPayload)
	}
	var certs []*x509.Certificate
	for _, it := range arr {
		s, ok := it.(string)
		if !ok {
			continue
		}
		der, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			continue
		}
		c, err := x509.ParseCertificate(der)
		if err != nil {
			continue
		}
		certs = append(certs, c)
	}
	if len(certs) == 0 {
		return nil, nil, fmt.Errorf("%w: no certs parsed", ErrInvalidSignedPayload)
	}
	leaf = certs[0]
	if len(certs) > 1 {
		intermediates = certs[1:]
	}
	return leaf, intermediates, nil
}

func verifyAppleCertChain(leaf *x509.Certificate, intermediates []*x509.Certificate) error {
	if leaf == nil {
		return fmt.Errorf("%w: missing leaf cert", ErrInvalidSignedPayload)
	}
	roots, err := appleTrustRoots()
	if err != nil {
		return err
	}
	interPool := x509.NewCertPool()
	for _, ic := range intermediates {
		interPool.AddCert(ic)
	}
	_, err = leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: interPool,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		return fmt.Errorf("%w: cert chain verify failed: %v", ErrInvalidSignedPayload, err)
	}
	return nil
}

// appleTrustRoots merges the OS trust store with Apple's official root CAs.
// Relying on SystemCertPool alone often fails in minimal Linux images (e.g. Alpine)
// where Apple Root CA - G2/G3 are not present, which breaks x5c chain verification.
func appleTrustRoots() (*x509.CertPool, error) {
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	for _, pemBlock := range [][]byte{[]byte(appleRootCAG2PEM), []byte(appleRootCAG3PEM)} {
		if ok := roots.AppendCertsFromPEM(pemBlock); !ok {
			return nil, fmt.Errorf("%w: could not load embedded Apple root CA", ErrInvalidSignedPayload)
		}
	}
	return roots, nil
}
