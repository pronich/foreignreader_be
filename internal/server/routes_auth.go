package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/config"
	"google.golang.org/api/idtoken"
)

func registerAuthRoutes(mux *http.ServeMux, cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer) {
	mux.HandleFunc("POST /api/v1/auth/apple", func(w http.ResponseWriter, r *http.Request) {
		handleAuthApple(w, r, cfg, store, issuer)
	})
	mux.HandleFunc("POST /api/v1/auth/google", func(w http.ResponseWriter, r *http.Request) {
		handleAuthGoogle(w, r, cfg, store, issuer)
	})
	mux.Handle("GET /api/v1/me", bearerAuthHandler(store, issuer, handleAuthMe))

	registerAppleWebAuthRoutes(mux, cfg, store, issuer)
}

type mockClaimsBody struct {
	Sub           string  `json:"sub"`
	Email         *string `json:"email,omitempty"`
	EmailVerified *bool   `json:"emailVerified,omitempty"`
	DisplayName   *string `json:"displayName,omitempty"`
	AvatarURL     *string `json:"avatarUrl,omitempty"`
}

type appleAuthRequest struct {
	Source        string          `json:"source"`
	IdentityToken string          `json:"identityToken"`
	Nonce         *string         `json:"nonce,omitempty"`
	Email         *string         `json:"email,omitempty"`
	FullName      *string         `json:"fullName,omitempty"`
	MockClaims    *mockClaimsBody `json:"mockClaims,omitempty"`
}

type googleAuthRequest struct {
	Source     string          `json:"source"`
	IDToken    string          `json:"idToken"`
	MockClaims *mockClaimsBody `json:"mockClaims,omitempty"`
}

type userPublic struct {
	ID                      string     `json:"id"`
	DisplayName             *string    `json:"displayName"`
	AvatarURL               *string    `json:"avatarUrl"`
	Email                   *string    `json:"email"`
	EmailVerified           bool       `json:"emailVerified"`
	AppStorefront           *string    `json:"appStorefront"`
	AppStorefrontUpdatedAt  *time.Time `json:"appStorefrontUpdatedAt"`
}

type authLoginResponse struct {
	AccessToken string     `json:"accessToken"`
	User        userPublic `json:"user"`
	TokenType   string     `json:"tokenType,omitempty"`
	ExpiresIn   int64      `json:"expiresIn,omitempty"`
}

type meResponse struct {
	User userPublic `json:"user"`
}

type authRequestSource string

const (
	authRequestSourceApp authRequestSource = "app"
	authRequestSourceWeb authRequestSource = "web"
)

func parseAuthRequestSourceField(s string) (authRequestSource, error) {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "app":
		return authRequestSourceApp, nil
	case "web":
		return authRequestSourceWeb, nil
	case "":
		return "", errAuthSourceMissing
	default:
		return "", errAuthSourceInvalid
	}
}

var (
	errAuthSourceMissing = errors.New("missing source")
	errAuthSourceInvalid = errors.New("invalid source")
)

func writeAuthSourceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errAuthSourceMissing):
		writeAPIError(w, http.StatusBadRequest, "missing_source", "source is required")
	case errors.Is(err, errAuthSourceInvalid):
		writeAPIError(w, http.StatusBadRequest, "invalid_source", "source must be app or web")
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid source")
	}
}

