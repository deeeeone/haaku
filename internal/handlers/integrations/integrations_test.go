package integrations

import (
	"strings"
	"testing"
	"time"

	"trakt-api/internal/database"
)

func TestValidScopesAllowlist(t *testing.T) {
	allowed := []string{
		"profile", "history:read", "history:write", "watchlist:read",
		"watchlist:write", "ratings:read", "ratings:write", "lists:read",
		"lists:write", "social:read", "social:write", "webhooks:manage",
		"offline_access",
	}
	for _, s := range allowed {
		if !validScopes[s] {
			t.Errorf("validScopes missing expected scope %q", s)
		}
	}
	// The wildcard and unknown scopes must never be accepted.
	for _, s := range []string{"*", "admin", "", "history"} {
		if validScopes[s] {
			t.Errorf("validScopes unexpectedly accepts %q", s)
		}
	}
}

func TestDefaultScopesMinimal(t *testing.T) {
	if len(defaultScopes) != 1 || defaultScopes[0] != "profile" {
		t.Errorf("defaultScopes = %v, want [profile]", defaultScopes)
	}
}

func TestGenerateToken(t *testing.T) {
	tok, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	if !strings.HasPrefix(tok, "tk_") {
		t.Errorf("token missing 'tk_' prefix: %q", tok)
	}
	if len(tok) <= len("tk_") {
		t.Errorf("token too short: %q", tok)
	}
	// Tokens must be unique-ish (random 24 bytes).
	tok2, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	if tok == tok2 {
		t.Error("two generated tokens were identical — expected randomness")
	}
}

func TestToTokenResponse(t *testing.T) {
	created := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	tok := &database.IntegrationToken{
		ID:        1,
		UserID:    "u",
		Name:      "My App",
		Scopes:    database.StringSlice{"profile", "history:read"},
		Active:    true,
		CreatedAt: created,
	}
	got := toTokenResponse(tok)
	if got.ID != 1 || got.Name != "My App" || got.Active != true {
		t.Errorf("token response base wrong: %+v", got)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "profile" {
		t.Errorf("scopes = %v", got.Scopes)
	}
	if got.CreatedAt != "2026-04-01T09:00:00.000Z" {
		t.Errorf("CreatedAt = %q", got.CreatedAt)
	}
}
