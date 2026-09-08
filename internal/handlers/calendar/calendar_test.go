package calendar

import (
	"testing"
	"time"

	"trakt-api/internal/database"
)

func TestClampDays(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 7},       // default
		{"7", 7},      // in range
		{"1", 1},      // min
		{"30", 30},    // max
		{"0", 1},      // below min → clamp to 1
		{"31", 30},    // above max → clamp to 30
		{"999", 30},   // above max
		{"abc", 7},    // non-numeric → default
		{"1a", 7},     // partial non-numeric → default
		{"12", 12},    // multi-digit
	}
	for _, tc := range cases {
		if got := clampDays(tc.in); got != tc.want {
			t.Errorf("clampDays(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestDeriveTarget(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/v1/calendars/my/shows", "my"},
		{"/v1/calendars/all/shows", "all"},
		{"/v1/calendars/premieres/shows", "premieres"},
		{"/v1/calendars/my/shows/premieres", "my"},
		{"/v1/calendars/", ""},   // trailing slash yields empty target (impl behavior)
		{"/", "all"},
		{"", "all"},
	}
	for _, tc := range cases {
		if got := deriveTarget(tc.path); got != tc.want {
			t.Errorf("deriveTarget(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestBuildShowIDs(t *testing.T) {
	s := &database.Show{ID: 11, TmdbID: 1212, ImdbID: "tt123"}
	ids := buildShowIDs(s)
	if ids.ID != 11 || ids.TMDB != 1212 || ids.IMDB != "tt123" {
		t.Errorf("buildShowIDs = %+v", ids)
	}

	// Zero/empty IDs are omitted (omitempty on the wire).
	s2 := &database.Show{ID: 12}
	ids2 := buildShowIDs(s2)
	if ids2.TMDB != 0 || ids2.IMDB != "" {
		t.Errorf("buildShowIDs with zero ids = %+v", ids2)
	}
}

func TestBuildEpisode(t *testing.T) {
	aired := database.Date{time.Date(2020, 3, 15, 0, 0, 0, 0, time.UTC)}
	ep := &database.Episode{
		ID:            30,
		SeasonID:      10,
		ShowID:        11,
		EpisodeNumber: 4,
		Title:         "Ep 4",
		Overview:      "o",
		Runtime:       45,
		Images:        database.JSONMap{"poster": "x"},
		AbsoluteNumber: intPtr(5),
		AiredAt:       &aired,
	}
	got := buildEpisode(ep)
	if got.IDs.ID != 30 || got.SeasonID != 10 || got.ShowID != 11 || got.EpisodeNumber != 4 {
		t.Errorf("episode base wrong: %+v", got)
	}
	if got.Title != "Ep 4" || got.Overview != "o" || got.Runtime != 45 {
		t.Errorf("episode content wrong: %+v", got)
	}
	if got.AiredAt != "2020-03-15" {
		t.Errorf("AiredAt = %q", got.AiredAt)
	}
	if got.AbsoluteNumber == nil || *got.AbsoluteNumber != 5 {
		t.Errorf("AbsoluteNumber = %v", got.AbsoluteNumber)
	}
	if len(got.Images) == 0 {
		t.Error("Images should carry through")
	}

	// Empty Images becomes an empty map (never nil on the wire).
	ep2 := &database.Episode{ID: 31, Images: nil}
	got2 := buildEpisode(ep2)
	if got2.Images == nil {
		t.Error("buildEpisode Images must be non-nil")
	}
	if got2.AiredAt != "" {
		t.Errorf("AiredAt = %q, want empty when AiredAt nil", got2.AiredAt)
	}
}

func intPtr(i int) *int { return &i }
