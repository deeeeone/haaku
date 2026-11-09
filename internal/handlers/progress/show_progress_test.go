package progress

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupShowProgressDB opens an in-memory sqlite DB migrated with every table
// the show-progress handlers touch and points the database singleton at it.
func setupShowProgressDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	models := []interface{}{
		&database.Movie{}, &database.Show{}, &database.Season{}, &database.Episode{},
		&database.Person{}, &database.ExternalID{},
		&database.UserHistory{}, &database.UserCollection{}, &database.UserRating{},
		&database.UserShowProgressReset{}, &database.EventsOutbox{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// showProgressApp wires the new show-progress handlers behind a middleware that
// injects the caller's user id, so the handler body runs without Clerk.
func showProgressApp(userID string) *fiber.App {
	// The media handlers return response.ErrHandled when a helper has already
	// written the response; without the app's error handler Fiber would turn
	// that into a 500. Production wires the same one (httpapi/server.go).
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	stub := func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	}
	shows := app.Group("/shows")
	shows.Get("/:id/progress/watched", stub, handleShowProgressWatched)
	shows.Get("/:id/progress/collection", stub, handleShowProgressCollection)
	shows.Post("/:id/progress/watched/reset", stub, handleShowProgressReset)
	shows.Delete("/:id/progress/watched/reset", stub, handleShowProgressResetUndo)
	return app
}

func doProgressReq(t *testing.T, app *fiber.App, method, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, path, nil)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s %s failed: %v", method, path, err)
	}
	return resp
}

func decodeShowProgress(t *testing.T, resp *http.Response) ShowProgressResponse {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, body)
	}
	var out ShowProgressResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode progress body: %v", err)
	}
	return out
}

// seedShow create a show and returns its id.
func seedShow(t *testing.T, db *gorm.DB, tmdbID int, title string) int64 {
	t.Helper()
	show := database.Show{Title: title, TmdbID: tmdbID}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	return show.ID
}

// seedSeasonWithEpisodes creates a season and one episode per (episodeNumber,
// airdate) pair, returning the episode ids in the order given.
func seedSeasonWithEpisodes(t *testing.T, db *gorm.DB, showID int64, seasonNumber int, episodes []struct {
	number  int
	airedAt *time.Time
}) []int64 {
	t.Helper()
	season := database.Season{ShowID: showID, SeasonNumber: seasonNumber, EpisodeCount: len(episodes)}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	ids := make([]int64, 0, len(episodes))
	for _, e := range episodes {
		ep := database.Episode{SeasonID: season.ID, ShowID: showID, EpisodeNumber: e.number, Title: "E" + strconv.Itoa(e.number)}
		if e.airedAt != nil {
			d := database.Date{Time: e.airedAt.UTC()}
			ep.AiredAt = &d
		}
		if err := db.Create(&ep).Error; err != nil {
			t.Fatalf("seed episode: %v", err)
		}
		ids = append(ids, ep.ID)
	}
	return ids
}

type epSeed = struct {
	number  int
	airedAt *time.Time
}

func airedDaysAgo(days int) *time.Time {
	t := time.Now().UTC().AddDate(0, 0, -days)
	return &t
}

func TestShowProgressZeroEpisodes(t *testing.T) {
	db := setupShowProgressDB(t)
	showID := seedShow(t, db, 7001, "Empty Show")

	resp := doProgressReq(t, showProgressApp("user-1"), http.MethodGet, "/shows/"+strconv.FormatInt(showID, 10)+"/progress/watched")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeShowProgress(t, resp)

	if got.Aired != 0 || got.Completed != 0 {
		t.Errorf("aired/completed = %d/%d, want 0/0", got.Aired, got.Completed)
	}
	if got.Seasons == nil {
		t.Error("seasons must be an empty array, not null")
	}
	if len(got.Seasons) != 0 {
		t.Errorf("seasons = %+v, want empty", got.Seasons)
	}
	if got.NextEpisode != nil {
		t.Errorf("next_episode = %+v, want null", got.NextEpisode)
	}
	if got.LastWatchedAt != nil {
		t.Errorf("last_watched_at = %v, want null", *got.LastWatchedAt)
	}
}