func handleAuthApple(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer) {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		log.Printf("auth: request_id=%s provider=apple reason=unsupported_media_type content_type=%q",
			requestIDFromContext(r.Context()), ct)
		writeAPIError(w, http.StatusUnsupportedMediaType, "invalid_request", "expected Content-Type: application/json")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		log.Printf("auth: request_id=%s provider=apple reason=body_read_failed err=%v",
			requestIDFromContext(r.Context()), err)
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "could not read body")
		return
	}

	var req appleAuthRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("auth: request_id=%s provider=apple reason=json_unmarshal_failed err=%v body_len=%d",
			requestIDFromContext(r.Context()), err, len(body))
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	source, err := parseAuthRequestSourceField(req.Source)
	if err != nil {
		log.Printf("auth: request_id=%s provider=apple reason=source_invalid err=%v", requestIDFromContext(r.Context()), err)
		writeAuthSourceError(w, err)
		return
	}

	if strings.TrimSpace(req.IdentityToken) == "" {
		log.Printf("auth: request_id=%s provider=apple source=%s reason=missing_identity_token", requestIDFromContext(r.Context()), source)
		writeAPIError(w, http.StatusBadRequest, "missing_identity_token", "identityToken is required")
		return
	}

	mockAllowed := cfg.MockAuthAllowed()
	if req.MockClaims != nil && !mockAllowed {
		log.Printf("auth: request_id=%s provider=apple source=%s reason=mock_present_but_disabled app_env=%q auth_dev_mode=%t",
			requestIDFromContext(r.Context()), source, cfg.AppEnv, cfg.AuthDevMode)
		writeAPIError(w, http.StatusForbidden, "mock_auth_disabled", "mock authentication is not enabled for this environment")
		return
	}

	if mockAllowed && req.MockClaims != nil {
		if strings.TrimSpace(req.MockClaims.Sub) == "" {
			log.Printf("auth: request_id=%s provider=apple source=%s reason=missing_mock_sub", requestIDFromContext(r.Context()), source)
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "mockClaims.sub is required")
			return
		}
		in := &auth.MockClaimsInput{
			Sub:           strings.TrimSpace(req.MockClaims.Sub),
			Email:         req.MockClaims.Email,
			EmailVerified: req.MockClaims.EmailVerified,
			DisplayName:   req.MockClaims.DisplayName,
			AvatarURL:     req.MockClaims.AvatarURL,
		}
		completeAuthLogin(w, r, store, issuer, "apple", in, source)
		return
	}

	handleAppleOIDC(
		w,
		r,
		cfg,
		store,
		issuer,
		strings.TrimSpace(req.IdentityToken),
		req.Nonce,
		req.Email,
		req.FullName,
		source,
	)
}

func handleAuthGoogle(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer) {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		log.Printf("auth: request_id=%s provider=google reason=unsupported_media_type content_type=%q",
			requestIDFromContext(r.Context()), ct)
		writeAPIError(w, http.StatusUnsupportedMediaType, "invalid_request", "expected Content-Type: application/json")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		log.Printf("auth: request_id=%s provider=google reason=body_read_failed err=%v",
			requestIDFromContext(r.Context()), err)
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "could not read body")
		return
	}

	var req googleAuthRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("auth: request_id=%s provider=google reason=json_unmarshal_failed err=%v body_len=%d",
			requestIDFromContext(r.Context()), err, len(body))
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	source, err := parseAuthRequestSourceField(req.Source)
	if err != nil {
		log.Printf("auth: request_id=%s provider=google reason=source_invalid err=%v", requestIDFromContext(r.Context()), err)
		writeAuthSourceError(w, err)
		return
	}

	if strings.TrimSpace(req.IDToken) == "" {
		log.Printf("auth: request_id=%s provider=google source=%s reason=missing_id_token", requestIDFromContext(r.Context()), source)
		writeAPIError(w, http.StatusBadRequest, "missing_id_token", "idToken is required")
		return
	}

	mockAllowed := cfg.MockAuthAllowed()
	if req.MockClaims != nil && !mockAllowed {
		log.Printf("auth: request_id=%s provider=google source=%s reason=mock_present_but_disabled app_env=%q auth_dev_mode=%t",
			requestIDFromContext(r.Context()), source, cfg.AppEnv, cfg.AuthDevMode)
		writeAPIError(w, http.StatusForbidden, "mock_auth_disabled", "mock authentication is not enabled for this environment")
		return
	}

	if mockAllowed && req.MockClaims != nil {
		if strings.TrimSpace(req.MockClaims.Sub) == "" {
			log.Printf("auth: request_id=%s provider=google source=%s reason=missing_mock_sub", requestIDFromContext(r.Context()), source)
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "mockClaims.sub is required")
			return
		}
		in := &auth.MockClaimsInput{
			Sub:           strings.TrimSpace(req.MockClaims.Sub),
			Email:         req.MockClaims.Email,
			EmailVerified: req.MockClaims.EmailVerified,
			DisplayName:   req.MockClaims.DisplayName,
			AvatarURL:     req.MockClaims.AvatarURL,
		}
		completeAuthLogin(w, r, store, issuer, "google", in, source)
		return
	}

	handleGoogleOIDC(w, r, cfg, store, issuer, strings.TrimSpace(req.IDToken), source)
}

