package config

import (
	"strings"
	"testing"
)

// isolate pins the variables Load reads to the values a test wants. Setting a
// variable to "" still counts as present for godotenv, so the repo's .env
// cannot leak a value into the test.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HAAKU_SECRET_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("TVDB_API_KEY", "")
	t.Setenv("TVDB_PIN", "")
	t.Setenv("TMDB_API_KEY", "")
}

func TestLoadTMDBKey(t *testing.T) {
	t.Run("TMDB_API_KEY alone is enough", func(t *testing.T) {
		isolate(t)
		t.Setenv("TMDB_API_KEY", "canonical-key")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.TMDBAPIToken != "canonical-key" {
			t.Fatalf("TMDBAPIToken = %q, want the key", cfg.TMDBAPIToken)
		}
		if cfg.TVDBAPIKey != "" {
			t.Fatalf("TVDBAPIKey = %q, want empty (TVDB is optional)", cfg.TVDBAPIKey)
		}
	})

	t.Run("missing", func(t *testing.T) {
		isolate(t)

		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "TMDB_API_KEY") {
			t.Fatalf("Load error = %v, want a TMDB_API_KEY requirement", err)
		}
	})
}
