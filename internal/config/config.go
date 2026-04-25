package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port   string
	AppEnv string

	// DatabaseURL is a PostgreSQL connection string (e.g. postgres://user:pass@host:5432/dbname?sslmode=disable).
	DatabaseURL string

	// AuthDevMode enables mock provider claims for local testing (see AUTH_DEV_MODE).
	AuthDevMode bool
	// JWTSecret signs backend-issued access tokens (required).
	JWTSecret string

	// AccessTokenTTL is the lifetime of JWT access tokens (app and web).
	AccessTokenTTL time.Duration
	// RefreshSessionTTL is the lifetime of refresh-token sessions stored in auth_sessions.
	RefreshSessionTTL time.Duration

	// GoogleServerClientID is the OAuth client ID (audience) used to validate Google ID tokens (e.g. iOS server client ID).
	GoogleServerClientID string

	// AppleAudience is the expected "aud" for Apple identity tokens (usually iOS bundle ID and/or Services ID).
	// Supports a comma-separated list to allow multiple audiences.
	AppleAudience string
	// AppleJWKSCacheTTL is the in-memory cache TTL for Apple's JWKS.
	AppleJWKSCacheTTL time.Duration

	// Apple IAP (App Store Server API) credentials + settings.
	AppleIAPIssuerID     string
	AppleIAPKeyID        string
	AppleIAPPrivateKey   string
	AppleIAPBundleID     string
	AppleIAPEnvironment  string // sandbox|production
	AppleIAPProProductID string

	// Sign in with Apple — web (Services ID) callback /auth/apple/callback.
	// APPLE_WEB_CLIENT_ID = Services ID; APPLE_WEB_REDIRECT_URL must match App Store Connect exactly.
	// APPLE_TEAM_ID, APPLE_KEY_ID, APPLE_PRIVATE_KEY = key used to sign the client_secret JWT for token exchange.
	// Optional APPLE_WEB_SUCCESS_REDIRECT_URL / APPLE_WEB_ACCOUNT_REQUIRED_REDIRECT_URL: browser redirects after callback (JSON if unset).
	AppleWebClientID                   string
	AppleWebRedirectURL                string
	AppleTeamID                        string
	AppleWebSignInKeyID                string // env APPLE_KEY_ID (Sign in with Apple key, not IAP)
	AppleWebPrivateKey                 string // env APPLE_PRIVATE_KEY (PEM, multiline or \n escaped)
	AppleWebSuccessRedirectURL         string // e.g. https://foreignreader.io/cabinet
	AppleWebAccountRequiredRedirectURL string // e.g. https://foreignreader.io/account-required

	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration

	OpenAIAPIKey        string
	TranslateModel      string
	TranslatePromptText string
	TranslateTimeout    time.Duration

	// OnboardingContextTranslateToken is the shared app secret used only to obtain short-lived onboarding access tokens (POST /api/v1/onboarding/session). Empty disables onboarding session + translation routes.
	OnboardingContextTranslateToken string

	// OnboardingSessionTokenTTL is the lifetime of opaque onboarding access tokens (not JWT).
	OnboardingSessionTokenTTL time.Duration

	// OnboardingSessionRateLimitPerIP is max POST /onboarding/session requests per client IP per minute.
	OnboardingSessionRateLimitPerIP int
	// OnboardingTranslateRateLimitPerIP is max POST /onboarding/translate/context requests per client IP per minute.
	OnboardingTranslateRateLimitPerIP int
	// OnboardingTranslateRateLimitPerToken is max translate requests per issued onboarding token per minute.
	OnboardingTranslateRateLimitPerToken int

	// OnboardingTokenCleanupRetention deletes onboarding_access_tokens rows older than this (expired or revoked).
	OnboardingTokenCleanupRetention time.Duration

	// FreeContextTranslationsPerMonth is the free monthly context-translation allowance for new users (stored as monthly_limit on signup).
	FreeContextTranslationsPerMonth int

	// StripeSecretKey is the Stripe API secret key (sk_live_... / sk_test_...). Not logged.
	StripeSecretKey string
	// StripeWebhookSecret is used to verify Stripe webhook signatures (whsec_...). Not logged.
	StripeWebhookSecret string
	// StripePriceIDPro is the Price ID for the Pro product (price_...).
	StripePriceIDPro string
	// StripeRedirectURL is where Stripe Checkout returns the user after success/cancel.
	StripeRedirectURL string

	// CORSAllowedOrigins lists allowed browser Origin values for /api/* (see CORS_ALLOWED_ORIGINS).
	CORSAllowedOrigins []string

	// AnalyticsEnabled controls whether analytics ingestion writes to the database.
	AnalyticsEnabled bool
	// AnalyticsIngestionKey is an optional shared key required by /api/v1/analytics/events.
	AnalyticsIngestionKey string
}