func handleAppleOIDC(
	w http.ResponseWriter,
	r *http.Request,
	cfg config.Config,
	store *auth.Store,
	issuer *auth.TokenIssuer,
	identityToken string,
	nonce *string,
	email *string,
	fullName *string,
	source authRequestSource,
) {
	rid := requestIDFromContext(r.Context())
	log.Printf("auth: request_id=%s provider=apple source=%s action=apple_auth_received token_len=%d", rid, source, len(identityToken))

	verifier, err := auth.NewAppleVerifier(cfg.AppleAudience, cfg.AppleJWKSCacheTTL)
	if err != nil {
		log.Printf("auth: request_id=%s provider=apple source=%s action=apple_verifier_init_failed err=%v", rid, source, err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "authentication processing failed")
		return
	}

	in, err := verifier.VerifyIdentityToken(r.Context(), identityToken, nonce)
	if err != nil {
		log.Printf("auth: request_id=%s provider=apple source=%s action=apple_token_invalid err=%v", rid, source, err)
		writeAPIError(w, http.StatusUnauthorized, "invalid_apple_token", "Apple identity token verification failed")
		return
	}
	log.Printf("auth: request_id=%s provider=apple source=%s action=apple_token_ok apple_sub=%s", rid, source, in.Sub)

	// Apple provides email/fullName out-of-band (credential), not reliably in the identity token.
	// Treat these as profile hints only after the identity token is verified.
	if email != nil {
		if t := strings.TrimSpace(*email); t != "" {
			in.Email = &t
		}
	}
	if fullName != nil {
		if t := strings.TrimSpace(*fullName); t != "" {
			in.DisplayName = &t
		}
	}

	completeAuthLogin(w, r, store, issuer, "apple", in, source)
}

func handleGoogleOIDC(w http.ResponseWriter, r *http.Request, cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer, idToken string, source authRequestSource) {
	rid := requestIDFromContext(r.Context())
	log.Printf("auth: request_id=%s provider=google source=%s action=google_auth_received id_token_len=%d", rid, source, len(idToken))

	payload, err := idtoken.Validate(r.Context(), idToken, cfg.GoogleServerClientID)
	if err != nil {
		log.Printf("auth: request_id=%s provider=google source=%s action=google_token_invalid err=%v", rid, source, err)
		writeAPIError(w, http.StatusUnauthorized, "invalid_google_token", "Google ID token verification failed")
		return
	}
	log.Printf("auth: request_id=%s provider=google source=%s action=google_token_ok", rid, source)

	in, err := auth.GoogleIDTokenClaims(payload)
	if err != nil {
		log.Printf("auth: request_id=%s provider=google source=%s action=google_claims_invalid err=%v", rid, source, err)
		writeAPIError(w, http.StatusUnauthorized, "invalid_google_token", "Google ID token is missing required claims")
		return
	}
	log.Printf("auth: request_id=%s provider=google source=%s google_sub=%s", rid, source, in.Sub)

	completeAuthLogin(w, r, store, issuer, "google", in, source)
}

func handleMockableAuth(
	w http.ResponseWriter,
	r *http.Request,
	cfg config.Config,
	store *auth.Store,
	issuer *auth.TokenIssuer,
	provider string,
	tokenField string,
	source authRequestSource,
	body []byte,
	parse func([]byte) (token string, mock *mockClaimsBody, err error),
) {
	token, mock, err := parse(body)
	if err != nil {
		log.Printf("auth: request_id=%s provider=%s source=%s reason=json_unmarshal_failed err=%v body_len=%d",
			requestIDFromContext(r.Context()), provider, source, err, len(body))
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if strings.TrimSpace(token) == "" {
		log.Printf("auth: request_id=%s provider=%s source=%s reason=missing_token token_field=%s mock_present=%t",
			requestIDFromContext(r.Context()), provider, source, tokenField, mock != nil)
		writeAPIError(w, http.StatusBadRequest, "invalid_request", tokenField+" is required")
		return
	}

	mockAllowed := cfg.MockAuthAllowed()
	if mock != nil && !mockAllowed {
		log.Printf("auth: request_id=%s provider=%s source=%s reason=mock_present_but_disabled app_env=%q auth_dev_mode=%t",
			requestIDFromContext(r.Context()), provider, source, cfg.AppEnv, cfg.AuthDevMode)
		writeAPIError(w, http.StatusForbidden, "mock_auth_disabled", "mock authentication is not enabled for this environment")
		return
	}

	if !mockAllowed {
		writeAPIError(w, http.StatusNotImplemented, "not_implemented", provider+" sign-in is not implemented yet")
		return
	}

	if mock == nil {
		log.Printf("auth: request_id=%s provider=%s source=%s reason=missing_mock_claims token_field=%s token_len=%d",
			requestIDFromContext(r.Context()), provider, source, tokenField, len(strings.TrimSpace(token)))
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "mockClaims is required when mock authentication is enabled")
		return
	}

	if strings.TrimSpace(mock.Sub) == "" {
		log.Printf("auth: request_id=%s provider=%s source=%s reason=missing_mock_sub has_email=%t has_display_name=%t has_avatar_url=%t",
			requestIDFromContext(r.Context()),
			provider,
			source,
			mock.Email != nil && strings.TrimSpace(*mock.Email) != "",
			mock.DisplayName != nil && strings.TrimSpace(*mock.DisplayName) != "",
			mock.AvatarURL != nil && strings.TrimSpace(*mock.AvatarURL) != "",
		)
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "mockClaims.sub is required")
		return
	}

	in := &auth.MockClaimsInput{
		Sub:           strings.TrimSpace(mock.Sub),
		Email:         mock.Email,
		EmailVerified: mock.EmailVerified,
		DisplayName:   mock.DisplayName,
		AvatarURL:     mock.AvatarURL,
	}

	completeAuthLogin(w, r, store, issuer, provider, in, source)
}

