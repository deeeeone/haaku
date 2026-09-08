package seasons

import (
	"testing"
)

func TestSeasonCacheKey(t *testing.T) {
	got := seasonCacheKey("season", 12, "")
	if got != "season:season:12" {
		t.Errorf("seasonCacheKey(no extended) = %q", got)
	}

	gotExt := seasonCacheKey("season", 12, "full")
	if gotExt != "season:season:12:extendedfull" {
		t.Errorf("seasonCacheKey(extended) = %q", gotExt)
	}

	// Different kinds (season vs ratings vs stats) must key apart.
	if seasonCacheKey("season", 12, "") == seasonCacheKey("ratings", 12, "") {
		t.Error("cache key must distinguish kind")
	}
}
