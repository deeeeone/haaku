package sync

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const remUser = "user-rem"

// setupRemainingDB mirrors the other sync handler tests: in-memory sqlite (CI
// runs with no services). UserPlayback carries `DEFAULT now()` in its GORM tag,
// which sqlite lacks, so it is created with portable DDL.
func setupRemainingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(database.MediaModels()...); err != nil {
		t.Fatalf("migrate media: %v", err)
	}
	if err := db.AutoMigrate(database.SyncModels()...); err != nil {
		t.Fatalf("migrate sync: %v", err)
	}
	if err := db.Exec(`CREATE TABLE user_playback (
		user_id TEXT NOT NULL, media_type TEXT NOT NULL, media_id INTEGER NOT NULL,
		progress REAL NOT NULL DEFAULT 0, paused_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (user_id, media_type, media_id))`).Error; err != nil {
		t.Fatalf("create sqlite user_playback: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// newRemainingApp mounts the issue-#106 handlers with a stub auth middleware
// that pins user_id, so the handlers run without a token.
func newRemainingApp(userID string) *fiber.App {
	// The production error handler: a helper that answers 400 and returns
	// response.ErrHandled must not be turned into a 500 by Fiber's default
	// handler (which is what a bare fiber.New() does).
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	v1 := app.Group("/v1")
	v1.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	})
	s := v1.Group("/sync")
	s.Get("/collection/media", handleCollectionMedia)
	s.Get("/collection/minimal/movies", handleCollectionMinimalMovies)
	s.Get("/collection/minimal/shows", handleCollectionMinimalShows)
	s.Get("/collection/minimal/episodes", handleCollectionMinimalEpisodes)
	s.Get("/watched/:type", handleWatchedType)
	s.Get("/progress/up_next", handleUpNext)
	s.Get("/progress/up_next_nitro", handleUpNextNitro)
	s.Get("/progress/watched", handleProgressWatched)
	s.Post("/favorites/reorder", handleReorderFavorites)
	s.Post("/watchlist/reorder", handleReorderWatchlistContract)
	s.Put("/watchlist", handleUpdateWatchlist)
	s.Put("/favorites/:list_item_id", handleUpdateFavoriteItem)
	s.Put("/watchlist/:list_item_id", handleUpdateWatchlistItem)
	s.Delete("/playback/:id", handleDeletePlaybackContract)
	// The native watchlist read, to prove a reorder is reflected.
	s.Get("/watchlist", handleGetWatchlist)
	// The contract reads, so a test can prove the id they return is the row id
	// the write handlers accept.
	s.Get("/watchlist/:type/:sort_by/:sort_how", handleContractWatchlist)
	s.Get("/favorites/:type/:sort_by/:sort_how", handleContractFavorites)
	return app
}

func seedRemaining(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	create := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("seed %T: %v", v, err)
		}
	}
	aired := func(daysAgo int) *database.Date { return &database.Date{Time: now.AddDate(0, 0, -daysAgo)} }

	create(&database.Movie{ID: 10, TmdbID: 603, ImdbID: "tt0133093", Slug: "the-matrix", Title: "The Matrix", Released: aired(9000), Metadata: database.JSONMap{}})
	create(&database.Show{ID: 20, TmdbID: 1399, Slug: "the-show", Title: "The Show", FirstAired: aired(5000), Metadata: database.JSONMap{}})
	create(&database.Season{ID: 5, ShowID: 20, SeasonNumber: 1, EpisodeCount: 3})
	create(&database.Episode{ID: 100, SeasonID: 5, ShowID: 20, EpisodeNumber: 1, Title: "Pilot", AiredAt: aired(3), Runtime: 30})
	create(&database.Episode{ID: 101, SeasonID: 5, ShowID: 20, EpisodeNumber: 2, Title: "Second", AiredAt: aired(2), Runtime: 30})
	create(&database.Episode{ID: 102, SeasonID: 5, ShowID: 20, EpisodeNumber: 3, Title: "Third", AiredAt: aired(1), Runtime: 30})

	seasonID := int64(5)
	epID := int64(100)
	create(&database.UserHistory{UserID: remUser, MediaType: "episode", MediaID: 100, SeasonID: &seasonID, EpisodeID: &epID, WatchedAt: now.Add(-3 * time.Hour), Source: "api"})
	create(&database.UserHistory{UserID: remUser, MediaType: "movie", MediaID: 10, WatchedAt: now.Add(-1 * time.Hour), Source: "api"})

	create(&database.UserCollection{UserID: remUser, MediaType: "movie", MediaID: 10, CollectedAt: now.Add(-10 * time.Hour)})
	create(&database.UserCollection{UserID: remUser, MediaType: "show", MediaID: 20, CollectedAt: now.Add(-9 * time.Hour)})
	create(&database.UserCollection{UserID: remUser, MediaType: "episode", MediaID: 100, CollectedAt: now.Add(-8 * time.Hour)})

	create(&database.UserWatchlist{UserID: remUser, MediaType: "movie", MediaID: 10, Rank: 1, ListedAt: now.Add(-2 * time.Hour)})
	create(&database.UserWatchlist{UserID: remUser, MediaType: "show", MediaID: 20, Rank: 2, ListedAt: now.Add(-1 * time.Hour)})

	create(&database.UserFavorite{UserID: remUser, MediaType: "movie", MediaID: 10, Rank: 1, FavoritedAt: now.Add(-2 * time.Hour)})
	create(&database.UserFavorite{UserID: remUser, MediaType: "show", MediaID: 20, Rank: 2, FavoritedAt: now.Add(-1 * time.Hour)})

	create(&database.UserPlayback{UserID: remUser, MediaType: "movie", MediaID: 10, Progress: 42, PausedAt: now})
}

