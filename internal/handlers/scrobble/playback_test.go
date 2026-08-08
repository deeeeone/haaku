package scrobble

import "testing"

func TestParsePlaybackMediaID(t *testing.T) {
	valid := []string{"1", "12", "9999999999"}
	for _, s := range valid {
		if _, err := parsePlaybackMediaID(s); err != nil {
			t.Errorf("parsePlaybackMediaID(%q) unexpected error: %v", s, err)
		}
	}

	invalid := []string{
		"", "abc", "12abc", "12.5", "-5", "0", " 12", "12 ",
	}
	for _, s := range invalid {
		if _, err := parsePlaybackMediaID(s); err == nil {
			t.Errorf("parsePlaybackMediaID(%q) expected error (rejects partial/non-positive parses)", s)
		}
	}
}

func TestValidMediaTypes(t *testing.T) {
	for _, mt := range []string{"movie", "show", "episode"} {
		if !validMediaTypes[mt] {
			t.Errorf("expected %q to be a valid playback media type", mt)
		}
	}
	for _, mt := range []string{"", "tv", "season", "movies", "unknown"} {
		if validMediaTypes[mt] {
			t.Errorf("expected %q to be rejected", mt)
		}
	}
}