func TestShowProgressPartiallyWatched(t *testing.T) {
	db := setupShowProgressDB(t)
	showID := seedShow(t, db, 7002, "Partial")
	s1 := seedSeasonWithEpisodes(t, db, showID, 1, []epSeed{{1, airedDaysAgo(10)}, {2, airedDaysAgo(9)}})
	seedSeasonWithEpisodes(t, db, showID, 2, []epSeed{{1, airedDaysAgo(5)}, {2, airedDaysAgo(4)}, {3, airedDaysAgo(3)}})

	watchedAt := time.Now().UTC().AddDate(0, 0, -8)
	if err := db.Create(&database.UserHistory{
		UserID: "user-1", MediaType: "episode", MediaID: s1[0], WatchedAt: watchedAt, Source: "api",
	}).Error; err != nil {
		t.Fatalf("seed history: %v", err)
	}
	// Another user's watch must not count.
	if err := db.Create(&database.UserHistory{
		UserID: "other", MediaType: "episode", MediaID: s1[1], WatchedAt: time.Now().UTC(), Source: "api",
	}).Error; err != nil {
		t.Fatalf("seed other history: %v", err)
	}

	resp := doProgressReq(t, showProgressApp("user-1"), http.MethodGet, "/shows/"+strconv.FormatInt(showID, 10)+"/progress/watched")
	got := decodeShowProgress(t, resp)

	if got.Aired != 5 {
		t.Errorf("aired = %d, want 5", got.Aired)
	}
	if got.Completed != 1 {
		t.Errorf("completed = %d, want 1", got.Completed)
	}
	if len(got.Seasons) != 2 {
		t.Fatalf("seasons = %d, want 2", len(got.Seasons))
	}
	if got.Seasons[0].Number != 1 || got.Seasons[0].Aired != 2 || got.Seasons[0].Completed != 1 {
		t.Errorf("season 1 = %+v, want aired 2 completed 1", got.Seasons[0])
	}
	if got.Seasons[1].Number != 2 || got.Seasons[1].Aired != 3 || got.Seasons[1].Completed != 0 {
		t.Errorf("season 2 = %+v, want aired 3 completed 0", got.Seasons[1])
	}
	if !got.Seasons[0].Episodes[0].Completed {
		t.Error("season 1 episode 1 should be completed")
	}
	if got.Seasons[0].Episodes[1].Completed {
		t.Error("season 1 episode 2 should not be completed")
	}
	if got.LastWatchedAt == nil {
		t.Fatal("last_watched_at should be set")
	}
	want := watchedAt.Format("2006-01-02T15:04:05.000Z")
	if *got.LastWatchedAt != want {
		t.Errorf("last_watched_at = %s, want %s", *got.LastWatchedAt, want)
	}
}

func TestShowProgressFullyWatched(t *testing.T) {
	db := setupShowProgressDB(t)
	showID := seedShow(t, db, 7003, "Complete")
	ids := seedSeasonWithEpisodes(t, db, showID, 1, []epSeed{{1, airedDaysAgo(3)}, {2, airedDaysAgo(2)}, {3, airedDaysAgo(1)}})
	for _, id := range ids {
		if err := db.Create(&database.UserHistory{
			UserID: "user-1", MediaType: "episode", MediaID: id, WatchedAt: time.Now().UTC(), Source: "api",
		}).Error; err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}

	got := decodeShowProgress(t, doProgressReq(t, showProgressApp("user-1"), http.MethodGet,
		"/shows/"+strconv.FormatInt(showID, 10)+"/progress/watched"))

	if got.Aired != 3 || got.Completed != 3 {
		t.Fatalf("aired/completed = %d/%d, want 3/3", got.Aired, got.Completed)
	}
	for _, e := range got.Seasons[0].Episodes {
		if !e.Completed {
			t.Errorf("episode %d should be completed", e.Number)
		}
	}
}

