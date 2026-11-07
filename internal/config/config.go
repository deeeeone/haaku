package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"

	"github.com/haaku-tv/haaku/internal/secret"
)

// Identity modes (issue #53). AuthModeLocal is the self-hosted default: this
// server issues identity (signup/login/sessions). AuthModeClerk delegates
// identity to Clerk and disables the local module outright — no local routes,
// no local session issuance. The two never both authenticate an instance.
const (
	AuthModeLocal = "local"
	AuthModeClerk = "clerk"

	// SignupApproval is the local sign-up policy default: the first account on
	// an empty instance becomes the owner, and every later sign-up lands
	// "pending" until an admin approves it. No open door on a box that happens
	// to be reachable; adding a household member is one click, not a config
	// change. See docs/architecture/local-auth.md.
	SignupApproval = "approval"
	SignupOpen     = "open"   // new accounts are active immediately
	SignupClosed   = "closed" // only the first-run bootstrap account may be created
)

// Config holds all runtime configuration, loaded once from env vars.
// Secrets live in .env (gitignored); never commit real values.
type Config struct {
	Env  string
	Port string

	// Identity (issue #53): which authenticator this instance runs, and the
	// local sign-up policy. AuthMode is HAAKU_AUTH_MODE (default local);
	// SignupPolicy is only consulted in local mode.
	AuthMode     string
	SignupPolicy string

	// Data layer (wired in T0.4/T0.8)
	DatabaseURL string
	RedisURL    string

	// External services
	TMDBAPIToken string // TMDB v4 read-access token; sent as Authorization Bearer to api.themoviedb.org/4
	ClerkBaseURL string // Clerk API base URL (clerk-emulator in dev)
	ClerkSecret  string // Clerk secret key (*** in dev)
	// ClerkWebhookSecret is the Svix signing secret for webhook payloads.
	// Separate from the API secret key. Falls back to ClerkSecret when unset
	// (dev convenience — clerk-emulator reuses one secret).
	ClerkWebhookSecret string

	// Admin/mod whitelists (pattern: theintrodb-api)
	AdminUserIDs []string
	ModUserIDs   []string

	// Rate limiting (Phase 9d)
	RateLimitEnabled bool
	RateLimitAuth    RateLimitConfig
	RateLimitPublic  RateLimitConfig

	// CORS
	CORSOrigins []string
}

// RateLimitConfig holds window + max for a rate limit tier.
type RateLimitConfig struct {
	WindowSeconds int
	Max           int
}

// Load reads configuration from the environment.
// In development, loads .env from the project root (silently ignores if missing).
func Load() (*Config, error) {
	// Load .env file if present — production uses real env vars.
	// Errors (missing file, etc.) are intentionally ignored.
	_ = godotenv.Load()

	cfg := &Config{
		Env:                getEnv("APP_ENV", "development"),
		Port:               getEnv("PORT", "8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		RedisURL:           getEnv("REDIS_URL", "redis://localhost:6379"),
		TMDBAPIToken:       os.Getenv("TMDB_API_TOKEN"),
		ClerkBaseURL:       getEnv("CLERK_API_BASE_URL", "http://localhost:4000"),
		ClerkSecret:        os.Getenv("CLERK_SECRET_KEY"),
		ClerkWebhookSecret: os.Getenv("CLERK_WEBHOOK_SECRET"),
		AdminUserIDs:       splitCSV(os.Getenv("ADMIN_USER_IDS")),
		ModUserIDs:         splitCSV(os.Getenv("MOD_USER_IDS")),

		AuthMode:     getEnv("HAAKU_AUTH_MODE", AuthModeLocal),
		SignupPolicy: getEnv("HAAKU_SIGNUP", SignupApproval),

		// Rate limiting (Phase 9d) — ON by default (primary abuse control);
		// set RATE_LIMIT_ENABLED=false for local dev without Redis.
		RateLimitEnabled: getEnvBool("RATE_LIMIT_ENABLED", true),
		RateLimitAuth: RateLimitConfig{
			WindowSeconds: getEnvInt("RATE_LIMIT_AUTH_WINDOW", 60),
			Max:           getEnvInt("RATE_LIMIT_AUTH_MAX", 100),
		},
		RateLimitPublic: RateLimitConfig{
			WindowSeconds: getEnvInt("RATE_LIMIT_PUBLIC_WINDOW", 60),
			Max:           getEnvInt("RATE_LIMIT_PUBLIC_MAX", 30),
		},

		CORSOrigins: splitCSV(os.Getenv("CORS_ORIGINS")),
	}

	if cfg.TMDBAPIToken == "" {
		return nil, fmt.Errorf("TMDB_API_TOKEN environment variable is required (TMDB v4 read token)")
	}

	// At-rest encryption root (issue #16 §2). The server must not run without a
	// usable key: stored secrets would be unreadable, and a short key would be
	// unsafe. Enforced here like TMDB_API_TOKEN so a misconfigured host fails
	// fast at startup instead of at the first credential write.
	if _, err := secret.RootKeyFromEnv(); err != nil {
		return nil, err
	}

	// Identity (issue #53). An unknown mode would leave the instance with no
	// authenticator at all; an unknown signup policy would silently pick one.
	// Both are refused at boot, like the root key, rather than guessed at.
	switch cfg.AuthMode {
	case AuthModeLocal, AuthModeClerk:
	default:
		return nil, fmt.Errorf("HAAKU_AUTH_MODE must be %q or %q, got %q",
			AuthModeLocal, AuthModeClerk, cfg.AuthMode)
	}
	switch cfg.SignupPolicy {
	case SignupApproval, SignupOpen, SignupClosed:
	default:
		return nil, fmt.Errorf("HAAKU_SIGNUP must be %q, %q or %q, got %q",
			SignupApproval, SignupOpen, SignupClosed, cfg.SignupPolicy)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		switch strings.ToLower(v) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no", "":
			return false
		}
	}
	return fallback
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