func Load() Config {
	// Local development: load `.env` when present (ignore missing file)
	_ = godotenv.Load()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "development"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		log.Fatal("config: DATABASE_URL is required")
	}

	jwtSecret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if jwtSecret == "" {
		log.Fatal("config: JWT_SECRET is required")
	}

	accessTokenTTL := getDurationEnv("ACCESS_TOKEN_TTL", 8*time.Hour)
	refreshSessionTTL := getDurationEnv("REFRESH_SESSION_TTL", 180*24*time.Hour)

	authDevMode := parseBoolEnv("AUTH_DEV_MODE", false)

	googleServerClientID := strings.TrimSpace(os.Getenv("GOOGLE_SERVER_CLIENT_ID"))
	if googleServerClientID == "" {
		log.Fatal("config: GOOGLE_SERVER_CLIENT_ID is required")
	}

	appleAudience := strings.TrimSpace(os.Getenv("APPLE_AUDIENCE"))
	if appleAudience == "" {
		log.Fatal("config: APPLE_AUDIENCE is required")
	}
	appleJWKSCacheTTL := getDurationEnv("APPLE_JWKS_CACHE_TTL", 6*time.Hour)

	appleIAPIssuerID := strings.TrimSpace(os.Getenv("APPLE_IAP_ISSUER_ID"))
	appleIAPKeyID := strings.TrimSpace(os.Getenv("APPLE_IAP_KEY_ID"))
	appleIAPPrivateKey := normalizeMultilineEnv(os.Getenv("APPLE_IAP_PRIVATE_KEY"))
	appleIAPBundleID := strings.TrimSpace(os.Getenv("APPLE_IAP_BUNDLE_ID"))
	appleIAPEnvironment := strings.TrimSpace(os.Getenv("APPLE_IAP_ENVIRONMENT"))
	if appleIAPEnvironment == "" {
		appleIAPEnvironment = "production"
	}
	appleIAPProProductID := strings.TrimSpace(os.Getenv("APPLE_IAP_PRO_PRODUCT_ID"))

	appleWebClientID := strings.TrimSpace(os.Getenv("APPLE_WEB_CLIENT_ID"))
	appleWebRedirectURL := strings.TrimSpace(os.Getenv("APPLE_WEB_REDIRECT_URL"))
	appleTeamID := strings.TrimSpace(os.Getenv("APPLE_TEAM_ID"))
	appleWebSignInKeyID := strings.TrimSpace(os.Getenv("APPLE_KEY_ID"))
	appleWebPrivateKey := normalizeMultilineEnv(os.Getenv("APPLE_PRIVATE_KEY"))
	appleWebSuccessRedirectURL := strings.TrimSpace(os.Getenv("APPLE_WEB_SUCCESS_REDIRECT_URL"))
	appleWebAccountRequiredRedirectURL := strings.TrimSpace(os.Getenv("APPLE_WEB_ACCOUNT_REQUIRED_REDIRECT_URL"))

	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		log.Fatal("config: OPENAI_API_KEY is required")
	}

	model := os.Getenv("TRANSLATE_CONTEXT_MODEL")
	if model == "" {
		model = "gpt-5.4-mini"
	}

	promptPath := os.Getenv("TRANSLATE_CONTEXT_PROMPT_FILE")
	if promptPath == "" {
		promptPath = "prompts/translate_context.txt"
	}
	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		log.Fatalf("config: cannot read prompt file %q: %v", promptPath, err)
	}
	promptText := string(promptBytes)

	translateTimeout := getDurationEnv("TRANSLATE_CONTEXT_TIMEOUT", 15*time.Second)

	onboardingContextTranslateToken := strings.TrimSpace(os.Getenv("ONBOARDING_CONTEXT_TRANSLATE_TOKEN"))

	freeContextTranslationsPerMonth := parseIntEnvNonNegative("FREE_CONTEXT_TRANSLATIONS_PER_MONTH", 100)

	onboardingSessionTTL := getDurationEnv("ONBOARDING_SESSION_TOKEN_TTL", 15*time.Minute)
	onboardingSessionRateIP := parseIntEnvWithDefault("ONBOARDING_SESSION_RATE_LIMIT_PER_IP", 30)
	onboardingTranslateRateIP := parseIntEnvWithDefault("ONBOARDING_TRANSLATE_RATE_LIMIT_PER_IP", 60)
	onboardingTranslateRateTok := parseIntEnvWithDefault("ONBOARDING_TRANSLATE_RATE_LIMIT_PER_TOKEN", 100)
	onboardingCleanupRetention := getDurationEnv("ONBOARDING_TOKEN_CLEANUP_RETENTION", 30*24*time.Hour)

	stripeSecretKey := strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY"))
	stripeWebhookSecret := strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET"))
	stripePriceIDPro := strings.TrimSpace(os.Getenv("STRIPE_PRICE_ID_PRO"))
	stripeRedirectURL := strings.TrimSpace(os.Getenv("STRIPE_REDIRECT_URL"))
	if stripeRedirectURL == "" {
		stripeRedirectURL = "https://foreignreader.io/cabinet"
	}
	validateStripeRequired(stripeSecretKey, stripeWebhookSecret, stripePriceIDPro)

	corsAllowedOrigins := parseCORSAllowedOrigins(os.Getenv("CORS_ALLOWED_ORIGINS"))

	analyticsEnabled := parseBoolEnv("ANALYTICS_ENABLED", true)
	analyticsIngestionKey := strings.TrimSpace(os.Getenv("ANALYTICS_INGESTION_KEY"))

	return Config{
		Port:   port,
		AppEnv: appEnv,

		DatabaseURL: databaseURL,

		AuthDevMode: authDevMode,
		JWTSecret:   jwtSecret,

		AccessTokenTTL:    accessTokenTTL,
		RefreshSessionTTL: refreshSessionTTL,

		GoogleServerClientID: googleServerClientID,
		AppleAudience:        appleAudience,
		AppleJWKSCacheTTL:    appleJWKSCacheTTL,

		AppleIAPIssuerID:     appleIAPIssuerID,
		AppleIAPKeyID:        appleIAPKeyID,
		AppleIAPPrivateKey:   appleIAPPrivateKey,
		AppleIAPBundleID:     appleIAPBundleID,
		AppleIAPEnvironment:  appleIAPEnvironment,
		AppleIAPProProductID: appleIAPProProductID,

		AppleWebClientID:                   appleWebClientID,
		AppleWebRedirectURL:                appleWebRedirectURL,
		AppleTeamID:                        appleTeamID,
		AppleWebSignInKeyID:                appleWebSignInKeyID,
		AppleWebPrivateKey:                 appleWebPrivateKey,
		AppleWebSuccessRedirectURL:         appleWebSuccessRedirectURL,
		AppleWebAccountRequiredRedirectURL: appleWebAccountRequiredRedirectURL,

		ReadTimeout: getDurationEnv("READ_TIMEOUT", 30*time.Second),
		// Includes handler time; set above TRANSLATE_CONTEXT_TIMEOUT so the HTTP server does not cut off OpenAI before the translate deadline.
		WriteTimeout:    getDurationEnv("WRITE_TIMEOUT", 120*time.Second),
		IdleTimeout:     getDurationEnv("IDLE_TIMEOUT", 120*time.Second),
		ShutdownTimeout: getDurationEnv("SHUTDOWN_TIMEOUT", 10*time.Second),

		OpenAIAPIKey:        key,
		TranslateModel:      model,
		TranslatePromptText: promptText,
		TranslateTimeout:    translateTimeout,

		OnboardingContextTranslateToken: onboardingContextTranslateToken,

		OnboardingSessionTokenTTL:            onboardingSessionTTL,
		OnboardingSessionRateLimitPerIP:      onboardingSessionRateIP,
		OnboardingTranslateRateLimitPerIP:    onboardingTranslateRateIP,
		OnboardingTranslateRateLimitPerToken: onboardingTranslateRateTok,
		OnboardingTokenCleanupRetention:      onboardingCleanupRetention,

		FreeContextTranslationsPerMonth: freeContextTranslationsPerMonth,

		StripeSecretKey:     stripeSecretKey,
		StripeWebhookSecret: stripeWebhookSecret,
		StripePriceIDPro:    stripePriceIDPro,
		StripeRedirectURL:   stripeRedirectURL,

		CORSAllowedOrigins: corsAllowedOrigins,

		AnalyticsEnabled:      analyticsEnabled,
		AnalyticsIngestionKey: analyticsIngestionKey,
	}
}

