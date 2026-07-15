package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

type Config struct {
	Env            string // "development" or "production"
	Port           int
	PostmarkToken  string
	PostmarkFrom   string
	PostmarkTo     string
	PixelID            string
	GtagID             string
	TurnstileSiteKey   string
	TurnstileSecretKey string
	DBPath             string
	SessionSecret      string
}

// Load reads configuration from environment variables, applying defaults where not set.
func Load() (*Config, error) {
	port, err := parseInt("PORT", 8080)
	if err != nil {
		return nil, err
	}

	env := envDefault("ENV", "development")

	secret, err := loadSessionSecret(env)
	if err != nil {
		return nil, err
	}

	return &Config{
		Env:           env,
		Port:          port,
		PostmarkToken: os.Getenv("POSTMARK_SERVER_TOKEN"),
		PostmarkFrom:  os.Getenv("POSTMARK_FROM"),
		PostmarkTo:    os.Getenv("POSTMARK_TO"),
		PixelID:            os.Getenv("PIXEL_ID"),
		GtagID:             os.Getenv("GTAG_ID"),
		TurnstileSiteKey:   os.Getenv("TURNSTILE_SITE_KEY"),
		TurnstileSecretKey: os.Getenv("TURNSTILE_SECRET_KEY"),
		DBPath:             envDefault("DB_PATH", "./data/app.db"),
		SessionSecret:      secret,
	}, nil
}

// IsDev reports whether the app is running in development mode.
func (c *Config) IsDev() bool {
	return c.Env != "production"
}

// loadSessionSecret reads SESSION_SECRET and enforces the 32-byte minimum
// required for HMAC signing (CSRF). In production a weak or missing secret
// is a hard error; in development we generate an ephemeral one so a fresh
// clone runs without setup (CSRF cookies reset on each restart).
func loadSessionSecret(env string) (string, error) {
	secret := os.Getenv("SESSION_SECRET")
	if len(secret) >= 32 {
		return secret, nil
	}

	if env == "production" {
		return "", fmt.Errorf(
			"SESSION_SECRET must be at least 32 bytes in production (got %d) — generate one with: openssl rand -hex 32",
			len(secret),
		)
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate dev session secret: %w", err)
	}
	slog.Warn("SESSION_SECRET not set or too short — using an ephemeral dev secret; set a real one with: openssl rand -hex 32")
	return hex.EncodeToString(b), nil
}

// Addr returns the server address string in the format expected by http.ListenAndServe.
func (c *Config) Addr() string {
	return fmt.Sprintf(":%d", c.Port)
}

// envDefault reads an environment variable, returning the fallback if unset.
func envDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// parseInt reads an environment variable as an integer, returning the fallback if unset.
func parseInt(key string, fallback int) (int, error) {
	val := os.Getenv(key)
	if val == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("invalid value for %s: %q", key, val)
	}
	return n, nil
}