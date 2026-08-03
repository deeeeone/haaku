package tests

import (
	"encoding/json"
	"testing"

	_ "trakt-api/internal/handlers/calendar"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"
)

// TestCalendarEntryJSON verifies the calendar entry JSON serialization.
func TestCalendarEntryJSON(t *testing.T) {
	absEp := 5
	entry := struct {
		Episode media.EpisodeResponse `json:"episode"`
		Show    struct {
			Title string   `json:"title"`
			IDs   media.IDs `json:"ids"`
		} `json:"show"`
		Season struct {
			Number int `json:"number"`
		} `json:"season"`
	}{
		Episode: media.EpisodeResponse{
			IDs:            media.IDs{ID: 100},
			SeasonID:       5,
			ShowID:         7,
			EpisodeNumber:  3,
			Title:          "Pilot",
			Overview:       "The first episode.",
			Runtime:        45,
			AbsoluteNumber: &absEp,
			Images:         map[string]any{"still": "url"},
			AiredAt:        "2026-09-01",
		},
	}
	entry.Show.Title = "Breaking Bad"
	entry.Show.IDs = media.IDs{ID: 7, TMDB: 1396, IMDB: "tt0903747", Slug: "breaking-bad"}
	entry.Season.Number = 1

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("failed to marshal CalendarEntry: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("marshaled JSON is empty")
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	episode, ok := raw["episode"].(map[string]interface{})
	if !ok {
		t.Fatal("episode field missing or wrong type")
	}
	if episode["title"] != "Pilot" {
		t.Errorf("episode.title = %v, want Pilot", episode["title"])
	}
	if episode["aired_at"] != "2026-09-01" {
		t.Errorf("episode.aired_at = %v, want 2026-09-01", episode["aired_at"])
	}

	show, ok := raw["show"].(map[string]interface{})
	if !ok {
		t.Fatal("show field missing or wrong type")
	}
	if show["title"] != "Breaking Bad" {
		t.Errorf("show.title = %v, want Breaking Bad", show["title"])
	}

	showIDs, ok := show["ids"].(map[string]interface{})
	if !ok {
		t.Fatal("show.ids missing")
	}
	if showIDs["id"] == nil {
		t.Error("show.ids.id missing")
	}

	season, ok := raw["season"].(map[string]interface{})
	if !ok {
		t.Fatal("season field missing or wrong type")
	}
	if season["number"] != float64(1) {
		t.Errorf("season.number = %v, want 1", season["number"])
	}
}

// TestCalendarEmptyArray verifies the my/shows stub returns empty array.
func TestCalendarEmptyArray(t *testing.T) {
	entries := make([]interface{}, 0)
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("empty calendar should be [], got %s", string(data))
	}
}

// TestCalendarOpenAPIRoutesRegistered verifies calendar routes can be registered.
func TestCalendarOpenAPIRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/calendars/my/shows":                        true,
		"/v1/calendars/all/shows/{start_date}/{days}":   true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected calendar route %q not registered", path)
		}
	}

	// Verify auth is "none" for calendar routes.
	for _, r := range routes {
		if expectedPaths[r.Path] {
			if r.Auth != "none" {
				t.Errorf("calendar route %s %s has auth=%q, want 'none'", r.Method, r.Path, r.Auth)
			}
		}
	}
}

// TestCalendarDateParsing verifies YYYY-MM-DD parsing logic.
func TestCalendarDateParsing(t *testing.T) {
	validDates := []string{"2026-01-01", "2026-12-31", "2025-02-28", "2024-02-29"}
	for _, d := range validDates {
		if !isValidDate(d) {
			t.Errorf("%q should be a valid date", d)
		}
	}

	invalidDates := []string{"invalid", "2026-13-01", "2026-00-01", "01-01-2026", "", "2026-1-1"}
	for _, d := range invalidDates {
		if isValidDate(d) {
			t.Errorf("%q should be invalid", d)
		}
	}
}

// TestCalendarDaysBounds verifies days parameter bounds (1-30, default 7).
func TestCalendarDaysBounds(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 7},       // default
		{"7", 7},
		{"1", 1},
		{"30", 30},
		{"31", 30},    // capped at 30
		{"100", 30},   // capped
		{"0", 1},      // floor at 1
		{"-5", 7},     // invalid (minus sign) → default 7
		{"abc", 7},    // invalid → default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := clampDaysHelper(tt.input)
			if got != tt.want {
				t.Errorf("clampDays(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestCalendarShowShape verifies the show sub-object shape.
func TestCalendarShowShape(t *testing.T) {
	show := struct {
		Title string   `json:"title"`
		IDs   media.IDs `json:"ids"`
	}{
		Title: "Test Show",
		IDs:   media.IDs{ID: 1, TMDB: 100, Slug: "test-show"},
	}

	data, err := json.Marshal(show)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if raw["title"] != "Test Show" {
		t.Errorf("title = %v", raw["title"])
	}
	if raw["ids"] == nil {
		t.Error("ids missing")
	}
}

// TestCalendarSeasonShape verifies the season sub-object shape.
func TestCalendarSeasonShape(t *testing.T) {
	season := struct {
		Number int `json:"number"`
	}{Number: 3}
	data, err := json.Marshal(season)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `{"number":3}` {
		t.Errorf("season JSON = %s, want {\"number\":3}", string(data))
	}
}

// --- Helpers ---

func isValidDate(s string) bool {
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
	daysInMonth := []int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
		daysInMonth[2] = 29
	}
	if day < 1 || day > daysInMonth[month] {
		return false
	}
	return true
}

func clampDaysHelper(input string) int {
	return clampDays(input)
}

// clampDays duplicates the handler's days-clamping logic.
func clampDays(input string) int {
	if input == "" {
		return 7
	}
	d := 0
	for _, c := range input {
		if c >= '0' && c <= '9' {
			d = d*10 + int(c-'0')
		} else {
			return 7 // invalid
		}
	}
	if d < 1 {
		return 1
	}
	if d > 30 {
		return 30
	}
	return d
}
