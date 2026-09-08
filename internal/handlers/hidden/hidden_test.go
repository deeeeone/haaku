package hidden

import (
	"testing"
)

func TestValidSectionsAllowlist(t *testing.T) {
	allowed := []string{
		"calendar", "progress_watched", "progress_collected",
		"recommendations", "comments", "dropped",
	}
	for _, s := range allowed {
		if !validSections[s] {
			t.Errorf("validSections missing %q", s)
		}
	}
	for _, s := range []string{"", "movies", "watchlist"} {
		if validSections[s] {
			t.Errorf("validSections unexpectedly accepts %q", s)
		}
	}
}

func TestValidMediaTypesAllowlist(t *testing.T) {
	allowed := []string{"movie", "show", "season", "episode"}
	for _, m := range allowed {
		if !validMediaTypes[m] {
			t.Errorf("validMediaTypes missing %q", m)
		}
	}
	for _, m := range []string{"", "person", "list", "MOVIE"} {
		if validMediaTypes[m] {
			t.Errorf("validMediaTypes unexpectedly accepts %q", m)
		}
	}
}
