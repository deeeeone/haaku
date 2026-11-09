package watch

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/haaku-tv/haaku/internal/database"
)

func TestFlatEpisodeWatchNowHydratesOnce(t *testing.T) {
	db := newStage5bWatchDB(t)
	client, calls := newStage5bProviderStub(t)
	setStage5bTMDB(t, client, nil)
	app := newStage5bWatchApp(t)
	seedStage5bMedia(t, db, true)

	for i := 0; i < 2; i++ {
		status, raw := stage5bWatchGET(t, app, "/v1/episodes/1/watchnow/us")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", status, raw)
		}
		got := decodeStage5bObject(t, raw)
		subscription, _ := got["subscription"].([]any)
		if len(subscription) != 1 {
			t.Fatalf("subscription = %v", got["subscription"])
		}
		provider := subscription[0].(map[string]any)
		if provider["source"] != "netflix" {
			t.Fatalf("provider = %v", provider)
		}
		if provider["link"] != "https://www.themoviedb.org/tv/1396/season/1/episode/1/watch?locale=US" {
			t.Fatalf("provider link = %v", provider["link"])
		}
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("TMDB calls = %d, want 1", got)
	}

	var episode database.Episode
	if err := db.First(&episode, 1).Error; err != nil {
		t.Fatalf("reload episode: %v", err)
	}
	if _, ok := episode.Metadata["watch_providers"]; !ok {
		t.Fatalf("episode metadata has no watch_providers key: %v", episode.Metadata)
	}
}

func TestFlatEpisodeWatchNowUnknownIDIs404(t *testing.T) {
	db := newStage5bWatchDB(t)
	client, calls := newStage5bProviderStub(t)
	setStage5bTMDB(t, client, nil)
	app := newStage5bWatchApp(t)
	seedStage5bMedia(t, db, true)

	if status, raw := stage5bWatchGET(t, app, "/v1/episodes/99/watchnow/us"); status != http.StatusNotFound {
		t.Fatalf("unknown episode status = %d, want 404 (%s)", status, raw)
	}
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("TMDB calls = %d, want 0 for unknown episode", got)
	}
}

func TestFlatEpisodeWatchNowNonIntegerIDIs400(t *testing.T) {
	db := newStage5bWatchDB(t)
	client, calls := newStage5bProviderStub(t)
	setStage5bTMDB(t, client, nil)
	app := newStage5bWatchApp(t)
	seedStage5bMedia(t, db, true)

	if status, raw := stage5bWatchGET(t, app, "/v1/episodes/not-a-number/watchnow/us"); status != http.StatusBadRequest {
		t.Fatalf("non-integer id status = %d, want 400 (%s)", status, raw)
	}
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("TMDB calls = %d, want 0 for invalid id", got)
	}
}