func doJSON(t *testing.T, app *fiber.App, method, path, body string) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	return res.StatusCode, raw
}

func decodeArray(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode array: %v (%s)", err, raw)
	}
	return out
}

func TestDeletePlaybackContract(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	status, raw := doJSON(t, app, http.MethodDelete, "/v1/sync/playback/10", "")
	if status != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d (%s)", status, raw)
	}
	var n int64
	db.Model(&database.UserPlayback{}).Where("user_id = ?", remUser).Count(&n)
	if n != 0 {
		t.Fatalf("playback row not deleted: %d remain", n)
	}

	// Deleting it again is a 404 (the contract documents 404 for an invalid id).
	status, raw = doJSON(t, app, http.MethodDelete, "/v1/sync/playback/10", "")
	if status != http.StatusNotFound {
		t.Fatalf("second delete: expected 404, got %d (%s)", status, raw)
	}
}

func TestCollectionMedia(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	status, raw := doJSON(t, app, http.MethodGet, "/v1/sync/collection/media", "")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", status, raw)
	}
	items := decodeArray(t, raw)
	byType := map[string]map[string]any{}
	for _, it := range items {
		byType[it["type"].(string)] = it
	}
	if byType["movie"] == nil || byType["show"] == nil || byType["episode"] == nil {
		t.Fatalf("expected movie, show and episode items, got %v", byType)
	}
	m, _ := byType["movie"]["movie"].(map[string]any)
	if m == nil || m["title"] != "The Matrix" {
		t.Errorf("movie item missing its media object: %v", byType["movie"])
	}
	if byType["movie"]["collected_at"] == nil {
		t.Errorf("movie item missing collected_at")
	}

	// A narrowing filter we cannot honour is rejected, not ignored.
	status, _ = doJSON(t, app, http.MethodGet, "/v1/sync/collection/media?available_on=netflix", "")
	if status != http.StatusBadRequest {
		t.Errorf("available_on: expected 400, got %d", status)
	}
}

func TestCollectionMinimal(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	status, raw := doJSON(t, app, http.MethodGet, "/v1/sync/collection/minimal/movies", "")
	if status != http.StatusOK {
		t.Fatalf("movies: expected 200, got %d (%s)", status, raw)
	}
	var movies map[string]string
	if err := json.Unmarshal(raw, &movies); err != nil {
		t.Fatalf("decode movies: %v (%s)", err, raw)
	}
	if movies["10"] == "" {
		t.Errorf("minimal movies missing collected movie 10: %v", movies)
	}

	status, raw = doJSON(t, app, http.MethodGet, "/v1/sync/collection/minimal/episodes", "")
	if status != http.StatusOK {
		t.Fatalf("episodes: expected 200, got %d (%s)", status, raw)
	}
	var eps map[string]string
	if err := json.Unmarshal(raw, &eps); err != nil {
		t.Fatalf("decode episodes: %v (%s)", err, raw)
	}
	if eps["100"] == "" {
		t.Errorf("minimal episodes missing collected episode 100: %v", eps)
	}

	status, raw = doJSON(t, app, http.MethodGet, "/v1/sync/collection/minimal/shows", "")
	if status != http.StatusOK {
		t.Fatalf("shows: expected 200, got %d (%s)", status, raw)
	}
	var shows map[string]map[string]map[string]string
	if err := json.Unmarshal(raw, &shows); err != nil {
		t.Fatalf("decode shows: %v (%s)", err, raw)
	}
	if shows["20"]["1"]["1"] == "" {
		t.Errorf("minimal shows missing show 20 season 1 episode 1: %v", shows)
	}
}

