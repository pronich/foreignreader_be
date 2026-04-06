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

	// GoogleServerClientID is the OAuth client ID (audience) used to validate Google ID tokens (e.g. iOS server client ID).
	GoogleServerClientID string

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

	authDevMode := parseBoolEnv("AUTH_DEV_MODE", false)

	googleServerClientID := strings.TrimSpace(os.Getenv("GOOGLE_SERVER_CLIENT_ID"))
	if googleServerClientID == "" {
		log.Fatal("config: GOOGLE_SERVER_CLIENT_ID is required")
	}

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
	validateStripeForNonDev(appEnv, stripeSecretKey, stripeWebhookSecret, stripePriceIDPro)

	return Config{
		Port:   port,
		AppEnv: appEnv,

		DatabaseURL: databaseURL,

		AuthDevMode: authDevMode,
		JWTSecret:   jwtSecret,

		GoogleServerClientID: googleServerClientID,

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

func isDevelopmentLikeEnv(appEnv string) bool {
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "development", "dev", "local":
		return true
	default:
		return false
	}
}

func validateStripeForNonDev(appEnv, secretKey, webhookSecret, priceIDPro string) {
	if isDevelopmentLikeEnv(appEnv) {
		return
	}
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
		log.Fatalf("config: Stripe variables required when APP_ENV=%s: %s", appEnv, strings.Join(missing, ", "))
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