func TestShowProgressExcludesUnairedEpisodes(t *testing.T) {
	db := setupShowProgressDB(t)
	showID := seedShow(t, db, 7004, "Mixed")
	future := time.Now().UTC().AddDate(0, 0, 7)
	ids := seedSeasonWithEpisodes(t, db, showID, 1, []epSeed{{1, airedDaysAgo(2)}, {2, &future}})
	// No aired_at at all: cannot be proven aired, so not counted.
	noDate := seedSeasonWithEpisodes(t, db, showID, 2, []epSeed{{1, nil}})
	_ = noDate

	if err := db.Create(&database.UserHistory{
		UserID: "user-1", MediaType: "episode", MediaID: ids[0], WatchedAt: time.Now().UTC(), Source: "api",
	}).Error; err != nil {
		t.Fatalf("seed history: %v", err)
	}

	got := decodeShowProgress(t, doProgressReq(t, showProgressApp("user-1"), http.MethodGet,
		"/shows/"+strconv.FormatInt(showID, 10)+"/progress/watched"))

	if got.Aired != 1 {
		t.Errorf("aired = %d, want 1 (future and undated episodes excluded)", got.Aired)
	}
	if got.Completed != 1 {
		t.Errorf("completed = %d, want 1", got.Completed)
	}
	if got.NextEpisode == nil {
		t.Fatal("next_episode should be the future episode, not null")
	}
	if got.NextEpisode.IDs.ID != ids[1] {
		t.Errorf("next_episode id = %d, want %d", got.NextEpisode.IDs.ID, ids[1])
	}
	if got.NextEpisode.EpisodeNumber != 2 {
		t.Errorf("next_episode number = %d, want 2", got.NextEpisode.EpisodeNumber)
	}
}

func TestShowProgressCollection(t *testing.T) {
	db := setupShowProgressDB(t)
	showID := seedShow(t, db, 7005, "Collected")
	ids := seedSeasonWithEpisodes(t, db, showID, 1, []epSeed{{1, airedDaysAgo(5)}, {2, airedDaysAgo(4)}, {3, airedDaysAgo(3)}})

	collectedAt := time.Now().UTC().AddDate(0, 0, -2)
	for _, id := range []int64{ids[0], ids[1]} {
		if err := db.Create(&database.UserCollection{
			UserID: "user-1", MediaType: "episode", MediaID: id, CollectedAt: collectedAt,
		}).Error; err != nil {
			t.Fatalf("seed collection: %v", err)
		}
	}
	// A watched-but-not-collected episode must not appear in collection progress.
	if err := db.Create(&database.UserHistory{
		UserID: "user-1", MediaType: "episode", MediaID: ids[2], WatchedAt: time.Now().UTC(), Source: "api",
	}).Error; err != nil {
		t.Fatalf("seed history: %v", err)
	}

	got := decodeShowProgress(t, doProgressReq(t, showProgressApp("user-1"), http.MethodGet,
		"/shows/"+strconv.FormatInt(showID, 10)+"/progress/collection"))

	if got.Aired != 3 || got.Completed != 2 {
		t.Fatalf("aired/completed = %d/%d, want 3/2", got.Aired, got.Completed)
	}
	if got.LastCollectedAt == nil {
		t.Fatal("last_collected_at should be set")
	}
	if got.LastWatchedAt != nil {
		t.Errorf("watched progress fields must not leak into collection progress: %v", *got.LastWatchedAt)
	}
	if len(got.Seasons[0].Episodes) != 2 {
		t.Fatalf("collected episodes = %d, want 2", len(got.Seasons[0].Episodes))
	}
	for _, e := range got.Seasons[0].Episodes {
		if e.CollectedAt == nil {
			t.Errorf("episode %d missing collected_at", e.Number)
		}
	}
}

func TestShowProgressUnknownShowIs404(t *testing.T) {
	setupShowProgressDB(t)
	resp := doProgressReq(t, showProgressApp("user-1"), http.MethodGet, "/shows/999999/progress/watched")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestShowProgressResetOnUnknownShowIs404(t *testing.T) {
	setupShowProgressDB(t)
	resp := doProgressReq(t, showProgressApp("user-1"), http.MethodPost, "/shows/999999/progress/watched/reset")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
