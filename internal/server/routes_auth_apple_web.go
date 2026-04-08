package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/config"
)

func registerAppleWebAuthRoutes(mux *http.ServeMux, cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer) {
	mux.HandleFunc("GET /auth/apple/callback", handleAppleWebCallback(cfg, store, issuer))
}

func handleAppleWebCallback(cfg config.Config, store *auth.Store, issuer *auth.TokenIssuer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rid := requestIDFromContext(r.Context())
		log.Printf("auth: request_id=%s provider=apple_web action=callback", rid)

		if !cfg.AppleWebSignInConfigured() {
			log.Printf("auth: request_id=%s provider=apple_web reason=not_configured", rid)
			respondAppleWebCallback(w, r, cfg, nil, http.StatusServiceUnavailable, "service_unavailable", "Sign in with Apple web is not configured")
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()
		if errO := strings.TrimSpace(q.Get("error")); errO != "" {
			log.Printf("auth: request_id=%s provider=apple_web action=oauth_error oauth_error=%s", rid, errO)
			respondAppleWebCallback(w, r, cfg, nil, http.StatusBadRequest, "oauth_denied", "Sign in with Apple was cancelled or failed")
			return
		}

		code := strings.TrimSpace(q.Get("code"))
		if code == "" {
			respondAppleWebCallback(w, r, cfg, nil, http.StatusBadRequest, "missing_code", "authorization code is required")
			return
		}

		var appleFirst *auth.AppleWebUserParam
		if uq := q.Get("user"); uq != "" {
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

		access, err := issuer.IssueWebAccessToken(user.ID, "apple")
		if err != nil {
			log.Printf("auth: request_id=%s provider=apple_web source=web jwt_issue err=%v", rid, err)
			respondAppleWebCallback(w, r, cfg, nil, http.StatusInternalServerError, "auth_failed", "could not issue access token")
			return
		}
		log.Printf("auth: request_id=%s provider=apple_web source=web user_id=%s token_type=web jwt_ok", rid, user.ID.String())

		resp := &authLoginResponse{
			AccessToken: access,
			User:        userPublicFromAuth(user),
			TokenType:   auth.TokenTypeWeb,
			ExpiresIn:   8 * 3600,
		}
		respondAppleWebCallback(w, r, cfg, resp, http.StatusOK, "", "")
	}
}

// respondAppleWebCallback writes JSON (same envelope as /api/v1/auth/*) or redirects to APPLE_WEB_LOGIN_LANDING_URL with query params.
func respondAppleWebCallback(w http.ResponseWriter, r *http.Request, cfg config.Config, success *authLoginResponse, errStatus int, errCode, errMsg string) {
	landing := strings.TrimSpace(cfg.AppleWebLoginLandingURL)
	if landing != "" {
		u, err := url.Parse(landing)
		if err == nil && u.Scheme != "" && u.Host != "" {
			q := u.Query()
			if success != nil {
				q.Set("accessToken", success.AccessToken)
				q.Set("tokenType", success.TokenType)
				q.Set("userId", success.User.ID)
			} else {
				q.Set("error_code", errCode)
				q.Set("error_message", errMsg)
			}
			u.RawQuery = q.Encode()
			log.Printf("auth: apple_web action=redirect host=%s success=%t", u.Host, success != nil)
			http.Redirect(w, r, u.String(), http.StatusFound)
			return
		}
	}

	if success != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(success)
		return
	}

	writeAPIError(w, errStatus, errCode, errMsg)
}
