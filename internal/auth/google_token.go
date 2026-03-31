package auth

import (
	"errors"
	"strings"

	"google.golang.org/api/idtoken"
)

// GoogleIDTokenClaims maps a verified Google ID token payload into our persistence shape.
// Only data from the validated token is used (no client-trusted fields).
func GoogleIDTokenClaims(p *idtoken.Payload) (*MockClaimsInput, error) {
	if p == nil {
		return nil, errors.New("nil payload")
	}
	sub := strings.TrimSpace(p.Subject)
	if sub == "" {
		return nil, errors.New("missing sub")
	}

	in := &MockClaimsInput{Sub: sub}

	if p.Claims == nil {
		return in, nil
	}

	if s := claimString(p.Claims, "email"); s != nil {
		in.Email = s
	}
	if b := claimBool(p.Claims, "email_verified"); b != nil {
		in.EmailVerified = b
	}
	if s := claimString(p.Claims, "name"); s != nil {
		in.DisplayName = s
	}
	if s := claimString(p.Claims, "picture"); s != nil {
		in.AvatarURL = s
	}

	return in, nil
}

func claimString(m map[string]interface{}, key string) *string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		return &s
	default:
		return nil
	}
}

func claimBool(m map[string]interface{}, key string) *bool {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case bool:
		return &t
	default:
		return nil
	}
}
