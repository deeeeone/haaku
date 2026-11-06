package tests

import (
	"encoding/json"
	"testing"

	_ "trakt-api/internal/handlers/watch"
	"trakt-api/internal/openapi"
)

// ---------------------------------------------------------------------------
// Watch providers route registration tests
// ---------------------------------------------------------------------------

// TestWatchRoutesRegistered verifies watch routes are in the OpenAPI registry.
func TestWatchRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/movies/{id}/watch": true,
		"/v1/shows/{id}/watch":  true,
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

// TestWatchRoutesHaveCorrectAuth verifies watch routes are public.
func TestWatchRoutesHaveCorrectAuth(t *testing.T) {
	routes := openapi.AllRoutes()

	for _, r := range routes {
		if r.Path == "/v1/movies/{id}/watch" || r.Path == "/v1/shows/{id}/watch" {
			if r.Auth != "none" {
				t.Errorf("watch route %s %s has auth=%q, want 'none'", r.Method, r.Path, r.Auth)
			}
			if len(r.Tags) == 0 {
				t.Errorf("watch route %s missing tags", r.Path)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Country validation tests
// ---------------------------------------------------------------------------

func TestIsValidCountryCode(t *testing.T) {
	tests := []struct {
		code  string
		valid bool
	}{
		{"us", true},
		{"US", true},
		{"gb", true},
		{"de", true},
		{"fr", true},
		{"jp", true},
		{"", false},
		{"u", false},
		{"usa", false},
		{"us1", false},
		{"us-", false},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			result := isValidCountryCode(tt.code)
			if result != tt.valid {
				t.Errorf("isValidCountryCode(%q) = %v, want %v", tt.code, result, tt.valid)
			}
		})
	}
}

func isValidCountryCode(code string) bool {
	return len(code) == 2
}

// ---------------------------------------------------------------------------
// Watch providers response shape tests
// ---------------------------------------------------------------------------

// WatchProvidersResponse matches the API response shape from /movies/:id/watch
// and /shows/:id/watch endpoints.
type WatchProvidersResponse struct {
	Link     string            `json:"link,omitempty"`
	Flatrate []ProviderSummary `json:"flatrate,omitempty"`
	Rent     []ProviderSummary `json:"rent,omitempty"`
	Buy      []ProviderSummary `json:"buy,omitempty"`
}

type ProviderSummary struct {
	ProviderID int    `json:"provider_id"`
	Name       string `json:"name"`
	LogoPath   string `json:"logo_path"`
}

func TestWatchProvidersResponseShape(t *testing.T) {
	resp := WatchProvidersResponse{
		Link: "https://www.themoviedb.org/movie/12345/watch",
		Flatrate: []ProviderSummary{
			{ProviderID: 8, Name: "Netflix", LogoPath: "/t2yyOv40HZeVlLjYsCsPHnWLk4W.jpg"},
			{ProviderID: 9, Name: "Amazon Prime Video", LogoPath: "/68MNrwlkpF7WnmNPjahKNhAxQpj.jpg"},
		},
		Rent: []ProviderSummary{
			{ProviderID: 3, Name: "Google Play Movies", LogoPath: "/8zVTObSWXOmPZAoE2nqMY8E9TV2.jpg"},
		},
		Buy: []ProviderSummary{
			{ProviderID: 3, Name: "Google Play Movies", LogoPath: "/8zVTObSWXOmPZAoE2nqMY8E9TV2.jpg"},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal WatchProvidersResponse: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("marshaled JSON is empty")
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["link"] == nil {
		t.Error("link field missing")
	}
	if raw["flatrate"] == nil {
		t.Error("flatrate field missing")
	}
	if raw["rent"] == nil {
		t.Error("rent field missing")
	}
	if raw["buy"] == nil {
		t.Error("buy field missing")
	}

	// Verify flatrate array structure
	flatrateArr, ok := raw["flatrate"].([]interface{})
	if !ok {
		t.Fatal("flatrate is not an array")
	}
	if len(flatrateArr) != 2 {
		t.Errorf("expected 2 flatrate providers, got %d", len(flatrateArr))
	}
	first, ok := flatrateArr[0].(map[string]interface{})
	if !ok {
		t.Fatal("first flatrate item is not an object")
	}
	if first["provider_id"] == nil {
		t.Error("provider_id missing in first item")
	}
	if first["name"] == nil {
		t.Error("name missing in first item")
	}
	if first["logo_path"] == nil {
		t.Error("logo_path missing in first item")
	}
}

// TestWatchProvidersEmptyResponse verifies empty watch providers marshal correctly.
func TestWatchProvidersEmptyResponse(t *testing.T) {
	resp := WatchProvidersResponse{}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal empty response: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if raw["flatrate"] != nil {
		t.Error("flatrate should be null/absent when empty")
	}
}

// ---------------------------------------------------------------------------
// TMDB watch providers type tests
// ---------------------------------------------------------------------------

// TestCountryProvidersShape verifies the TMDB v3 response shape can be parsed.
func TestCountryProvidersShape(t *testing.T) {
	// Simulate the TMDB v3 /movie/{id}/watch/providers response.
	tmdbJSON := `{
		"id": 12345,
		"results": {
			"US": {
				"link": "https://www.themoviedb.org/movie/12345/watch?locale=US",
				"flatrate": [
					{"logo_path": "/t2yyOv40HZeVlLjYsCsPHnWLk4W.jpg", "provider_id": 8, "provider_name": "Netflix", "display_priority": 0}
				],
				"rent": [
					{"logo_path": "/8zVTObSWXOmPZAoE2nqMY8E9TV2.jpg", "provider_id": 3, "provider_name": "Google Play Movies", "display_priority": 4}
				],
				"buy": [
					{"logo_path": "/8zVTObSWXOmPZAoE2nqMY8E9TV2.jpg", "provider_id": 3, "provider_name": "Google Play Movies", "display_priority": 4}
				]
			}
		}
	}`

	// Verify the TMDB types parse correctly
	type TMDBProvider struct {
		ProviderID   int    `json:"provider_id"`
		ProviderName string `json:"provider_name"`
		LogoPath     string `json:"logo_path"`
	}

	type TMDBCountryProviders struct {
		Link     string         `json:"link"`
		Flatrate []TMDBProvider `json:"flatrate"`
		Rent     []TMDBProvider `json:"rent"`
		Buy      []TMDBProvider `json:"buy"`
	}

	type TMDBWatchProviders struct {
		ID      int                             `json:"id"`
		Results map[string]TMDBCountryProviders `json:"results"`
	}

	var parsed TMDBWatchProviders
	if err := json.Unmarshal([]byte(tmdbJSON), &parsed); err != nil {
		t.Fatalf("failed to unmarshal TMDB response: %v", err)
	}

	if parsed.ID != 12345 {
		t.Errorf("ID = %d, want 12345", parsed.ID)
	}

	usProviders, ok := parsed.Results["US"]
	if !ok {
		t.Fatal("US results missing")
	}

	if len(usProviders.Flatrate) != 1 {
		t.Errorf("expected 1 flatrate, got %d", len(usProviders.Flatrate))
	}
	if len(usProviders.Rent) != 1 {
		t.Errorf("expected 1 rent, got %d", len(usProviders.Rent))
	}
	if len(usProviders.Buy) != 1 {
		t.Errorf("expected 1 buy, got %d", len(usProviders.Buy))
	}

	netflix := usProviders.Flatrate[0]
	if netflix.ProviderID != 8 {
		t.Errorf("Netflix provider_id = %d, want 8", netflix.ProviderID)
	}
	if netflix.ProviderName != "Netflix" {
		t.Errorf("Netflix name = %q, want 'Netflix'", netflix.ProviderName)
	}
}
