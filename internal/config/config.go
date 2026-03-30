package config

import (
	"fmt"
	"log"
	"os"
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

	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration

	OpenAIAPIKey         string
	TranslateModel       string
	TranslatePromptText  string
	TranslateTimeout     time.Duration
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

	return Config{
		Port:   port,
		AppEnv: appEnv,

		DatabaseURL: databaseURL,

		AuthDevMode: authDevMode,
		JWTSecret:   jwtSecret,

		// READ_TIMEOUT: time to read the full request (body included).
		ReadTimeout: getDurationEnv("READ_TIMEOUT", 30*time.Second),
		// WRITE_TIMEOUT: includes handler execution time; must exceed slow LLM calls (see TRANSLATE_CONTEXT_TIMEOUT).
		WriteTimeout: getDurationEnv("WRITE_TIMEOUT", 120*time.Second),
		IdleTimeout:     getDurationEnv("IDLE_TIMEOUT", 120*time.Second),
		ShutdownTimeout: getDurationEnv("SHUTDOWN_TIMEOUT", 10*time.Second),

		OpenAIAPIKey:        key,
		TranslateModel:      model,
		TranslatePromptText: promptText,
		TranslateTimeout:    translateTimeout,
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

