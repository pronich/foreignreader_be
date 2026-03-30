package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	Port   string
	AppEnv string

	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

func Load() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "development"
	}

	return Config{
		Port:   port,
		AppEnv: appEnv,

		ReadTimeout:     getDurationEnv("READ_TIMEOUT", 5*time.Second),
		WriteTimeout:    getDurationEnv("WRITE_TIMEOUT", 10*time.Second),
		IdleTimeout:     getDurationEnv("IDLE_TIMEOUT", 60*time.Second),
		ShutdownTimeout: getDurationEnv("SHUTDOWN_TIMEOUT", 10*time.Second),
	}
}

func (c Config) Addr() string {
	return ":" + c.Port
}

func (c Config) BaseURL() string {
	return fmt.Sprintf("http://localhost:%s", c.Port)
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

