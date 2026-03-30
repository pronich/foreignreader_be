package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/config"
)

func registerAuthRoutes(mux *http.ServeMux, cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer) {
	mux.HandleFunc("POST /api/v1/auth/apple", func(w http.ResponseWriter, r *http.Request) {
		handleAuthApple(w, r, cfg, store, issuer)
	})
	mux.HandleFunc("POST /api/v1/auth/google", func(w http.ResponseWriter, r *http.Request) {
		handleAuthGoogle(w, r, cfg, store, issuer)
	})
	mux.Handle("GET /api/v1/me", bearerAuthHandler(store, issuer, handleAuthMe))
}

type mockClaimsBody struct {
	Sub             string  `json:"sub"`
	Email           *string `json:"email,omitempty"`
	EmailVerified   *bool   `json:"emailVerified,omitempty"`
	DisplayName     *string `json:"displayName,omitempty"`
	AvatarURL       *string `json:"avatarUrl,omitempty"`
}

type appleAuthRequest struct {
	IdentityToken string          `json:"identityToken"`
	MockClaims    *mockClaimsBody `json:"mockClaims,omitempty"`
}

type googleAuthRequest struct {
	IDToken    string          `json:"idToken"`
	MockClaims *mockClaimsBody `json:"mockClaims,omitempty"`
}

type userPublic struct {
	ID            string  `json:"id"`
	DisplayName   *string `json:"displayName"`
	AvatarURL     *string `json:"avatarUrl"`
	Email         *string `json:"email"`
	EmailVerified bool    `json:"emailVerified"`
}

type authLoginResponse struct {
	AccessToken string     `json:"accessToken"`
	User        userPublic `json:"user"`
}

type meResponse struct {
	User userPublic `json:"user"`
}

func handleAuthApple(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer) {
	handleMockableAuth(w, r, cfg, store, issuer, "apple", "identityToken", func(body []byte) (string, *mockClaimsBody, error) {
		var req appleAuthRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return "", nil, err
		}
		return req.IdentityToken, req.MockClaims, nil
	})
}

func handleAuthGoogle(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer) {
	handleMockableAuth(w, r, cfg, store, issuer, "google", "idToken", func(body []byte) (string, *mockClaimsBody, error) {
		var req googleAuthRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return "", nil, err
		}
		return req.IDToken, req.MockClaims, nil
	})
}

func handleMockableAuth(
	w http.ResponseWriter,
	r *http.Request,
	cfg config.Config,
	store *auth.Store,
	issuer *auth.TokenIssuer,
	provider string,
	tokenField string,
	parse func([]byte) (token string, mock *mockClaimsBody, err error),
) {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeAPIError(w, http.StatusUnsupportedMediaType, "invalid_request", "expected Content-Type: application/json")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "could not read body")
		return
	}

	token, mock, err := parse(body)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if strings.TrimSpace(token) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", tokenField+" is required")
		return
	}

	mockAllowed := cfg.MockAuthAllowed()
	if mock != nil && !mockAllowed {
		writeAPIError(w, http.StatusForbidden, "mock_auth_disabled", "mock authentication is not enabled for this environment")
		return
	}

	if !mockAllowed {
		writeAPIError(w, http.StatusNotImplemented, "not_implemented", provider+" sign-in is not implemented yet")
		return
	}

	if mock == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "mockClaims is required when mock authentication is enabled")
		return
	}

	if strings.TrimSpace(mock.Sub) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "mockClaims.sub is required")
		return
	}

	in := &auth.MockClaimsInput{
		Sub:             strings.TrimSpace(mock.Sub),
		Email:           mock.Email,
		EmailVerified:   mock.EmailVerified,
		DisplayName:     mock.DisplayName,
		AvatarURL:       mock.AvatarURL,
	}

	user, err := store.LoginOrRegisterMock(r.Context(), provider, in)
	if err != nil {
		log.Printf("auth: mock login: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "auth_failed", "authentication processing failed")
		return
	}

	access, err := issuer.IssueAccessToken(user.ID, provider)
	if err != nil {
		log.Printf("auth: issue token: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "auth_failed", "could not issue access token")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(authLoginResponse{
		AccessToken: access,
		User:        userPublicFromAuth(user),
	})
}

func handleAuthMe(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "missing authentication context")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(meResponse{User: userPublicFromAuth(u)})
}

func userPublicFromAuth(u auth.User) userPublic {
	out := userPublic{
		ID:            u.ID.String(),
		EmailVerified: u.EmailVerified,
	}
	if u.DisplayName.Valid {
		s := u.DisplayName.String
		out.DisplayName = &s
	}
	if u.AvatarURL.Valid {
		s := u.AvatarURL.String
		out.AvatarURL = &s
	}
	if u.Email.Valid {
		s := u.Email.String
		out.Email = &s
	}
	return out
}
