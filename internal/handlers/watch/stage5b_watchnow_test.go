package watch

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newStage5bWatchDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(database.MediaModels()...); err != nil {
		t.Fatalf("migrate media: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

func newStage5bWatchApp(t *testing.T) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	RegisterRoutes(app.Group("/v1"))
	return app
}

// setStage5bTMDB points the package singleton at a client (or at a nil client
// with an error, which is how "TMDB not configured" is exercised).
func setStage5bTMDB(t *testing.T, client *tmdb.Client, err error) {
	t.Helper()
	oldClient, oldErr := tmdbClient, tmdbClientErr
	tmdbClient, tmdbClientErr = client, err
	tmdbOnce = sync.Once{}
	tmdbOnce.Do(func() {})
	t.Cleanup(func() {
		tmdbClient, tmdbClientErr = oldClient, oldErr
		tmdbOnce = sync.Once{}
		tmdbOnce.Do(func() {})
	})
}

func newStage5bProviderStub(t *testing.T) (*tmdb.Client, *int32) {
	t.Helper()
	var calls int32
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, payload string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			atomic.AddInt32(&calls, 1)
			next(w, r)
		}
	}
	mux.HandleFunc("/tv/1396/season/1/episode/1/watch/providers", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, `{"id":1396,"results":{"US":{
			"link":"https://www.themoviedb.org/tv/1396/season/1/episode/1/watch?locale=US",
			"flatrate":[{"provider_id":8,"provider_name":"Netflix","logo_path":"/n.png"}],
			"rent":[],"buy":[]
		}}}`)
	}))
	mux.HandleFunc("/movie/603/watch/providers", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, `{"id":603,"results":{"US":{
			"link":"https://www.themoviedb.org/movie/603/watch?locale=US",
			"flatrate":[{"provider_id":8,"provider_name":"Netflix","logo_path":"/n.png"}],
			"rent":[],"buy":[]
		}}}`)
	}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Logf("unexpected TMDB path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Setenv("TMDB_API_KEY", "test-token")
	client, err := tmdb.New()
	if err != nil {
		t.Fatalf("tmdb.New: %v", err)
	}
	client.SetBaseURL(srv.URL)
	return client, &calls
}

func seedStage5bMedia(t *testing.T, db *gorm.DB, withEpisode bool) {
	t.Helper()
	if err := db.Create(&database.Show{ID: 1, TmdbID: 1396, Title: "Breaking Bad", Slug: "breaking-bad"}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	if err := db.Create(&database.Season{ID: 1, ShowID: 1, TmdbShowID: 1396, SeasonNumber: 1, EpisodeCount: 1}).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	if withEpisode {
		if err := db.Create(&database.Episode{ID: 1, SeasonID: 1, ShowID: 1, EpisodeNumber: 1, Title: "Pilot"}).Error; err != nil {
			t.Fatalf("seed episode: %v", err)
		}
	}
	if err := db.Create(&database.Movie{ID: 1, TmdbID: 603, Title: "The Matrix", Slug: "the-matrix", Metadata: database.JSONMap{}}).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
}

func stage5bWatchGET(t *testing.T, app *fiber.App, path string) (int, []byte) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil), -1)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: read body: %v", path, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, raw
}

func decodeStage5bObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode object: %v (%s)", err, raw)
	}
	return out
}

func TestEpisodeWatchNowHydratesOnce(t *testing.T) {
	db := newStage5bWatchDB(t)
	client, calls := newStage5bProviderStub(t)
	setStage5bTMDB(t, client, nil)
	app := newStage5bWatchApp(t)
	seedStage5bMedia(t, db, true)

	for i := 0; i < 2; i++ {
		status, raw := stage5bWatchGET(t, app, "/v1/shows/1/seasons/1/episodes/1/watchnow/us")
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

	if status, raw := stage5bWatchGET(t, app, "/v1/shows/1/seasons/1/episodes/9/watchnow/us"); status != http.StatusNotFound {
		t.Fatalf("unknown episode status = %d, want 404 (%s)", status, raw)
	}
}

func TestEpisodeWatchNowWithoutTMDBReturnsEmptyObject(t *testing.T) {
	db := newStage5bWatchDB(t)
	setStage5bTMDB(t, nil, errors.New("not configured"))
	app := newStage5bWatchApp(t)
	seedStage5bMedia(t, db, true)

	status, raw := stage5bWatchGET(t, app, "/v1/shows/1/seasons/1/episodes/1/watchnow/us")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	got := decodeStage5bObject(t, raw)
	for _, key := range []string{"cable", "free", "cinema", "subscription", "purchase"} {
		items, ok := got[key].([]any)
		if !ok || len(items) != 0 {
			t.Fatalf("%s = %#v, want an empty array", key, got[key])
		}
	}
}

func TestMovieJustWatchLinksFromStoredProviders(t *testing.T) {
	db := newStage5bWatchDB(t)
	setStage5bTMDB(t, nil, errors.New("not configured"))
	app := newStage5bWatchApp(t)
	seedStage5bMedia(t, db, false)

	link := "https://www.themoviedb.org/movie/603/watch?locale=US"
	meta := mustPersistWatchProviders(map[string]string{"US": link})
	if err := db.Model(&database.Movie{}).Where("id = ?", 1).Update("metadata", meta).Error; err != nil {
		t.Fatalf("seed movie metadata: %v", err)
	}

	status, raw := stage5bWatchGET(t, app, "/v1/movies/1/watchnow/justwatch_links/us")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	got := decodeStage5bObject(t, raw)
	if got["web"] != link {
		t.Fatalf("justwatch links = %v, want web = %s", got, link)
	}

	status, raw = stage5bWatchGET(t, app, "/v1/movies/1/watchnow/justwatch_links/ca")
	if status != http.StatusOK {
		t.Fatalf("absent country status = %d, want 200 (%s)", status, raw)
	}
	if got := decodeStage5bObject(t, raw); len(got) != 0 {
		t.Fatalf("absent country links = %v, want {}", got)
	}
}

func TestMovieJustWatchLinksHydrateOnce(t *testing.T) {
	db := newStage5bWatchDB(t)
	client, calls := newStage5bProviderStub(t)
	setStage5bTMDB(t, client, nil)
	app := newStage5bWatchApp(t)
	seedStage5bMedia(t, db, false)

	link := "https://www.themoviedb.org/movie/603/watch?locale=US"
	for i := 0; i < 2; i++ {
		status, raw := stage5bWatchGET(t, app, "/v1/movies/1/watchnow/justwatch_links/us")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", status, raw)
		}
		if got := decodeStage5bObject(t, raw); got["web"] != link {
			t.Fatalf("links = %v, want web = %s", got, link)
		}
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("TMDB calls = %d, want 1", got)
	}
}
