package reference

import (
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Action", "action"},
		{"Action & Adventure", "action-and-adventure"},
		{"SCIENCE FICTION", "science-fiction"},
		{"Sci-Fi", "sci-fi"},
		{"", ""},
		{"  Spaces  ", "--spaces--"}, // every space becomes a hyphen (impl behavior)
	}
	for _, tc := range cases {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCacheKey(t *testing.T) {
	if got := cacheKey("genres", "movie"); got != "reference:genres:movie" {
		t.Errorf("cacheKey = %q", got)
	}
	if got := cacheKey("certifications", ""); got != "reference:certifications:" {
		t.Errorf("cacheKey(empty subtype) = %q", got)
	}
}
