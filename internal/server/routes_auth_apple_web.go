package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/config"
	"foreignreader_be/internal/entitlement"
)

// maxAppleWebCallbackBody caps POST body size for Apple's form_post callback (application/x-www-form-urlencoded).
const maxAppleWebCallbackBody = 64 << 10 // 64 KiB; Apple sends a few KB

func registerAppleWebAuthRoutes(mux *http.ServeMux, cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer, entStore *entitlement.Store) {
	h := handleAppleWebCallback(cfg, store, issuer, entStore)
	// GET: response_mode=query. POST: response_mode=form_post (required when using Sign in with Apple JS with name/email scope).
	mux.HandleFunc("GET /auth/apple/callback", h)
	mux.HandleFunc("POST /auth/apple/callback", h)
}

// appleWebAuthorizationValues returns OAuth callback parameters from the query string (GET) or form body (POST).
// Apple's web flow POSTs application/x-www-form-urlencoded to redirect_uri with code, state, optional user, optional id_token.
func appleWebAuthorizationValues(r *http.Request, w http.ResponseWriter) (url.Values, error) {
	switch r.Method {
	case http.MethodGet:
		return r.URL.Query(), nil
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, maxAppleWebCallbackBody)
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		return r.Form, nil
	default:
		return nil, errors.New("unsupported method")
	}
}

func handleAppleWebCallback(cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer, entStore *entitlement.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rid := requestIDFromContext(r.Context())
		log.Printf("auth: request_id=%s provider=apple_web action=callback", rid)

		if !cfg.AppleWebSignInConfigured() {
			log.Printf("auth: request_id=%s provider=apple_web reason=not_configured", rid)
			respondAppleWebCallback(w, r, cfg, nil, http.StatusServiceUnavailable, "service_unavailable", "Sign in with Apple web is not configured")
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		transport := "query"
		if r.Method == http.MethodPost {
			transport = "form_post"
		}
		log.Printf("auth: request_id=%s provider=apple_web transport=%s", rid, transport)

		vals, err := appleWebAuthorizationValues(r, w)
		if err != nil {
			log.Printf("auth: request_id=%s provider=apple_web action=parse_form_failed err=%v", rid, err)
			respondAppleWebCallback(w, r, cfg, nil, http.StatusBadRequest, "malformed_callback", "invalid or oversized callback request")
			return
		}

		if errO := strings.TrimSpace(vals.Get("error")); errO != "" {
			log.Printf("auth: request_id=%s provider=apple_web action=oauth_error oauth_error=%s", rid, errO)
			respondAppleWebCallback(w, r, cfg, nil, http.StatusBadRequest, "oauth_denied", "Sign in with Apple was cancelled or failed")
			return
		}

		code := strings.TrimSpace(vals.Get("code"))
		if code == "" {
			respondAppleWebCallback(w, r, cfg, nil, http.StatusBadRequest, "missing_code", "authorization code is required")
			return
		}

		var appleFirst *auth.AppleWebUserParam
		if uq := vals.Get("user"); uq != "" {
			if u, err := auth.ParseAppleWebUserQuery(uq); err == nil && u != nil {
				appleFirst = u
			}
		}

		clientSecret, err := auth.GenerateAppleWebClientSecret(cfg.AppleTeamID, cfg.AppleWebClientID, cfg.AppleWebSignInKeyID, cfg.AppleWebPrivateKey)
		if err != nil {
			log.Printf("auth: request_id=%s provider=apple_web action=client_secret_failed err=%v", rid, err)
			respondAppleWebCallback(w, r, cfg, nil, http.StatusInternalServerError, "internal_error", "authentication processing failed")
			return
		}

		tokResp, err := auth.ExchangeAppleAuthorizationCode(r.Context(), cfg.AppleWebClientID, clientSecret, cfg.AppleWebRedirectURL, code)
		if err != nil {
			log.Printf("auth: request_id=%s provider=apple_web action=token_exchange_failed err=%v", rid, err)
			respondAppleWebCallback(w, r, cfg, nil, http.StatusUnauthorized, "apple_token_exchange_failed", "could not complete Sign in with Apple")
			return
		}

		verifier, err := auth.NewAppleVerifier(cfg.AppleWebClientID, cfg.AppleJWKSCacheTTL)
		if err != nil {
			log.Printf("auth: request_id=%s provider=apple_web action=verifier_init_failed err=%v", rid, err)
			respondAppleWebCallback(w, r, cfg, nil, http.StatusInternalServerError, "internal_error", "authentication processing failed")
			return
		}

		in, err := verifier.VerifyIdentityToken(r.Context(), tokResp.IDToken, nil)
		if err != nil {
			log.Printf("auth: request_id=%s provider=apple_web action=id_token_invalid err=%v", rid, err)
			respondAppleWebCallback(w, r, cfg, nil, http.StatusUnauthorized, "invalid_apple_token", "Apple identity token verification failed")
			return
		}
		log.Printf("auth: request_id=%s provider=apple_web action=id_token_ok apple_sub=%s", rid, in.Sub)

		if appleFirst != nil {
			if em := strings.TrimSpace(appleFirst.Email); em != "" {
				in.Email = &em
				t := true
				in.EmailVerified = &t
			}
			if appleFirst.Name != nil {
				fn := strings.TrimSpace(appleFirst.Name.FirstName + " " + appleFirst.Name.LastName)
				if fn != "" {
					in.DisplayName = &fn
				}
			}
		}

		user, err := store.LoginExistingByIdentity(r.Context(), "apple", in)
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("auth: request_id=%s provider=apple_web source=web action=user_not_found", rid)
			respondAppleWebCallback(w, r, cfg, nil, http.StatusNotFound, "web_account_required",
				"No account exists for this sign-in. Install the mobile app and create an account first; web sign-in cannot register new users.")
			return
		}
		if err != nil {
			log.Printf("auth: request_id=%s provider=apple_web source=web identity_lookup err=%v", rid, err)
			respondAppleWebCallback(w, r, cfg, nil, http.StatusInternalServerError, "internal_error", "authentication processing failed")
			return
		}
		log.Printf("auth: request_id=%s provider=apple_web source=web action=user_found user_id=%s", rid, user.ID.String())

		access, accessExp, err := issuer.IssueWebAccessToken(user.ID, "apple")
		if err != nil {
			log.Printf("auth: request_id=%s provider=apple_web source=web jwt_issue err=%v", rid, err)
			respondAppleWebCallback(w, r, cfg, nil, http.StatusInternalServerError, "auth_failed", "could not issue access token")
			return
		}
		log.Printf("auth: request_id=%s provider=apple_web source=web user_id=%s token_type=web jwt_ok", rid, user.ID.String())

		isOwner, err := entStore.IsOwner(r.Context(), user.ID)
		if err != nil {
			log.Printf("auth: request_id=%s provider=apple_web source=web owner_check err=%v", rid, err)
			isOwner = false
		}

		resp := &authLoginResponse{
			AccessToken:          access,
			AccessTokenExpiresAt: accessExp.UTC(),
			User:                 userPublicFromAuth(user),
			TokenType:            auth.TokenTypeWeb,
			ExpiresIn:            int64(cfg.AccessTokenTTL / time.Second),
			IsOwner:              isOwner,
		}
		respondAppleWebCallback(w, r, cfg, resp, http.StatusOK, "", "")
	}
}