func (c Config) Addr() string {
	return ":" + c.Port
}

func (c Config) BaseURL() string {
	return fmt.Sprintf("http://localhost:%s", c.Port)
}

// MockAuthAllowed is true when mock login is permitted (dev flag on and not production).
func (c Config) MockAuthAllowed() bool {
	env := strings.ToLower(strings.TrimSpace(c.AppEnv))
	return c.AuthDevMode && env != "production"
}

// AppleIAPConfigured is true when all required Apple IAP credentials are present.
func (c Config) AppleIAPConfigured() bool {
	return strings.TrimSpace(c.AppleIAPIssuerID) != "" &&
		strings.TrimSpace(c.AppleIAPKeyID) != "" &&
		strings.TrimSpace(c.AppleIAPPrivateKey) != "" &&
		strings.TrimSpace(c.AppleIAPBundleID) != "" &&
		strings.TrimSpace(c.AppleIAPEnvironment) != "" &&
		strings.TrimSpace(c.AppleIAPProProductID) != ""
}

// AppleWebSignInConfigured is true when Sign in with Apple web callback can exchange codes and validate id_tokens.
func (c Config) AppleWebSignInConfigured() bool {
	return strings.TrimSpace(c.AppleWebClientID) != "" &&
		strings.TrimSpace(c.AppleWebRedirectURL) != "" &&
		strings.TrimSpace(c.AppleTeamID) != "" &&
		strings.TrimSpace(c.AppleWebSignInKeyID) != "" &&
		strings.TrimSpace(c.AppleWebPrivateKey) != ""
}

