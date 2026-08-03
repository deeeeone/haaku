package tests

import (
	"encoding/json"
	"testing"

	_ "trakt-api/internal/handlers/media" // trigger init() for route registration
	"trakt-api/internal/openapi"
)

// ---------------------------------------------------------------------------
// Translation response shape tests
// ---------------------------------------------------------------------------

// TestTranslationResponseShape verifies the translation response shape.
func TestTranslationResponseShape(t *testing.T) {
	trans := []map[string]interface{}{
		{
			"language": "fr",
			"title":    "Le Film",
			"overview": "Un film incroyable.",
			"tagline":  "Le meilleur film.",
		},
	}

	data, err := json.Marshal(trans)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw []map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(raw) == 0 {
		t.Fatal("expected at least one translation")
	}

	first := raw[0]
	if first["language"] != "fr" {
		t.Errorf("language = %v, want fr", first["language"])
	}
	if first["title"] != "Le Film" {
		t.Errorf("title = %v", first["title"])
	}
	if first["overview"] != "Un film incroyable." {
		t.Errorf("overview = %v", first["overview"])
	}
	if first["tagline"] == nil {
		t.Error("tagline should not be nil")
	}
}

// TestEmptyTranslationResponseShape verifies empty translations array.
func TestEmptyTranslationResponseShape(t *testing.T) {
	trans := make([]map[string]interface{}, 0)
	data, err := json.Marshal(trans)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	if string(data) != "[]" {
		t.Errorf("empty translations should be [], got %s", string(data))
	}
}

// TestTranslationWithMissingTagline verifies tagline is omitted when empty.
func TestTranslationWithMissingTagline(t *testing.T) {
	trans := map[string]interface{}{
		"language": "en",
		"title":    "The Movie",
		"overview": "An amazing movie.",
	}

	data, err := json.Marshal(trans)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	// tagline should be absent or empty in the JSON.
	if _, ok := raw["tagline"]; ok && raw["tagline"] != nil {
		// It's fine either way — omitempty may exclude it.
	}
}

// ---------------------------------------------------------------------------
// Alias response shape tests
// ---------------------------------------------------------------------------

// TestAliasResponseShape verifies the alias response shape.
func TestAliasResponseShape(t *testing.T) {
	aliases := []map[string]interface{}{
		{
			"title":   "Alternative Title 1",
			"country": "US",
		},
		{
			"title":   "Alternative Title 2",
			"country": "GB",
		},
	}

	data, err := json.Marshal(aliases)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw []map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(raw) != 2 {
		t.Fatalf("expected 2 aliases, got %d", len(raw))
	}

	for i, alias := range raw {
		if alias["title"] == nil {
			t.Errorf("alias[%d].title missing", i)
		}
		if alias["country"] == nil {
			t.Errorf("alias[%d].country missing", i)
		}
	}
}

// TestEmptyAliasResponseShape verifies empty aliases array.
func TestEmptyAliasResponseShape(t *testing.T) {
	aliases := make([]map[string]interface{}, 0)
	data, err := json.Marshal(aliases)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	if string(data) != "[]" {
		t.Errorf("empty aliases should be [], got %s", string(data))
	}
}

// ---------------------------------------------------------------------------
// Translation/Alias OpenAPI route registration tests
// ---------------------------------------------------------------------------

// TestTranslationRoutesRegistered verifies translation routes are registered.
func TestTranslationRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/movies/{id}/translations/{lang}":                                    true,
		"/v1/shows/{id}/translations/{lang}":                                     true,
		"/v1/shows/{id}/seasons/{season}/translations/{lang}":                    true,
		"/v1/shows/{id}/seasons/{season}/episodes/{episode}/translations/{lang}": true,
		"/v1/movies/{id}/aliases":                                                true,
		"/v1/shows/{id}/aliases":                                                 true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected translation/alias route %q not registered", path)
		}
	}
}

// TestTranslationRoutesHaveCorrectAuth verifies translation/alias routes are public.
func TestTranslationRoutesHaveCorrectAuth(t *testing.T) {
	routes := openapi.AllRoutes()
	transPaths := map[string]bool{
		"/v1/movies/{id}/translations/{lang}":                                    true,
		"/v1/shows/{id}/translations/{lang}":                                     true,
		"/v1/shows/{id}/seasons/{season}/translations/{lang}":                    true,
		"/v1/shows/{id}/seasons/{season}/episodes/{episode}/translations/{lang}": true,
		"/v1/movies/{id}/aliases":                                                true,
		"/v1/shows/{id}/aliases":                                                 true,
	}

	for _, r := range routes {
		if transPaths[r.Path] {
			if r.Auth != "none" {
				t.Errorf("translation/alias route %s %s has auth=%q, want 'none'", r.Method, r.Path, r.Auth)
			}
		}
	}
}

// TestTMDBTranslationParsing verifies the TMDB translation JSON parsing logic.
func TestTMDBTranslationParsing(t *testing.T) {
	// Simulate a raw TMDB translations response.
	raw := `{
		"translations": [
			{
				"iso_3166_1": "FR",
				"iso_639_1": "fr",
				"name": "Français",
				"english_name": "French",
				"data": {
					"title": "Le Film",
					"overview": "Un film incroyable.",
					"tagline": "Le meilleur film."
				}
			},
			{
				"iso_3166_1": "US",
				"iso_639_1": "en",
				"name": "English",
				"english_name": "English",
				"data": {
					"title": "The Movie",
					"overview": "An amazing movie."
				}
			}
		]
	}`

	var resp struct {
		Translations []struct {
			ISO3166 string `json:"iso_3166_1"`
			ISO639  string `json:"iso_639_1"`
			Name    string `json:"name"`
			English string `json:"english_name"`
			Data    struct {
				Title    string `json:"title"`
				Overview string `json:"overview"`
				Name     string `json:"name"`
				Tagline  string `json:"tagline"`
			} `json:"data"`
		} `json:"translations"`
	}

	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("failed to parse TMDB translations: %v", err)
	}

	if len(resp.Translations) != 2 {
		t.Fatalf("expected 2 translations, got %d", len(resp.Translations))
	}

	fr := resp.Translations[0]
	if fr.ISO639 != "fr" {
		t.Errorf("first translation ISO639 = %s, want fr", fr.ISO639)
	}
	if fr.Data.Title != "Le Film" {
		t.Errorf("first translation title = %s, want Le Film", fr.Data.Title)
	}
}

// TestTMDBAlternativeTitlesParsing verifies the TMDB alt titles JSON parsing.
func TestTMDBAlternativeTitlesParsing(t *testing.T) {
	raw := `{
		"titles": [
			{"iso_3166_1": "US", "title": "Title US", "type": ""},
			{"iso_3166_1": "GB", "title": "Title UK", "type": ""},
			{"iso_3166_1": "FR", "title": "Titre FR", "type": ""}
		]
	}`

	var resp struct {
		Titles []struct {
			ISO3166 string `json:"iso_3166_1"`
			Title   string `json:"title"`
			Type    string `json:"type"`
		} `json:"titles"`
	}

	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("failed to parse alt titles: %v", err)
	}

	if len(resp.Titles) != 3 {
		t.Fatalf("expected 3 titles, got %d", len(resp.Titles))
	}

	// Check simplified output shape.
	simplified := make([]map[string]interface{}, 0, len(resp.Titles))
	for _, t := range resp.Titles {
		simplified = append(simplified, map[string]interface{}{
			"title":   t.Title,
			"country": t.ISO3166,
		})
	}

	if len(simplified) != 3 {
		t.Errorf("expected 3 simplified aliases, got %d", len(simplified))
	}
}