func TestUpNextAdvancesAndEmptiesWhenCaughtUp(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	nextNumber := func() int {
		t.Helper()
		status, raw := doJSON(t, app, http.MethodGet, "/v1/sync/progress/up_next", "")
		if status != http.StatusOK {
			t.Fatalf("up_next: expected 200, got %d (%s)", status, raw)
		}
		items := decodeArray(t, raw)
		if len(items) == 0 {
			return 0
		}
		prog := items[0]["progress"].(map[string]any)
		ne, _ := prog["next_episode"].(map[string]any)
		if ne == nil {
			return 0
		}
		return int(ne["number"].(float64))
	}

	// Episode 1 watched: next is episode 2.
	if got := nextNumber(); got != 2 {
		t.Fatalf("after episode 1, next_episode.number = %d, want 2", got)
	}

	// Watch episode 2: up_next advances to episode 3.
	seasonID := int64(5)
	epID := int64(101)
	if err := db.Create(&database.UserHistory{UserID: remUser, MediaType: "episode", MediaID: 101, SeasonID: &seasonID, EpisodeID: &epID, WatchedAt: time.Now().UTC(), Source: "api"}).Error; err != nil {
		t.Fatalf("seed episode 2: %v", err)
	}
	if got := nextNumber(); got != 3 {
		t.Fatalf("after episode 2, next_episode.number = %d, want 3", got)
	}

	// Watch episode 3: the show is caught up, so up_next answers empty.
	epID = 102
	if err := db.Create(&database.UserHistory{UserID: remUser, MediaType: "episode", MediaID: 102, SeasonID: &seasonID, EpisodeID: &epID, WatchedAt: time.Now().UTC(), Source: "api"}).Error; err != nil {
		t.Fatalf("seed episode 3: %v", err)
	}
	status, raw := doJSON(t, app, http.MethodGet, "/v1/sync/progress/up_next", "")
	if status != http.StatusOK {
		t.Fatalf("caught up: expected 200, got %d (%s)", status, raw)
	}
	if items := decodeArray(t, raw); len(items) != 0 {
		t.Fatalf("caught-up show still in up_next: %v", items)
	}
}

func TestUpNextNitroIntentAndFilters(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	// intent=continue: one of three episodes watched, so it is in progress.
	status, raw := doJSON(t, app, http.MethodGet, "/v1/sync/progress/up_next_nitro?intent=continue", "")
	if status != http.StatusOK {
		t.Fatalf("intent=continue: expected 200, got %d (%s)", status, raw)
	}
	if items := decodeArray(t, raw); len(items) != 1 {
		t.Fatalf("intent=continue: expected 1 in-progress show, got %d", len(items))
	}

	// A catalogue filter is applied, not ignored: the seeded show carries no
	// genres, so filtering by one narrows it out of the result set.
	status, raw = doJSON(t, app, http.MethodGet, "/v1/sync/progress/up_next_nitro?intent=continue&genres=drama", "")
	if status != http.StatusOK {
		t.Fatalf("genres filter: expected 200, got %d (%s)", status, raw)
	}
	if items := decodeArray(t, raw); len(items) != 0 {
		t.Errorf("genres=drama should narrow out the genreless show, got %d items", len(items))
	}

	// A declared filter this store cannot evaluate is ignored, not refused: the
	// request succeeds with the superset and the parity row is `divergent`
	// naming the parameter. Refusing it (the #130 behaviour) returned nothing at
	// all to a client that sent a documented parameter.
	status, raw = doJSON(t, app, http.MethodGet, "/v1/sync/progress/up_next_nitro?countries=us", "")
	if status != http.StatusOK {
		t.Errorf("countries filter: expected 200 (ignored), got %d (%s)", status, raw)
	} else if items := decodeArray(t, raw); len(items) != 1 {
		t.Errorf("countries filter should be ignored, want 1 show, got %d", len(items))
	}
	// runtimes is not a shows column, so it is ignored for the same reason.
	status, raw = doJSON(t, app, http.MethodGet, "/v1/sync/progress/up_next_nitro?runtimes=30-60", "")
	if status != http.StatusOK {
		t.Errorf("runtimes filter (not a shows column): expected 200 (ignored), got %d (%s)", status, raw)
	} else if items := decodeArray(t, raw); len(items) != 1 {
		t.Errorf("runtimes filter should be ignored, want 1 show, got %d", len(items))
	}
}

