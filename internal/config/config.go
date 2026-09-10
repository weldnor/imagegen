// Package config loads all runtime settings from environment variables only.
//
// No configuration file is read. Load takes a getenv function (os.Getenv in
// production, a map-backed stub in tests) so it is verifiable that startup never
// touches the filesystem for configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Default values for every optional setting. These are the single source of
// truth the .env.example file and README must match.
const (
	DefaultImageStorageDir       = "/data/images"
	DefaultListenAddr            = ":8080"
	DefaultSessionTTL            = 720 * time.Hour
	DefaultSessionCookieSecure   = true
	DefaultSessionCookieName     = "imagen_session"
	DefaultMaxUploadBytes        = 33554432 // 32 MiB
	DefaultOpenRouterConcurrency = 4
	DefaultOpenRouterTimeout     = 120 * time.Second
	DefaultOpenRouterBaseURL     = "https://openrouter.ai/api/v1"
)

// EnvVarNames lists every environment variable Load reads, in declaration
// order. The .env.example file is kept in lockstep with this list (verified by
// TestEnvExampleMatchesLoader).
func EnvVarNames() []string {
	return []string{
		"OPENROUTER_API_KEY",
		"TELEGRAM_BOT_TOKEN",
		"ADMIN_PASSWORD",
		"DATABASE_URL",
		"IMAGE_STORAGE_DIR",
		"LISTEN_ADDR",
		"SESSION_TTL",
		"SESSION_COOKIE_SECURE",
		"SESSION_COOKIE_NAME",
		"MAX_UPLOAD_BYTES",
		"OPENROUTER_CONCURRENCY",
		"OPENROUTER_TIMEOUT",
		"OPENROUTER_BASE_URL",
		"STATIC_DIR",
	}
}

// RequiredEnvVarNames lists the environment variables Load rejects startup
// without.
func RequiredEnvVarNames() []string {
	return []string{"OPENROUTER_API_KEY", "TELEGRAM_BOT_TOKEN", "ADMIN_PASSWORD", "DATABASE_URL"}
}

// Config holds the fully parsed and validated runtime configuration.
type Config struct {
	OpenRouterAPIKey string
	TelegramBotToken string
	AdminPassword    string
	DatabaseURL      string

	ImageStorageDir     string
	ListenAddr          string
	SessionTTL          time.Duration
	SessionCookieSecure bool
	SessionCookieName   string
	MaxUploadBytes      int64

	OpenRouterConcurrency int
	OpenRouterTimeout     time.Duration
	OpenRouterBaseURL     string

	// StaticDir, when non-empty, serves the frontend from disk instead of the
	// binary's embedded copy.
	StaticDir string
}

// Load reads every setting from getenv, applies defaults for optional values,
// and validates required and unusable values. On any problem it returns an
// error whose message names the offending environment variable. It never reads
// a file.
func Load(getenv func(string) string) (*Config, error) {
	c := &Config{
		ImageStorageDir:       DefaultImageStorageDir,
		ListenAddr:            DefaultListenAddr,
		SessionTTL:            DefaultSessionTTL,
		SessionCookieSecure:   DefaultSessionCookieSecure,
		SessionCookieName:     DefaultSessionCookieName,
		MaxUploadBytes:        DefaultMaxUploadBytes,
		OpenRouterConcurrency: DefaultOpenRouterConcurrency,
		OpenRouterTimeout:     DefaultOpenRouterTimeout,
		OpenRouterBaseURL:     DefaultOpenRouterBaseURL,
	}

	// --- required strings ---
	c.OpenRouterAPIKey = strings.TrimSpace(getenv("OPENROUTER_API_KEY"))
	if c.OpenRouterAPIKey == "" {
		return nil, errors.New("OPENROUTER_API_KEY is required but not set")
	}
	c.DatabaseURL = strings.TrimSpace(getenv("DATABASE_URL"))
	if c.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required but not set")
	}

	c.TelegramBotToken = strings.TrimSpace(getenv("TELEGRAM_BOT_TOKEN"))
	if c.TelegramBotToken == "" {
		return nil, errors.New("TELEGRAM_BOT_TOKEN is required but not set")
	}
	c.AdminPassword = strings.TrimSpace(getenv("ADMIN_PASSWORD"))
	if c.AdminPassword == "" {
		return nil, errors.New("ADMIN_PASSWORD is required but not set")
	}

	// --- optional overrides ---
	if v := strings.TrimSpace(getenv("IMAGE_STORAGE_DIR")); v != "" {
		c.ImageStorageDir = v
	}
	if v := strings.TrimSpace(getenv("LISTEN_ADDR")); v != "" {
		c.ListenAddr = v
	}
	if v := strings.TrimSpace(getenv("SESSION_COOKIE_NAME")); v != "" {
		c.SessionCookieName = v
	}
	if v := strings.TrimSpace(getenv("OPENROUTER_BASE_URL")); v != "" {
		c.OpenRouterBaseURL = strings.TrimRight(v, "/")
	}
	c.StaticDir = strings.TrimSpace(getenv("STATIC_DIR"))

	if v := strings.TrimSpace(getenv("SESSION_TTL")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("SESSION_TTL: %q is not a valid Go duration: %w", v, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("SESSION_TTL: must be positive, got %q", v)
		}
		c.SessionTTL = d
	}
	if v := strings.TrimSpace(getenv("OPENROUTER_TIMEOUT")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("OPENROUTER_TIMEOUT: %q is not a valid Go duration: %w", v, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("OPENROUTER_TIMEOUT: must be positive, got %q", v)
		}
		c.OpenRouterTimeout = d
	}

	if v := strings.TrimSpace(getenv("SESSION_COOKIE_SECURE")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("SESSION_COOKIE_SECURE: %q is not a valid boolean: %w", v, err)
		}
		c.SessionCookieSecure = b
	}

	if v := strings.TrimSpace(getenv("MAX_UPLOAD_BYTES")); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("MAX_UPLOAD_BYTES: %q is not a valid integer: %w", v, err)
		}
		if n <= 0 {
			return nil, fmt.Errorf("MAX_UPLOAD_BYTES: must be positive, got %q", v)
		}
		c.MaxUploadBytes = n
	}
	if v := strings.TrimSpace(getenv("OPENROUTER_CONCURRENCY")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("OPENROUTER_CONCURRENCY: %q is not a valid integer: %w", v, err)
		}
		if n <= 0 {
			return nil, fmt.Errorf("OPENROUTER_CONCURRENCY: must be positive, got %q", v)
		}
		c.OpenRouterConcurrency = n
	}

	// --- unusable value: storage dir must exist and be writable ---
	if err := checkWritableDir(c.ImageStorageDir); err != nil {
		return nil, fmt.Errorf("IMAGE_STORAGE_DIR: %w", err)
	}

	return c, nil
}

// checkWritableDir returns an error if path does not exist, is not a directory,
// or cannot be written to.
func checkWritableDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory %q does not exist", path)
		}
		return fmt.Errorf("cannot stat directory %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", path)
	}
	probe := filepath.Join(path, ".imagen-write-probe")
	f, err := os.Create(probe)
	if err != nil {
		return fmt.Errorf("directory %q is not writable: %w", path, err)
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return nil
}
