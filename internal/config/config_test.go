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
	t.Setenv("HAAKU_DEV_FRONTEND_URL", "")
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

// TestLoadDevFrontendURL guards the redirect target's shape: the handler appends
// the request path and query to it, so the configured value must be an origin.
// A path, query or fragment would produce a doubled or malformed redirect.
func TestLoadDevFrontendURL(t *testing.T) {
	t.Run("origin is accepted", func(t *testing.T) {
		isolate(t)
		t.Setenv("TMDB_API_KEY", "k")
		t.Setenv("HAAKU_DEV_FRONTEND_URL", "http://localhost:5173")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.DevFrontendURL != "http://localhost:5173" {
			t.Fatalf("DevFrontendURL = %q, want the origin", cfg.DevFrontendURL)
		}
	})

	// A trailing slash is trimmed, so "http://host/" stays an origin.
	t.Run("trailing slash is trimmed", func(t *testing.T) {
		isolate(t)
		t.Setenv("TMDB_API_KEY", "k")
		t.Setenv("HAAKU_DEV_FRONTEND_URL", "http://localhost:5173/")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.DevFrontendURL != "http://localhost:5173" {
			t.Fatalf("DevFrontendURL = %q, want the trimmed origin", cfg.DevFrontendURL)
		}
	})

	for _, value := range []string{
		"http://localhost:5173/app", // path
		"http://localhost:5173?x=1", // query
		"http://localhost:5173#top", // fragment
		"http://localhost:5173?",    // empty query delimiter
		"http://localhost:5173#",    // empty fragment delimiter
		"localhost:5173",            // no scheme
		"ftp://localhost:5173",      // wrong scheme
	} {
		t.Run("rejected "+value, func(t *testing.T) {
			isolate(t)
			t.Setenv("TMDB_API_KEY", "k")
			t.Setenv("HAAKU_DEV_FRONTEND_URL", value)

			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted %q, want a rejection", value)
			}
		})
	}
}