func TestReorderWatchlistWritesRealRanks(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	// The item id a read returns is the list row's own id, not its media id.
	movieRow, showRow := watchlistRows(t, db)

	// rank [show, movie] puts the show first. 999 is not owned and must be
	// reported as skipped, never ranked.
	status, raw := doJSON(t, app, http.MethodPost, "/v1/sync/watchlist/reorder",
		`{"rank":[`+itoa(showRow)+`,`+itoa(movieRow)+`,999]}`)
	if status != http.StatusOK {
		t.Fatalf("reorder: expected 200, got %d (%s)", status, raw)
	}
	var resp reorderResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode reorder: %v (%s)", err, raw)
	}
	if resp.Updated != 2 || resp.List.ItemCount != 2 || len(resp.SkippedIDs) != 1 || resp.SkippedIDs[0] != 999 {
		t.Fatalf("reorder response = %+v, want updated 2, item_count 2, skipped [999]", resp)
	}

	// The native watchlist read (rank asc) reflects the new order.
	status, raw = doJSON(t, app, http.MethodGet, "/v1/sync/watchlist?sort_by=rank&sort_how=asc", "")
	if status != http.StatusOK {
		t.Fatalf("get watchlist: expected 200, got %d (%s)", status, raw)
	}
	var body struct {
		Items []struct {
			MediaID   int64  `json:"media_id"`
			Rank      int    `json:"rank"`
			MediaType string `json:"media_type"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode watchlist: %v (%s)", err, raw)
	}
	if len(body.Items) != 2 || body.Items[0].MediaID != 20 || body.Items[0].Rank != 1 {
		t.Fatalf("watchlist order after reorder = %+v, want show 20 at rank 1 first", body.Items)
	}
}

func TestReorderFavoritesWritesRealRanks(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	movieRow, showRow := favoriteRows(t, db)
	status, raw := doJSON(t, app, http.MethodPost, "/v1/sync/favorites/reorder",
		`{"rank":[`+itoa(showRow)+`,`+itoa(movieRow)+`]}`)
	if status != http.StatusOK {
		t.Fatalf("reorder favorites: expected 200, got %d (%s)", status, raw)
	}
	var resp reorderResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	if resp.Updated != 2 || resp.List.ItemCount != 2 {
		t.Fatalf("favorites reorder response = %+v", resp)
	}
	var show database.UserFavorite
	db.Where("user_id = ? AND media_id = ?", remUser, 20).First(&show)
	if show.Rank != 1 {
		t.Errorf("favorite show rank = %d, want 1", show.Rank)
	}
}

func TestWatchedType(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	cases := map[string]string{"movies": "movie", "episodes": "episode", "shows": "show", "seasons": "season"}
	for seg, wantType := range cases {
		status, raw := doJSON(t, app, http.MethodGet, "/v1/sync/watched/"+seg, "")
		if status != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d (%s)", seg, status, raw)
		}
		items := decodeArray(t, raw)
		if len(items) != 1 {
			t.Fatalf("%s: expected 1 item, got %d (%s)", seg, len(items), raw)
		}
		if items[0]["type"] != wantType {
			t.Errorf("%s: type = %v, want %s", seg, items[0]["type"], wantType)
		}
		if plays, _ := items[0]["plays"].(float64); plays < 1 {
			t.Errorf("%s: plays = %v, want >= 1", seg, items[0]["plays"])
		}
	}

	status, _ := doJSON(t, app, http.MethodGet, "/v1/sync/watched/bogus", "")
	if status != http.StatusBadRequest {
		t.Errorf("invalid type: expected 400, got %d", status)
	}
}

func TestProgressWatched(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	status, raw := doJSON(t, app, http.MethodGet, "/v1/sync/progress/watched", "")
	if status != http.StatusOK {
		t.Fatalf("watched: expected 200, got %d (%s)", status, raw)
	}
	items := decodeArray(t, raw)
	if len(items) != 1 {
		t.Fatalf("watched: expected 1 show, got %d (%s)", len(items), raw)
	}
	prog := items[0]["progress"].(map[string]any)
	if int(prog["aired"].(float64)) != 3 || int(prog["completed"].(float64)) != 1 {
		t.Fatalf("watched progress = aired %v completed %v, want 3/1", prog["aired"], prog["completed"])
	}

	// hide_completed excludes nothing here (the show is 1/3 complete), but
	// hide_not_completed also keeps it; both are honoured.
	status, raw = doJSON(t, app, http.MethodGet, "/v1/sync/progress/watched?hide_completed=true", "")
	if status != http.StatusOK {
		t.Fatalf("hide_completed: expected 200, got %d (%s)", status, raw)
	}
	if items := decodeArray(t, raw); len(items) != 1 {
		t.Errorf("hide_completed: incomplete show should remain, got %d", len(items))
	}

	// Rewatch counts are not stored, so only_rewatching is rejected.
	status, _ = doJSON(t, app, http.MethodGet, "/v1/sync/progress/watched?only_rewatching=true", "")
	if status != http.StatusBadRequest {
		t.Errorf("only_rewatching: expected 400, got %d", status)
	}
}

func TestUpdateWatchlistItem(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	movieRow, _ := watchlistRows(t, db)

	status, raw := doJSON(t, app, http.MethodPut, "/v1/sync/watchlist/"+itoa(movieRow), `{"rank":5,"notes":"later"}`)
	if status != http.StatusNoContent {
		t.Fatalf("update watchlist item: expected 204, got %d (%s)", status, raw)
	}
	var row database.UserWatchlist
	db.Where("user_id = ? AND id = ?", remUser, movieRow).First(&row)
	if row.Rank != 5 || row.Notes == nil || *row.Notes != "later" {
		t.Errorf("watchlist row after update = rank %d notes %v", row.Rank, row.Notes)
	}

	status, _ = doJSON(t, app, http.MethodPut, "/v1/sync/watchlist/9999", `{"rank":1}`)
	if status != http.StatusNotFound {
		t.Errorf("unknown item: expected 404, got %d", status)
	}
}

func TestUpdateFavoriteItem(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	movieRow, _ := favoriteRows(t, db)

	status, raw := doJSON(t, app, http.MethodPut, "/v1/sync/favorites/"+itoa(movieRow), `{"rank":3}`)
	if status != http.StatusNoContent {
		t.Fatalf("update favorite: expected 204, got %d (%s)", status, raw)
	}

	// Favorites have no notes column; a notes field is rejected, not ignored.
	status, _ = doJSON(t, app, http.MethodPut, "/v1/sync/favorites/"+itoa(movieRow), `{"notes":"n"}`)
	if status != http.StatusBadRequest {
		t.Errorf("favorite notes: expected 400, got %d", status)
	}
}

func TestUpdateWatchlistAddsMissing(t *testing.T) {
	db := setupRemainingDB(t)
	seedRemaining(t, db)
	app := newRemainingApp(remUser)

	// Movie 10 (tmdb 603) is already on the watchlist; movie 11 is new.
	if err := db.Create(&database.Movie{ID: 11, TmdbID: 604, Slug: "new-movie", Title: "New Movie", Metadata: database.JSONMap{}}).Error; err != nil {
		t.Fatalf("seed movie 11: %v", err)
	}

	status, raw := doJSON(t, app, http.MethodPut, "/v1/sync/watchlist",
		`{"movies":[{"ids":{"tmdb":603}},{"ids":{"tmdb":604}}]}`)
	if status != http.StatusOK {
		t.Fatalf("put watchlist: expected 200, got %d (%s)", status, raw)
	}
	var resp watchlistUpdateResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	if resp.Added.Movies != 1 || resp.Existing.Movies != 1 {
		t.Fatalf("added/existing = %d/%d, want 1/1 (%s)", resp.Added.Movies, resp.Existing.Movies, raw)
	}

	// Seasons/episodes are not storable and are rejected rather than dropped.
	status, _ = doJSON(t, app, http.MethodPut, "/v1/sync/watchlist", `{"seasons":[{"ids":{"tmdb":1}}]}`)
	if status != http.StatusBadRequest {
		t.Errorf("seasons: expected 400, got %d", status)
	}
}
