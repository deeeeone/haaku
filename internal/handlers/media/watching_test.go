package media

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupWatchingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Movie{}, &database.Show{}, &database.Season{}, &database.Episode{},
		&database.User{}, &database.UserBlock{}, &database.UserFollow{},
	); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	// scrobble_sessions carries `DEFAULT now()` in its GORM tag, which sqlite
	// rejects; create the same columns with portable DDL.
	if err := db.Exec(`CREATE TABLE scrobble_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT, user_id TEXT NOT NULL, media_type TEXT NOT NULL,
		media_id INTEGER NOT NULL, season_id INTEGER, episode_id INTEGER, action TEXT NOT NULL,
		progress REAL NOT NULL DEFAULT 0, expires_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`).Error; err != nil {
		t.Fatalf("create sqlite scrobble_sessions: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

func watchingApp() *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	api := app.Group("/v1")
	api.Get("/movies/:id/watching", handleGetMovieWatching)
	api.Get("/shows/:id/watching", handleGetShowWatching)
	return app
}

func seedUser(t *testing.T, db *gorm.DB, id, username string, private bool) {
	t.Helper()
	if err := db.Create(&database.User{ID: id, Username: username, IsPrivate: private}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func seedSession(t *testing.T, db *gorm.DB, user, mediaType string, mediaID int64, action string, episodeID *int64) {
	t.Helper()
	session := database.ScrobbleSession{
		UserID:    user,
		MediaType: mediaType,
		MediaID:   mediaID,
		EpisodeID: episodeID,
		Action:    action,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func watchingProfiles(t *testing.T, app *fiber.App, path string) []map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s status = %d, want 200; body: %s", path, resp.StatusCode, body)
	}
	var out []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestMovieWatchingExcludesPrivateUsers(t *testing.T) {
	db := setupWatchingDB(t)
	seedUser(t, db, "pub", "public-user", false)
	seedUser(t, db, "priv", "private-user", true)
	movie := database.Movie{TmdbID: 950, Title: "Watched Movie"}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	seedSession(t, db, "pub", "movie", movie.ID, "start", nil)
	seedSession(t, db, "priv", "movie", movie.ID, "start", nil)

	profiles := watchingProfiles(t, watchingApp(), "/v1/movies/"+watchingID(movie.ID)+"/watching")
	if len(profiles) != 1 {
		t.Fatalf("watching = %+v, want only the public user", profiles)
	}
	if profiles[0]["username"] != "public-user" {
		t.Errorf("username = %v, want public-user", profiles[0]["username"])
	}
}

func TestMovieWatchingEmptyIsArray(t *testing.T) {
	db := setupWatchingDB(t)
	movie := database.Movie{TmdbID: 951, Title: "Nobody Watching"}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/movies/"+watchingID(movie.ID)+"/watching", nil)
	req.Host = "example.com"
	resp, err := watchingApp().Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(body)) != "[]" {
		t.Errorf("body = %q, want []", body)
	}
}

func TestShowWatchingRollsUpEpisodesAndDedupes(t *testing.T) {
	db := setupWatchingDB(t)
	seedUser(t, db, "pub", "public-user", false)
	seedUser(t, db, "priv", "private-user", true)
	show := database.Show{TmdbID: 960, Title: "Watched Show"}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	season := database.Season{ShowID: show.ID, SeasonNumber: 1, EpisodeCount: 1}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	episode := database.Episode{SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: 1, Title: "E1"}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatalf("seed episode: %v", err)
	}

	// The same public user appears through an episode and the show row; the
	// feed reports one account, not two.
	seedSession(t, db, "pub", "episode", episode.ID, "start", &episode.ID)
	seedSession(t, db, "pub", "show", show.ID, "scrobble", nil)
	seedSession(t, db, "priv", "episode", episode.ID, "start", &episode.ID)

	profiles := watchingProfiles(t, watchingApp(), "/v1/shows/"+watchingID(show.ID)+"/watching")
	if len(profiles) != 1 {
		t.Fatalf("watching = %+v, want one deduped public user", profiles)
	}
}

func watchingID(n int64) string {
	return strconv.FormatInt(n, 10)
}