// respondAppleWebCallback writes JSON (same envelope as /api/v1/auth/*), or redirects:
//   - success → APPLE_WEB_SUCCESS_REDIRECT_URL (accessToken, tokenType, userId)
//   - web_account_required → APPLE_WEB_ACCOUNT_REQUIRED_REDIRECT_URL (error_code, error_message)
func respondAppleWebCallback(w http.ResponseWriter, r *http.Request, cfg config.Config, success *authLoginResponse, errStatus int, errCode, errMsg string) {
	if success != nil {
		if dest := strings.TrimSpace(cfg.AppleWebSuccessRedirectURL); dest != "" {
			if u, err := url.Parse(dest); err == nil && u.Scheme != "" && u.Host != "" {
				q := u.Query()
				q.Set("accessToken", success.AccessToken)
				q.Set("tokenType", success.TokenType)
				q.Set("userId", success.User.ID)
				q.Set("isOwner", strconv.FormatBool(success.IsOwner))
				if !success.AccessTokenExpiresAt.IsZero() {
					q.Set("accessTokenExpiresAt", success.AccessTokenExpiresAt.UTC().Format(time.RFC3339))
				}
				u.RawQuery = q.Encode()
				log.Printf("auth: apple_web action=redirect outcome=success host=%s", u.Host)
				http.Redirect(w, r, u.String(), http.StatusFound)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(success)
		return
	}

	if errCode == "web_account_required" {
		if dest := strings.TrimSpace(cfg.AppleWebAccountRequiredRedirectURL); dest != "" {
			if u, err := url.Parse(dest); err == nil && u.Scheme != "" && u.Host != "" {
				q := u.Query()
				q.Set("error_code", errCode)
				q.Set("error_message", errMsg)
				u.RawQuery = q.Encode()
				log.Printf("auth: apple_web action=redirect outcome=web_account_required host=%s", u.Host)
				http.Redirect(w, r, u.String(), http.StatusFound)
				return
			}
		}
	}

	writeAPIError(w, errStatus, errCode, errMsg)
}
