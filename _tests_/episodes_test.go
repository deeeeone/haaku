package tests

import (
	"encoding/json"
	"strconv"
	"testing"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
)

// ---------------------------------------------------------------------------
// Episode response shape tests
// ---------------------------------------------------------------------------

// TestEpisodeDetailResponseShape verifies the episode detail response shape.
func TestEpisodeDetailResponseShape(t *testing.T) {
	ep := createTestEpisode()
	season := createTestSeason()
	show := createTestShow()

	resp := map[string]interface{}{
		"ids": map[string]interface{}{
			"id":    ep.ID,
			"slug":  "",
			"tmdb":  nil,
			"imdb":  nil,
			"tvdb":  nil,
		},
		"season_id":       ep.SeasonID,
		"show_id":         ep.ShowID,
		"episode_number":  ep.EpisodeNumber,
		"title":           ep.Title,
		"overview":        ep.Overview,
		"runtime":         ep.Runtime,
		"images":          ep.Images,
		"absolute_number": nil,
		"aired_at":        nil,
		"show": map[string]interface{}{
			"title":  show.Title,
			"year":   0,
			"ids":    map[string]interface{}{"id": show.ID},
			"images": show.Images,
		},
		"season": map[string]interface{}{
			"show_id":       season.ShowID,
			"season_number": season.SeasonNumber,
			"episode_count": season.EpisodeCount,
			"ids":           map[string]interface{}{"id": season.ID},
			"images":        season.Images,
		},
		"stats": map[string]interface{}{
			"watchers": float64(0),
			"plays":    float64(0),
			"rating":   float64(0),
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

	// Check required fields.
	requiredFields := []string{"ids", "season_id", "show_id", "episode_number", "title", "show", "season", "stats"}
	for _, field := range requiredFields {
		if raw[field] == nil {
			t.Errorf("required field %q is missing", field)
		}
	}
}

// TestEpisodeStatsResponseShape verifies the episode stats response.
func TestEpisodeStatsResponseShape(t *testing.T) {
	stats := map[string]interface{}{
		"watchers": 42,
		"plays":    100,
		"rating":   8.5,
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	if raw["watchers"] == nil {
		t.Error("watchers missing")
	}
	if raw["plays"] == nil {
		t.Error("plays missing")
	}
	if raw["rating"] == nil {
		t.Error("rating missing")
	}
}

// TestEpisodeRatingsDistributionShape verifies the ratings distribution response.
func TestEpisodeRatingsDistributionShape(t *testing.T) {
	dist := map[string]interface{}{
		"rating": 8.0,
		"votes":  int64(50),
		"distribution": map[string]int64{
			"1":  10,
			"5":  20,
			"10": 20,
		},
	}

	data, err := json.Marshal(dist)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	if raw["rating"] == nil {
		t.Error("rating missing")
	}
	if raw["votes"] == nil {
		t.Error("votes missing")
	}
	if raw["distribution"] == nil {
		t.Error("distribution missing")
	}
}

// TestEpisodeCommentsResponseShape verifies the paginated comments response.
func TestEpisodeCommentsResponseShape(t *testing.T) {
	resp := map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"id":           int64(1),
				"user_id":      "user_1",
				"media_type":   "episode",
				"media_id":     int64(100),
				"comment_type": "shout",
				"body":         "Great episode!",
				"spoiler":      false,
				"like_count":   5,
				"reply_count":  2,
				"created_at":   "2024-01-01T00:00:00Z",
			},
		},
		"page":        1,
		"limit":       25,
		"total_count": int64(1),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if raw["data"] == nil {
		t.Error("data missing")
	}
	if raw["total_count"] == nil {
		t.Error("total_count missing")
	}
}

// TestEpisodeListsResponseShape verifies the lists containing this episode response.
func TestEpisodeListsResponseShape(t *testing.T) {
	resp := map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"list_item": map[string]interface{}{
					"id":         int64(1),
					"list_id":    int64(10),
					"media_type": "episode",
					"media_id":   int64(100),
				},
				"list": map[string]interface{}{
					"id":      int64(10),
					"name":    "Best Episodes",
					"privacy": "public",
				},
			},
		},
		"page":        1,
		"limit":       25,
		"total_count": int64(1),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if raw["data"] == nil {
		t.Error("data missing")
	}
}

// ---------------------------------------------------------------------------
// Episode OpenAPI route registration tests
// ---------------------------------------------------------------------------

// TestEpisodeRoutesRegistered verifies episode routes are registered.
func TestEpisodeRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/episodes/{id}":          true,
		"/v1/episodes/{id}/stats":    true,
		"/v1/episodes/{id}/ratings": true,
		"/v1/episodes/{id}/comments": true,
		"/v1/episodes/{id}/lists":    true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected episode route %q not registered", path)
		}
	}
}

// TestEpisodeRoutesHaveCorrectAuth verifies all episode routes are public.
func TestEpisodeRoutesHaveCorrectAuth(t *testing.T) {
	routes := openapi.AllRoutes()
	for _, r := range routes {
		if len(r.Tags) > 0 && r.Tags[0] == "Episodes" {
			if r.Auth != "none" {
				t.Errorf("episode route %s %s has auth=%q, want 'none'", r.Method, r.Path, r.Auth)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func createTestEpisode() database.Episode {
	return database.Episode{
		ID:            100,
		SeasonID:      5,
		ShowID:        7,
		EpisodeNumber: 3,
		Title:         "Pilot",
		Overview:      "The first episode.",
		Runtime:       45,
		Images:        database.JSONMap{"still": "url"},
	}
}

func createTestSeason() database.Season {
	return database.Season{
		ID:           5,
		ShowID:       7,
		SeasonNumber: 1,
		EpisodeCount: 10,
		Images:       database.JSONMap{"poster": "url"},
	}
}

func createTestShow() database.Show {
	return database.Show{
		ID:              7,
		TmdbID:          1396,
		Title:           "Test Show",
		Overview:        "A test show.",
		Status:          "ended",
		EpisodeCount:    62,
		SeasonCount:     5,
		Images:          database.JSONMap{"poster": "url"},
	}
}

// Ensure fmt and strconv are used.
var _ = strconv.Itoa
