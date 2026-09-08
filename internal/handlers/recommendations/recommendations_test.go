package recommendations

import (
	"testing"
)

func TestTopGenresCacheKey(t *testing.T) {
	got := topGenresCacheKey("user-1", 7)
	if got != "recommendations:topgenres:user-1:v7" {
		t.Errorf("topGenresCacheKey = %q", got)
	}

	// Different versions must produce different keys (version-keyed cache).
	a := topGenresCacheKey("u", 1)
	b := topGenresCacheKey("u", 2)
	if a == b {
		t.Error("cache key must be version-sensitive")
	}
	if a == topGenresCacheKey("other", 1) {
		t.Error("cache key must be user-sensitive")
	}
}