// parseCORSAllowedOrigins parses CORS_ALLOWED_ORIGINS (comma-separated exact Origin values).
// Empty or unset defaults to production web origins.
func parseCORSAllowedOrigins(raw string) []string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return []string{"https://foreignreader.io", "https://www.foreignreader.io"}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"https://foreignreader.io", "https://www.foreignreader.io"}
	}
	return out
}

func normalizeMultilineEnv(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Support env values that encode newlines as "\n" (common in Docker/CI secrets).
	s = strings.ReplaceAll(s, `\n`, "\n")
	return s
}

func parseBoolEnv(key string, def bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return def
	}
	switch raw {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return def
	}
}

func parseIntEnvWithDefault(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return def
	}
	return v
}

func parseIntEnvNonNegative(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		log.Fatalf("config: %s must be a non-negative integer", key)
	}
	return v
}

func validateStripeRequired(secretKey, webhookSecret, priceIDPro string) {
	var missing []string
	if secretKey == "" {
		missing = append(missing, "STRIPE_SECRET_KEY")
	}
	if webhookSecret == "" {
		missing = append(missing, "STRIPE_WEBHOOK_SECRET")
	}
	if priceIDPro == "" {
		missing = append(missing, "STRIPE_PRICE_ID_PRO")
	}
	if len(missing) > 0 {
		log.Fatalf("config: Stripe variables required: %s", strings.Join(missing, ", "))
	}
}

func getDurationEnv(key string, def time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return def
	}
	return d
}
