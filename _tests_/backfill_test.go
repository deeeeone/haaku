package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/metadata"
)

// newFakeTMDB serves the v3 routes the metadata worker's backfill walks.
func newFakeTMDB(t *testing.T) *tmdb.Client {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/tv/popular", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"page":1,"results":[{"id":1437,"name":"Firefly","media_type":"tv"}],"total_pages":1}`))
	})
	mux.HandleFunc("/movie/popular", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"page":1,"results":[{"id":603,"title":"The Matrix","media_type":"movie"}],"total_pages":1}`))
	})
	mux.HandleFunc("/trending/all/day", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"page":1,"results":[],"total_pages":1}`))
	})
	mux.HandleFunc("/trending/all/week", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"page":1,"results":[],"total_pages":1}`))
	})
	mux.HandleFunc("/tv/1437", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":1437,"name":"Firefly","imdb_id":"tt0303461","first_air_date":"2002-09-20",
			"number_of_seasons":1,"number_of_episodes":2,
			"seasons":[{"id":1,"season_number":1,"name":"Season 1","episode_count":2,"air_date":"2002-09-20"}]}`))
	})
	mux.HandleFunc("/tv/1437/season/1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":1,"season_number":1,"name":"Season 1","episodes":[
			{"id":11,"episode_number":1,"season_number":1,"name":"Serenity","air_date":"2002-09-20","runtime":86},
			{"id":12,"episode_number":2,"season_number":1,"name":"The Train Job","air_date":"2002-09-27","runtime":44}]}`))
	})
	mux.HandleFunc("/movie/603", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":603,"title":"The Matrix","imdb_id":"tt0133093","release_date":"1999-03-31"}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Setenv("TMDB_API_TOKEN", "test-token")
	client, err := tmdb.New()
	if err != nil {
		t.Fatalf("tmdb.New: %v", err)
	}
	client.SetBaseURL(srv.URL)
	return client
}

// TestBackfillAttachesEpisodesToTheirSeason is the regression test for the
// backfill passing the SHOW id where a SEASON id belongs: every episode of
// every season landed under one bogus season_id, so season-level reads
// (/shows/{id}/seasons/{n}/episodes) returned nothing for a show the worker had
// just imported.
func TestBackfillAttachesEpisodesToTheirSeason(t *testing.T) {
	db := setupTVDBTestDB(t)
	client := newFakeTMDB(t)

	// Seed a decoy season FIRST so the seasons sequence is ahead of the shows
	// sequence. Without it a fresh database gives the backfilled show id 1 and
	// its season id 1, and "episodes point at the show id" would look identical
	// to the correct answer — the bug this test exists for would slip through.
	decoy := database.Season{ShowID: 999, SeasonNumber: 9, EpisodeCount: 0}
	if err := db.Create(&decoy).Error; err != nil {
		t.Fatalf("seed decoy season: %v", err)
	}

	if err := metadata.Backfill(context.Background(), client, db, 1); err != nil {
		t.Fatalf("Backfill: %v", err)
	}

	var show database.Show
	if err := db.Where("tmdb_id = ?", 1437).First(&show).Error; err != nil {
		t.Fatalf("backfill did not ingest the show: %v", err)
	}

	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", show.ID, 1).First(&season).Error; err != nil {
		t.Fatalf("backfill did not ingest season 1: %v", err)
	}

	var episodes []database.Episode
	if err := db.Where("show_id = ?", show.ID).Order("episode_number").Find(&episodes).Error; err != nil {
		t.Fatalf("load episodes: %v", err)
	}
	if len(episodes) != 2 {
		t.Fatalf("episodes = %d, want 2", len(episodes))
	}
	for _, ep := range episodes {
		if ep.SeasonID != season.ID {
			t.Errorf("episode %d has season_id %d, want the season row %d (show id is %d)",
				ep.EpisodeNumber, ep.SeasonID, season.ID, show.ID)
		}
	}

	// A movie from the same pass still ingests (the fix must not disturb the
	// movie path).
	var movie database.Movie
	if err := db.Where("tmdb_id = ?", 603).First(&movie).Error; err != nil {
		t.Fatalf("backfill did not ingest the movie: %v", err)
	}
	if movie.Title != "The Matrix" {
		t.Errorf("movie = %q", movie.Title)
	}
}

// TestBackfillUsesV3Paths fails if the client is ever pointed back at the v4
// tree, where every one of these routes 404s (measured against the live API):
// the backfill then "succeeds" while importing nothing.
func TestBackfillUsesV3Paths(t *testing.T) {
	// The handler runs on the server's goroutines; the test goroutine reads
	// paths after Backfill returns. Without the lock `go test -race` flags it.
	var (
		mu    sync.Mutex
		paths []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch {
		case r.URL.Path == "/tv/popular":
			_, _ = w.Write([]byte(`{"page":1,"results":[],"total_pages":1}`))
		case r.URL.Path == "/movie/popular":
			_, _ = w.Write([]byte(`{"page":1,"results":[],"total_pages":1}`))
		case strings.HasPrefix(r.URL.Path, "/trending/"):
			_, _ = w.Write([]byte(`{"page":1,"results":[],"total_pages":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status_code":34,"status_message":"The resource you requested could not be found."}`))
		}
	}))
	defer srv.Close()

	t.Setenv("TMDB_API_TOKEN", "test-token")
	client, err := tmdb.New()
	if err != nil {
		t.Fatalf("tmdb.New: %v", err)
	}
	client.SetBaseURL(srv.URL)

	db := setupTVDBTestDB(t)
	if err := metadata.Backfill(context.Background(), client, db, 1); err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	mu.Lock()
	recorded := append([]string(nil), paths...)
	mu.Unlock()
	if len(recorded) == 0 {
		t.Fatal("the backfill made no requests")
	}
	for _, p := range recorded {
		if strings.HasPrefix(p, "/v4/") || strings.HasPrefix(p, "/4/") {
			t.Errorf("backfill requested %s — the client is addressing the v4 tree again", p)
		}
	}
}
