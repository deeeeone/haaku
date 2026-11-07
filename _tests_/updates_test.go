package tests

import (
	"encoding/json"
	"testing"

	"github.com/haaku-tv/haaku/internal/handlers/media"
	_ "github.com/haaku-tv/haaku/internal/handlers/updates"
	"github.com/haaku-tv/haaku/internal/openapi"
)

// TestUpdatesOpenAPIRoutesRegistered verifies updates routes can be registered.
func TestUpdatesOpenAPIRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/movies/updates/{start_date}": true,
		"/v1/shows/updates/{start_date}":  true,
		"/v1/people/updates/{start_date}": true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected updates route %q not registered", path)
		}
	}

	// Verify auth is "none" for updates routes.
	for _, r := range routes {
		if expectedPaths[r.Path] {
			if r.Auth != "none" {
				t.Errorf("updates route %s %s has auth=%q, want 'none'", r.Method, r.Path, r.Auth)
			}
		}
	}
}

// TestUpdatesMovieResponseShape verifies movie update responses marshal correctly.
func TestUpdatesMovieResponseShape(t *testing.T) {
	resp := media.MovieResponse{
		IDs:   media.IDs{ID: 42, TMDB: 13885, IMDB: "tt1234567"},
		Title: "Test Movie",
		Year:  2024,
		Images: map[string]any{
			"poster": "https://example.com/poster.jpg",
		},
		RatingSummary: &media.RatingSummary{
			Rating:       8.5,
			Votes:        100,
			Distribution: make(map[string]int64),
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["title"] != "Test Movie" {
		t.Errorf("title = %v", raw["title"])
	}
	if raw["year"] == nil {
		t.Error("year missing")
	}
	if raw["ids"] == nil {
		t.Error("ids missing")
	}
}

// TestUpdatesShowResponseShape verifies show update responses marshal correctly.
func TestUpdatesShowResponseShape(t *testing.T) {
	resp := media.ShowResponse{
		IDs:   media.IDs{ID: 7, TMDB: 1396},
		Title: "Test Show",
		Year:  2023,
		Images: map[string]any{
			"poster": "https://example.com/poster.jpg",
		},
		RatingSummary: &media.RatingSummary{
			Rating:       9.0,
			Votes:        200,
			Distribution: make(map[string]int64),
		},
		Seasons: make([]media.SeasonResponse, 0),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["title"] != "Test Show" {
		t.Errorf("title = %v", raw["title"])
	}
	if raw["seasons"] == nil {
		t.Error("seasons should not be null in ShowResponse")
	}
}

// TestUpdatesPersonResponseShape verifies person update responses marshal correctly.
func TestUpdatesPersonResponseShape(t *testing.T) {
	resp := media.PersonResponse{
		IDs:                media.IDs{ID: 1, TMDB: 17419},
		Name:               "Test Person",
		Gender:             2,
		KnownForDepartment: "Acting",
		Images:             map[string]any{"headshot": "url"},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["name"] != "Test Person" {
		t.Errorf("name = %v", raw["name"])
	}
	if raw["ids"] == nil {
		t.Error("ids missing")
	}
}

// TestUpdatesEmptyArray verifies empty updates response marshals to [].
func TestUpdatesEmptyArray(t *testing.T) {
	movies := make([]media.MovieResponse, 0)
	data, err := json.Marshal(movies)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("empty movies should be [], got %s", string(data))
	}

	shows := make([]media.ShowResponse, 0)
	data, err = json.Marshal(shows)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("empty shows should be [], got %s", string(data))
	}

	people := make([]media.PersonResponse, 0)
	data, err = json.Marshal(people)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("empty people should be [], got %s", string(data))
	}
}

// TestUpdatesDateParsing verifies YYYY-MM-DD validation for start_date.
func TestUpdatesDateParsing(t *testing.T) {
	valid := []string{"2024-01-01", "2026-12-31", "2023-06-15"}
	for _, d := range valid {
		if !isValidDateHelper(d) {
			t.Errorf("%q should be valid YYYY-MM-DD", d)
		}
	}

	invalid := []string{"not-a-date", "2024-13-01", "01-01-2024", "", "2024-1-1"}
	for _, d := range invalid {
		if isValidDateHelper(d) {
			t.Errorf("%q should be invalid", d)
		}
	}
}

// TestUpdatesResponseFieldTypes verifies no null fields in responses.
func TestUpdatesResponseFieldTypes(t *testing.T) {
	// Movie with min extended should not have overview
	movie := media.MovieResponse{
		IDs:    media.IDs{ID: 1},
		Title:  "Test",
		Year:   2024,
		Images: map[string]any{},
	}

	data, _ := json.Marshal(movie)
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	// Images should never be null
	if raw["images"] == nil {
		t.Error("images should not be null")
	}
	// With omitempty, overview is absent; if it is present it must be empty.
	if v, exists := raw["overview"]; exists && v != nil && v != "" {
		t.Errorf("overview = %#v, want it absent or empty", v)
	}
}

func isValidDateHelper(s string) bool {
	if len(s) != 10 {
		return false
	}
	if s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, c := range s {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	year := int(s[0]-'0')*1000 + int(s[1]-'0')*100 + int(s[2]-'0')*10 + int(s[3]-'0')
	month := int(s[5]-'0')*10 + int(s[6]-'0')
	day := int(s[8]-'0')*10 + int(s[9]-'0')
	if month < 1 || month > 12 {
		return false
	}
	if day < 1 || day > 31 {
		return false
	}
	daysInMonth := []int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
		daysInMonth[2] = 29
	}
	if day > daysInMonth[month] {
		return false
	}
	return true
}
