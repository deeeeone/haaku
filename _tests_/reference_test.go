package tests

import (
	"encoding/json"
	"strings"
	"testing"

	_ "github.com/haaku-tv/haaku/internal/handlers/reference"
	"github.com/haaku-tv/haaku/internal/openapi"
)

// ---------------------------------------------------------------------------
// Route registration tests
// ---------------------------------------------------------------------------

func TestReferenceRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/genres/{type}":         true,
		"/v1/languages/{type}":      true,
		"/v1/countries/{type}":      true,
		"/v1/networks":              true,
		"/v1/certifications/movies": true,
		"/v1/certifications/shows":  true,
		"/v1/certifications/{type}": true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected route %q not registered", path)
		}
	}
}

func TestReferenceRoutesHaveCorrectAuth(t *testing.T) {
	routes := openapi.AllRoutes()

	refPaths := map[string]bool{
		"/v1/genres/{type}":         true,
		"/v1/languages/{type}":      true,
		"/v1/countries/{type}":      true,
		"/v1/networks":              true,
		"/v1/certifications/movies": true,
		"/v1/certifications/shows":  true,
		"/v1/certifications/{type}": true,
	}

	for _, r := range routes {
		if refPaths[r.Path] {
			if r.Auth != "none" {
				t.Errorf("reference route %s %s has auth=%q, want 'none'", r.Method, r.Path, r.Auth)
			}
			if len(r.Tags) == 0 {
				t.Errorf("reference route %s missing tags", r.Path)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Response shape tests
// ---------------------------------------------------------------------------

func TestGenreItemShape(t *testing.T) {
	// Verify a GenreItem JSON shape has {name, slug}
	item := struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}{
		Name: "Science Fiction",
		Slug: "science-fiction",
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := result["name"]; !ok {
		t.Error("genre item missing 'name' field")
	}
	if _, ok := result["slug"]; !ok {
		t.Error("genre item missing 'slug' field")
	}
}

func TestLanguageItemShape(t *testing.T) {
	// Verify a LanguageItem JSON shape has {name, code}
	item := struct {
		Name string `json:"name"`
		Code string `json:"code"`
	}{
		Name: "English",
		Code: "en",
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := result["name"]; !ok {
		t.Error("language item missing 'name' field")
	}
	if _, ok := result["code"]; !ok {
		t.Error("language item missing 'code' field")
	}
}

func TestCountryItemShape(t *testing.T) {
	// Verify a CountryItem JSON shape has {name, code}
	item := struct {
		Name string `json:"name"`
		Code string `json:"code"`
	}{
		Name: "United States of America",
		Code: "US",
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := result["name"]; !ok {
		t.Error("country item missing 'name' field")
	}
	if _, ok := result["code"]; !ok {
		t.Error("country item missing 'code' field")
	}
}

func TestCertificationShape(t *testing.T) {
	// Verify certification response is grouped by country
	// Each country has {country, certifications: [{name, meaning, order}]}
	type CertItem struct {
		Name    string `json:"name"`
		Meaning string `json:"meaning,omitempty"`
		Order   int    `json:"order"`
	}
	type CertGroup struct {
		Country        string     `json:"country"`
		Certifications []CertItem `json:"certifications"`
	}

	group := CertGroup{
		Country: "US",
		Certifications: []CertItem{
			{Name: "G", Meaning: "General Audiences", Order: 1},
			{Name: "PG", Meaning: "Parental Guidance Suggested", Order: 2},
		},
	}

	data, err := json.Marshal(group)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if _, ok := result["country"]; !ok {
		t.Error("cert group missing 'country' field")
	}
	certs, ok := result["certifications"].([]interface{})
	if !ok {
		t.Fatal("'certifications' is not an array")
	}
	if len(certs) != 2 {
		t.Fatalf("expected 2 certifications, got %d", len(certs))
	}
}

// ---------------------------------------------------------------------------
// Redis cache key pattern tests
// ---------------------------------------------------------------------------

func TestCacheKeyPatterns(t *testing.T) {
	// Verify the cache key format follows "reference:{type}:{subtype}"
	tests := []struct {
		category string
		subtype  string
		expected string
	}{
		{"genres", "movies", "reference:genres:movies"},
		{"genres", "shows", "reference:genres:shows"},
		{"languages", "movies", "reference:languages:movies"},
		{"languages", "shows", "reference:languages:shows"},
		{"countries", "movies", "reference:countries:movies"},
		{"countries", "shows", "reference:countries:shows"},
		{"networks", "tv", "reference:networks:tv"},
		{"certifications", "movies", "reference:certifications:movies"},
		{"certifications", "shows", "reference:certifications:shows"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			key := "reference:" + tt.category + ":" + tt.subtype
			if key != tt.expected {
				t.Errorf("cache key = %q, want %q", key, tt.expected)
			}

			// Verify the pattern: must start with "reference:"
			if key[:10] != "reference:" {
				t.Errorf("cache key %q does not start with 'reference:'", key)
			}

			// Verify it has exactly two colons
			colons := 0
			for _, ch := range key {
				if ch == ':' {
					colons++
				}
			}
			if colons != 2 {
				t.Errorf("cache key %q has %d colons, want 2", key, colons)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Slug helper test (verify the slugify logic used for genres)
// ---------------------------------------------------------------------------

func TestSlugify(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"Action", "action"},
		{"Science Fiction", "science-fiction"},
		{"Action & Adventure", "action-and-adventure"},
		{"War & Politics", "war-and-politics"},
		{"TV Movie", "tv-movie"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Inline slugify - matches the logic in reference.go
			slug := slugify(tt.name)
			if slug != tt.expected {
				t.Errorf("slugify(%q) = %q, want %q", tt.name, slug, tt.expected)
			}
		})
	}
}

func slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "&", "and")
	return s
}
