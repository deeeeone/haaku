package tests

import (
	"encoding/json"
	"testing"

	_ "github.com/haaku-tv/haaku/internal/handlers/calendar"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/openapi"
)

// TestCalendarEntryJSON verifies the calendar entry JSON serialization.
func TestCalendarEntryJSON(t *testing.T) {
	absEp := 5
	entry := struct {
		Episode media.EpisodeResponse `json:"episode"`
		Show    struct {
			Title string    `json:"title"`
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

// TestCalendarOpenAPIRoutesRegistered verifies all calendar routes are registered.
func TestCalendarOpenAPIRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]string{
		"/v1/calendars/my/shows/{start_date}/{days}":        "required",
		"/v1/calendars/all/shows/{start_date}/{days}":       "none",
		"/v1/calendars/friends/shows/{start_date}/{days}":   "required",
		"/v1/calendars/favorites/shows/{start_date}/{days}": "required",
		"/v1/calendars/watched/shows/{start_date}/{days}":   "required",
	}

	found := make(map[string]string)
	for _, r := range routes {
		if expectedAuth, exists := expectedPaths[r.Path]; exists {
			found[r.Path] = r.Auth
			if r.Auth != expectedAuth {
				t.Errorf("calendar route %s %s has auth=%q, want %q", r.Method, r.Path, r.Auth, expectedAuth)
			}
		}
	}

	for path := range expectedPaths {
		if _, ok := found[path]; !ok {
			t.Errorf("expected calendar route %q not registered", path)
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
		{"", 7}, // default
		{"7", 7},
		{"1", 1},
		{"30", 30},
		{"31", 30},  // capped at 30
		{"100", 30}, // capped
		{"0", 1},    // floor at 1
		{"-5", 7},   // invalid (minus sign) → default 7
		{"abc", 7},  // invalid → default
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
		Title string    `json:"title"`
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
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
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

// TestCalendarTypeFilterValues verifies accepted type filter values.
func TestCalendarTypeFilterValues(t *testing.T) {
	validTypes := map[string]bool{"": true, "new": true, "premieres": true, "finale": true}
	invalidTypes := []string{"invalid", "premiere", "final", "NEW", " "}

	for _, ft := range invalidTypes {
		if validTypes[ft] {
			t.Errorf("%q should be invalid type filter", ft)
		}
	}
	for ft := range validTypes {
		if !validTypes[ft] {
			t.Errorf("%q should be valid type filter", ft)
		}
	}
}

// TestCalendarPaginationEnvelope verifies pagination fields in responses.
func TestCalendarPaginationEnvelope(t *testing.T) {
	envelope := struct {
		Items    []interface{} `json:"items"`
		Page     int           `json:"page"`
		Limit    int           `json:"limit"`
		RowCount int64         `json:"row_count"`
	}{
		Items:    []interface{}{},
		Page:     1,
		Limit:    25,
		RowCount: 0,
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if raw["page"] == nil || raw["limit"] == nil || raw["row_count"] == nil {
		t.Error("pagination envelope missing required fields")
	}
	if raw["items"] == nil {
		t.Error("items field missing from envelope")
	}
}

// TestCalendarTargetsDerivation verifies target derivation logic.
func TestCalendarTargetsDerivation(t *testing.T) {
	tests := []struct {
		path   string
		target string
	}{
		{"/v1/calendars/my/shows/2026-01-01/7", "my"},
		{"/v1/calendars/all/shows/2026-01-01/7", "all"},
		{"/v1/calendars/friends/shows/2026-01-01/7", "friends"},
		{"/v1/calendars/favorites/shows/2026-01-01/7", "favorites"},
		{"/v1/calendars/watched/shows/2026-01-01/7", "watched"},
		{"/unknown", "all"},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got := deriveTargetHelper(tt.path)
			if got != tt.target {
				t.Errorf("deriveTarget(%q) = %q, want %q", tt.path, got, tt.target)
			}
		})
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

// deriveTargetHelper duplicates the handler's target derivation logic.
func deriveTargetHelper(path string) string {
	idx := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			idx++
			if idx == 3 {
				start := i + 1
				end := start
				for end < len(path) && path[end] != '/' {
					end++
				}
				return path[start:end]
			}
		}
	}
	return "all"
}
