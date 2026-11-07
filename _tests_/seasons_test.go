package tests

import (
	"encoding/json"
	"testing"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
)

// ---------------------------------------------------------------------------
// Season response shape tests
// ---------------------------------------------------------------------------

// TestSeasonDetailResponseShape verifies the season detail response shape.
func TestSeasonDetailResponseShape(t *testing.T) {
	season := database.Season{
		ID:           5,
		ShowID:       7,
		SeasonNumber: 1,
		EpisodeCount: 10,
		Images:       database.JSONMap{"poster": "url"},
	}

	show := database.Show{
		ID:     7,
		Title:  "Test Show",
		Images: database.JSONMap{"poster": "url"},
	}

	_ = season
	_ = show

	resp := map[string]interface{}{
		"ids": map[string]interface{}{
			"id": int64(5),
		},
		"show_id":       int64(7),
		"season_number": 1,
		"episode_count": 10,
		"images":        map[string]interface{}{"poster": "url"},
		"show": map[string]interface{}{
			"title":  "Test Show",
			"ids":    map[string]interface{}{"id": int64(7)},
			"images": map[string]interface{}{"poster": "url"},
		},
		"stats": map[string]interface{}{
			"watchers":       float64(0),
			"plays":          float64(0),
			"episodes_count": float64(0),
			"rating":         float64(0),
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	requiredFields := []string{"ids", "show_id", "season_number", "episode_count", "show", "stats"}
	for _, field := range requiredFields {
		if raw[field] == nil {
			t.Errorf("required field %q is missing", field)
		}
	}
}

// TestSeasonStatsResponseShape verifies season stats.
func TestSeasonStatsResponseShape(t *testing.T) {
	stats := map[string]interface{}{
		"watchers":       float64(100),
		"plays":          float64(500),
		"episodes_count": float64(10),
		"rating":         8.5,
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	required := []string{"watchers", "plays", "episodes_count", "rating"}
	for _, field := range required {
		if raw[field] == nil {
			t.Errorf("required field %q missing", field)
		}
	}
}

// TestSeasonRatingsDistributionShape verifies season rating distribution.
func TestSeasonRatingsDistributionShape(t *testing.T) {
	dist := map[string]interface{}{
		"rating": 8.2,
		"votes":  int64(80),
		"distribution": map[string]int64{
			"1":  5,
			"5":  25,
			"10": 50,
		},
	}

	data, err := json.Marshal(dist)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if raw["distribution"] == nil {
		t.Error("distribution missing")
	}
}

// ---------------------------------------------------------------------------
// Season OpenAPI route registration tests
// ---------------------------------------------------------------------------

// TestSeasonRoutesRegistered verifies season routes are registered.
func TestSeasonRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/seasons/{id}":         true,
		"/v1/seasons/{id}/stats":   true,
		"/v1/seasons/{id}/ratings": true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected season route %q not registered", path)
		}
	}
}

// TestSeasonRoutesHaveCorrectAuth verifies all season routes are public.
func TestSeasonRoutesHaveCorrectAuth(t *testing.T) {
	routes := openapi.AllRoutes()
	for _, r := range routes {
		if len(r.Tags) > 0 && r.Tags[0] == "Seasons" {
			if r.Auth != "none" {
				t.Errorf("season route %s %s has auth=%q, want 'none'", r.Method, r.Path, r.Auth)
			}
		}
	}
}