func completeAuthLogin(w http.ResponseWriter, r *http.Request, store *auth.Store, issuer *auth.TokenIssuer, provider string, in *auth.MockClaimsInput, source authRequestSource) {
	rid := requestIDFromContext(r.Context())
	sub := strings.TrimSpace(in.Sub)

	switch source {
	case authRequestSourceWeb:
		user, err := store.LoginExistingByIdentity(r.Context(), provider, in)
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("auth: request_id=%s provider=%s source=web action=user_not_found token_type=none", rid, provider)
			writeAPIError(w, http.StatusNotFound, "web_account_required",
				"No account exists for this sign-in. Install the mobile app and create an account first; web sign-in cannot register new users.")
			return
		}
		if err != nil {
			log.Printf("auth: request_id=%s provider=%s source=web identity_lookup err=%v", rid, provider, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "authentication processing failed")
			return
		}
		log.Printf("auth: request_id=%s provider=%s source=web action=user_found user_id=%s", rid, provider, user.ID.String())

		access, err := issuer.IssueWebAccessToken(user.ID, provider)
		if err != nil {
			log.Printf("auth: request_id=%s provider=%s source=web jwt_issue err=%v", rid, provider, err)
			writeAPIError(w, http.StatusInternalServerError, "auth_failed", "could not issue access token")
			return
		}
		log.Printf("auth: request_id=%s provider=%s source=web user_id=%s token_type=web jwt_ok", rid, provider, user.ID.String())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(authLoginResponse{
			AccessToken: access,
			User:        userPublicFromAuth(user),
			TokenType:   auth.TokenTypeWeb,
			ExpiresIn:   8 * 3600,
		})
		return

	case authRequestSourceApp:
		reused, err := store.HasIdentity(r.Context(), provider, sub)
		if err != nil {
			log.Printf("auth: request_id=%s provider=%s source=app identity_lookup err=%v", rid, provider, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "authentication processing failed")
			return
		}
		if reused {
			log.Printf("auth: request_id=%s provider=%s source=app action=identity_reused", rid, provider)
		} else {
			log.Printf("auth: request_id=%s provider=%s source=app action=identity_created", rid, provider)
		}

		user, err := store.LoginOrRegisterMock(r.Context(), provider, in)
		if err != nil {
			log.Printf("auth: request_id=%s provider=%s source=app login err=%v", rid, provider, err)
			writeAPIError(w, http.StatusInternalServerError, "auth_failed", "authentication processing failed")
			return
		}

		access, err := issuer.IssueAccessToken(user.ID, provider)
		if err != nil {
			log.Printf("auth: request_id=%s provider=%s source=app jwt_issue err=%v", rid, provider, err)
			writeAPIError(w, http.StatusInternalServerError, "auth_failed", "could not issue access token")
			return
		}
		log.Printf("auth: request_id=%s provider=%s source=app user_id=%s token_type=app jwt_ok", rid, provider, user.ID.String())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(authLoginResponse{
			AccessToken: access,
			User:        userPublicFromAuth(user),
			TokenType:   auth.TokenTypeApp,
			ExpiresIn:   24 * 3600,
		})
		return

	default:
		log.Printf("auth: request_id=%s provider=%s reason=unsupported_source %q", rid, provider, source)
		writeAPIError(w, http.StatusBadRequest, "invalid_source", "source must be app or web")
	}
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
	if u.AppStorefront.Valid {
		s := u.AppStorefront.String
		out.AppStorefront = &s
	}
	if u.AppStorefrontUpdatedAt.Valid {
		t := u.AppStorefrontUpdatedAt.Time.UTC()
		out.AppStorefrontUpdatedAt = &t
	}
	return out
}
