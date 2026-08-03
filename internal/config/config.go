package config

import (
	"fmt"
	"os"
)

// Config holds all runtime configuration, loaded once from env vars.
// Secrets live in .env (gitignored); never commit real values.
type Config struct {
	Env  string
	Port string

	// Data layer (wired in T0.4/T0.8)
	DatabaseURL string
	RedisURL    string

	// External services
	TMDBAPIToken string // TMDB v4 read-access token; sent as Authorization Bearer to api.themoviedb.org/4
	ClerkBaseURL string // Clerk API base URL (clerk-emulator in dev)
	ClerkSecret  string // Clerk secret key (sk_test_emulate in dev)

	// Admin/mod whitelists (pattern: theintrodb-api)
	AdminUserIDs []string
	ModUserIDs   []string
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	cfg := &Config{
		Env:          getEnv("APP_ENV", "development"),
		Port:         getEnv("PORT", "8080"),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		RedisURL:     getEnv("REDIS_URL", "redis://localhost:6379"),
		TMDBAPIToken: os.Getenv("TMDB_API_TOKEN"),
		ClerkBaseURL: getEnv("CLERK_API_BASE_URL", "http://localhost:4000"),
		ClerkSecret:  os.Getenv("CLERK_SECRET_KEY"),
		AdminUserIDs: splitCSV(os.Getenv("ADMIN_USER_IDS")),
		ModUserIDs:   splitCSV(os.Getenv("MOD_USER_IDS")),
	}

	if cfg.TMDBAPIToken == "" {
		return nil, fmt.Errorf("TMDB_API_TOKEN environment variable is required (TMDB v4 read token)")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
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
