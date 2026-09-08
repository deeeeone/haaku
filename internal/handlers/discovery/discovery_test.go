package discovery

import (
	"testing"
)

func TestDiscoveryCacheKey(t *testing.T) {
	got := discoveryCacheKey("movie", "trending", 1, 20, "min")
	if got != "discovery:movie:trending:page1:limit20:extendedmin" {
		t.Errorf("cache key = %q", got)
	}

	// Distinct inputs must produce distinct keys.
	a := discoveryCacheKey("movie", "trending", 1, 20, "min")
	b := discoveryCacheKey("movie", "popular", 1, 20, "min")
	c := discoveryCacheKey("show", "trending", 1, 20, "min")
	if a == b || a == c || b == c {
		t.Error("cache keys must differ across media type / kind")
	}
}
