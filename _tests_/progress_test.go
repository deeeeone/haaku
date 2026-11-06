package tests

import (
	"encoding/json"
	"testing"

	"trakt-api/internal/handlers/media"
	_ "trakt-api/internal/handlers/progress"
	"trakt-api/internal/openapi"
)

// TestPlaybackOpenAPIRoutesRegistered verifies playback progress routes are registered.
func TestPlaybackOpenAPIRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]string{
		"/v1/sync/playback/movies":   "required",
		"/v1/sync/playback/episodes": "required",
	}

	found := make(map[string]string)
	for _, r := range routes {
		if expectedAuth, exists := expectedPaths[r.Path]; exists {
			found[r.Path] = r.Auth
			if r.Auth != expectedAuth {
				t.Errorf("playback route %s %s has auth=%q, want %q", r.Method, r.Path, r.Auth, expectedAuth)
			}
		}
	}

	for path := range expectedPaths {
		if _, ok := found[path]; !ok {
			t.Errorf("expected playback route %q not registered", path)
		}
	}
}

// TestMoviePlaybackResponseShape verifies the movie playback JSON shape.
func TestMoviePlaybackResponseShape(t *testing.T) {
	resp := struct {
		Progress float64             `json:"progress"`
		PausedAt string              `json:"paused_at"`
		Movie    media.MovieResponse `json:"movie"`
	}{
		Progress: 0.75,
		PausedAt: "2026-08-01T12:00:00.000Z",
		Movie: media.MovieResponse{
			IDs:    media.IDs{ID: 1, TMDB: 100},
			Title:  "Test Movie",
			Year:   2026,
			Images: map[string]any{},
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

	if raw["progress"] != float64(0.75) {
		t.Errorf("progress = %v, want 0.75", raw["progress"])
	}
	if raw["paused_at"] != "2026-08-01T12:00:00.000Z" {
		t.Errorf("paused_at = %v", raw["paused_at"])
	}

	movie, ok := raw["movie"].(map[string]interface{})
	if !ok {
		t.Fatal("movie field missing")
	}
	if movie["title"] != "Test Movie" {
		t.Errorf("movie.title = %v, want Test Movie", movie["title"])
	}
}

// TestEpisodePlaybackResponseShape verifies the episode playback JSON shape.
func TestEpisodePlaybackResponseShape(t *testing.T) {
	resp := struct {
		Progress float64 `json:"progress"`
		PausedAt string  `json:"paused_at"`
		Episode  struct {
			Title  string `json:"title"`
			Season int    `json:"season"`
			Number int    `json:"number"`
		} `json:"episode"`
		Show struct {
			Title string    `json:"title"`
			IDs   media.IDs `json:"ids"`
		} `json:"show"`
	}{
		Progress: 0.5,
		PausedAt: "2026-08-02T10:00:00.000Z",
	}
	resp.Episode.Title = "Pilot"
	resp.Episode.Season = 1
	resp.Episode.Number = 1
	resp.Show.Title = "Test Show"
	resp.Show.IDs = media.IDs{ID: 1, TMDB: 200}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	episode, ok := raw["episode"].(map[string]interface{})
	if !ok {
		t.Fatal("episode field missing")
	}
	if episode["title"] != "Pilot" {
		t.Errorf("episode.title = %v, want Pilot", episode["title"])
	}
	if episode["season"] != float64(1) {
		t.Errorf("episode.season = %v, want 1", episode["season"])
	}
	if episode["number"] != float64(1) {
		t.Errorf("episode.number = %v, want 1", episode["number"])
	}

	show, ok := raw["show"].(map[string]interface{})
	if !ok {
		t.Fatal("show field missing")
	}
	if show["title"] != "Test Show" {
		t.Errorf("show.title = %v, want Test Show", show["title"])
	}
}
