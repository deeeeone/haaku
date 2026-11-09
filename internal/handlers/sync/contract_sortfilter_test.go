package sync

import (
	"net/http"
	"strings"
	"testing"
)

// TestSyncSortSegmentsNeverFallBack proves the sorted path segments answer 400
// naming the accepted sorts instead of quietly returning a different ordering.
func TestSyncSortSegmentsNeverFallBack(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	// Every accepted sort is served.
	for _, sortBy := range []string{"rank", "added", "listed", "title", "popularity", "released", "runtime", "votes", "percentage", "random"} {
		status, body := doJSON(t, app, http.MethodGet, "/v1/sync/watchlist/movies/"+sortBy+"/asc", "")
		if status != http.StatusOK {
			t.Errorf("watchlist movie %s: status %d (%s)", sortBy, status, body)
		}
		status, body = doJSON(t, app, http.MethodGet, "/v1/sync/favorites/movies/"+sortBy+"/desc", "")
		if status != http.StatusOK {
			t.Errorf("favorites movie %s: status %d (%s)", sortBy, status, body)
		}
	}

	status, body := doJSON(t, app, http.MethodGet, "/v1/sync/watchlist/movies/notasort/asc", "")
	if status != http.StatusBadRequest {
		t.Fatalf("unknown sort: status %d (%s)", status, body)
	}
	if !strings.Contains(string(body), "rank") || !strings.Contains(string(body), "percentage") {
		t.Errorf("400 body does not name the accepted sorts: %s", body)
	}

	status, _ = doJSON(t, app, http.MethodGet, "/v1/sync/watchlist/movies/rank/sideways", "")
	if status != http.StatusBadRequest {
		t.Errorf("invalid sort_how: status %d, want 400", status)
	}
}

// TestProgressSortRejectionsNameAccepted proves the computed progress reads
// reject an unknown sort instead of ordering by something else.
func TestProgressSortRejectionsNameAccepted(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	status, body := doJSON(t, app, http.MethodGet, "/v1/sync/progress/up_next?sort_by=bogus", "")
	if status != http.StatusBadRequest {
		t.Fatalf("up_next bogus sort: status %d (%s)", status, body)
	}
	if !strings.Contains(string(body), "last_watched_at") {
		t.Errorf("up_next 400 body does not name accepted sorts: %s", body)
	}

	status, body = doJSON(t, app, http.MethodGet, "/v1/sync/progress/watched?sort_by=title&sort_how=sideways", "")
	if status != http.StatusBadRequest {
		t.Fatalf("watched bad direction: status %d (%s)", status, body)
	}
}
