package progress

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB opens an in-memory sqlite DB, migrates the tables the playback
// handlers query, and points the database singleton at it via the test-only
// SetDBForTest seam. Each call gets a fresh DB; the seam's cleanup resets the
// singleton to nil when the test ends.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	models := []interface{}{
		&database.Show{}, &database.Season{}, &database.Episode{},
		&database.Movie{}, &database.ExternalID{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	// UserPlayback has no primary-key field on the model (its prod `id`
	// column comes from the SQL migrations), which GORM's sqlite migrator
	// cannot auto-create. Reproduce the real table shape directly so the
	// playback handlers can query/order by up.id.
	if err := db.Exec(`CREATE TABLE user_playback (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id text NOT NULL,
		media_type text NOT NULL,
		media_id integer NOT NULL,
		progress real NOT NULL DEFAULT 0,
		paused_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT uq_playback_user_media UNIQUE (user_id, media_type, media_id)
	)`).Error; err != nil {
		t.Fatalf("create user_playback table: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// seedMoviePlayback creates two movies plus playback rows for userID and one
// row for another user (to assert the per-user filter).
func seedMoviePlayback(t *testing.T, db *gorm.DB, userID string) {
	t.Helper()
	now := time.Now().UTC()
	movies := []database.Movie{
		{Title: "Inception", TmdbID: 27205, CreatedAt: now, UpdatedAt: now},
		{Title: "Interstellar", TmdbID: 157336, CreatedAt: now, UpdatedAt: now},
	}
	for i := range movies {
		if err := db.Create(&movies[i]).Error; err != nil {
			t.Fatalf("seed movie: %v", err)
		}
	}
	rows := []database.UserPlayback{
		{UserID: userID, MediaType: "movie", MediaID: movies[0].ID, Progress: 0.5, PausedAt: now},
		{UserID: userID, MediaType: "movie", MediaID: movies[1].ID, Progress: 0.9, PausedAt: now.Add(time.Second)},
		{UserID: "other-user", MediaType: "movie", MediaID: movies[0].ID, Progress: 0.1, PausedAt: now},
	}
	for _, r := range rows {
		if err := db.Create(&r).Error; err != nil {
			t.Fatalf("seed movie playback: %v", err)
		}
	}
}

// seedEpisodePlayback creates a show -> season -> episode plus an episode
// playback row for userID and one for another user.
func seedEpisodePlayback(t *testing.T, db *gorm.DB, userID string) {
	t.Helper()
	now := time.Now().UTC()

	show := database.Show{Title: "Breaking Go", TmdbID: 9001, ImdbID: "tt9001"}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	season := database.Season{ShowID: show.ID, SeasonNumber: 2}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	episode := database.Episode{
		SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: 3,
		Title: "The Empty Interface",
	}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatalf("seed episode: %v", err)
	}

	rows := []database.UserPlayback{
		{UserID: userID, MediaType: "episode", MediaID: episode.ID, Progress: 0.42, PausedAt: now},
		{UserID: "other-user", MediaType: "episode", MediaID: episode.ID, Progress: 0.9, PausedAt: now},
	}
	for _, r := range rows {
		if err := db.Create(&r).Error; err != nil {
			t.Fatalf("seed episode playback: %v", err)
		}
	}
}

// makeAuthedApp registers the playback handlers behind a stub middleware that
// injects userID into ctx locals (bypassing Clerk so the handler body runs).
func makeAuthedApp(userID string) *fiber.App {
	app := fiber.New()
	stub := func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	}
	sync := app.Group("/sync/playback")
	sync.Get("/movies", stub, handlePlaybackMovies)
	sync.Get("/episodes", stub, handlePlaybackEpisodes)
	return app
}

// makeRealApp registers the playback routes exactly as production does, with
// the shared auth.AuthRequired() middleware, so the 401 guard can be asserted.
func makeRealApp() *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1")
	RegisterRoutes(v1)
	return app
}

func doGET(t *testing.T, app *fiber.App, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("build request %s: %v", path, err)
	}
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s failed: %v", path, err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func assertPagination(t *testing.T, resp *http.Response, page, limit, totalPages, totalItems, itemsCount string) {
	t.Helper()
	for _, h := range []struct {
		key, want string
	}{
		{"X-Pagination-Page", page},
		{"X-Pagination-Limit", limit},
		{"X-Pagination-Total-Pages", totalPages},
		{"X-Pagination-Total-Items", totalItems},
		{"X-Pagination-Items-Count", itemsCount},
	} {
		if got := resp.Header.Get(h.key); got != h.want {
			t.Errorf("header %s = %q, want %q", h.key, got, h.want)
		}
	}
}

func TestPlaybackMovies(t *testing.T) {
	db := setupTestDB(t)
	seedMoviePlayback(t, db, "user-1")
	app := makeAuthedApp("user-1")

	t.Run("200 returns only current user's rows with pagination headers", func(t *testing.T) {
		resp := doGET(t, app, "/sync/playback/movies")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		assertPagination(t, resp, "1", "25", "1", "2", "2")
		var body []struct {
			Progress float64 `json:"progress"`
			Movie    struct {
				Title string `json:"title"`
			} `json:"movie"`
		}
		decodeBody(t, resp, &body)
		if len(body) != 2 {
			t.Fatalf("expected 2 movies, got %d", len(body))
		}
		if body[0].Movie.Title == "" || body[1].Movie.Title == "" {
			t.Errorf("expected enriched movie titles, got %+v", body)
		}
	})
}

// TestPlaybackMoviesDBUnavailable exercises the GetDB() failure path. It must
// be a standalone top-level test that does NOT call setupTestDB: every other
// test's SetDBForTest cleanup resets the singleton to nil, so the DB is
// guaranteed unavailable here and the handler returns its 500 internal path.
func TestPlaybackMoviesDBUnavailable(t *testing.T) {
	app := makeAuthedApp("user-1")
	resp := doGET(t, app, "/sync/playback/movies")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}

func TestPlaybackEpisodes(t *testing.T) {
	db := setupTestDB(t)
	seedEpisodePlayback(t, db, "user-1")
	app := makeAuthedApp("user-1")

	t.Run("200 returns joined episode+show info with pagination headers", func(t *testing.T) {
		resp := doGET(t, app, "/sync/playback/episodes")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		assertPagination(t, resp, "1", "25", "1", "1", "1")
		var body []struct {
			Progress float64 `json:"progress"`
			Episode  struct {
				Title  string `json:"title"`
				Season int    `json:"season"`
				Number int    `json:"number"`
			} `json:"episode"`
			Show struct {
				Title string `json:"title"`
			} `json:"show"`
		}
		decodeBody(t, resp, &body)
		if len(body) != 1 {
			t.Fatalf("expected 1 episode, got %d", len(body))
		}
		got := body[0]
		if got.Episode.Title != "The Empty Interface" || got.Episode.Season != 2 || got.Episode.Number != 3 {
			t.Errorf("unexpected episode payload: %+v", got.Episode)
		}
		if got.Show.Title != "Breaking Go" {
			t.Errorf("expected show title, got %q", got.Show.Title)
		}
		if got.Progress != 0.42 {
			t.Errorf("expected progress 0.42, got %v", got.Progress)
		}
	})
}

// TestPlaybackEpisodesDBUnavailable is the episode analogue of
// TestPlaybackMoviesDBUnavailable (see that comment for why it is standalone).
func TestPlaybackEpisodesDBUnavailable(t *testing.T) {
	app := makeAuthedApp("user-1")
	resp := doGET(t, app, "/sync/playback/episodes")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}

func TestPlaybackRequiresAuth(t *testing.T) {
	app := makeRealApp()

	t.Run("movies without token is 401", func(t *testing.T) {
		resp := doGET(t, app, "/v1/sync/playback/movies")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("episodes without token is 401", func(t *testing.T) {
		resp := doGET(t, app, "/v1/sync/playback/episodes")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

// seedMoviePlaybackN creates n movies, each with a playback row owned by userID
// and strictly increasing paused_at, so paused_at DESC ordering is deterministic.
func seedMoviePlaybackN(t *testing.T, db *gorm.DB, userID string, n int) {
	t.Helper()
	base := time.Now().UTC()
	for i := 0; i < n; i++ {
		m := database.Movie{
			Title: "Movie " + strconv.Itoa(i), TmdbID: 1000 + i,
			CreatedAt: base, UpdatedAt: base,
		}
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("seed movie: %v", err)
		}
		row := database.UserPlayback{
			UserID: userID, MediaType: "movie", MediaID: m.ID,
			Progress: float64(i) / 10,
			PausedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed movie playback: %v", err)
		}
	}
}

// seedEpisodePlaybackN creates one show + season with n episodes, each with a
// playback row owned by userID and paused_at increasing with the episode number.
func seedEpisodePlaybackN(t *testing.T, db *gorm.DB, userID string, n int) {
	t.Helper()
	base := time.Now().UTC()
	show := database.Show{Title: "Breaking Go Paged", TmdbID: 9100, ImdbID: "tt9100"}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	season := database.Season{ShowID: show.ID, SeasonNumber: 3}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	for i := 0; i < n; i++ {
		ep := database.Episode{
			SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: i + 1,
			Title: "Paged " + strconv.Itoa(i),
		}
		if err := db.Create(&ep).Error; err != nil {
			t.Fatalf("seed episode: %v", err)
		}
		row := database.UserPlayback{
			UserID: userID, MediaType: "episode", MediaID: ep.ID,
			Progress: float64(i) / 10,
			PausedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed episode playback: %v", err)
		}
	}
}

// TestPlaybackMoviesPagination drives the movies endpoint's page/limit handling:
// defaults, the next-page Link, the tail page, an out-of-range page (empty page,
// totals intact), the 100 limit cap, and invalid-input fallback.
func TestPlaybackMoviesPagination(t *testing.T) {
	db := setupTestDB(t)
	seedMoviePlaybackN(t, db, "user-1", 3)
	app := makeAuthedApp("user-1")

	cases := []struct {
		name                                                                string
		query                                                               string
		wantPage, wantLimit, wantTotalPages, wantTotalItems, wantItemsCount int
		wantNextLink                                                        string
	}{
		{"defaults", "", 1, 25, 1, 3, 3, ""},
		{"page 1 limit 2 advertises a next link", "?page=1&limit=2", 1, 2, 2, 3, 2, `</sync/playback/movies?page=2&limit=2>; rel="next"`},
		{"page 2 limit 2 returns the tail row", "?page=2&limit=2", 2, 2, 2, 3, 1, ""},
		{"out-of-range page is empty but totals hold", "?page=99&limit=2", 99, 2, 2, 3, 0, ""},
		{"limit above the 100 cap clamps", "?limit=1000", 1, 100, 1, 3, 3, ""},
		{"non-positive/invalid page and limit fall back", "?page=0&limit=abc", 1, 25, 1, 3, 3, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doGET(t, app, "/sync/playback/movies"+tc.query)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
			assertPagination(t, resp,
				itoa(tc.wantPage), itoa(tc.wantLimit), itoa(tc.wantTotalPages),
				itoa(tc.wantTotalItems), itoa(tc.wantItemsCount))
			if got := resp.Header.Get("Link"); got != tc.wantNextLink {
				t.Errorf("Link = %q, want %q", got, tc.wantNextLink)
			}
			var body []json.RawMessage
			decodeBody(t, resp, &body)
			if len(body) != tc.wantItemsCount {
				t.Errorf("body rows = %d, want %d", len(body), tc.wantItemsCount)
			}
		})
	}
}

// TestPlaybackEpisodesPagination is the episode analogue of
// TestPlaybackMoviesPagination (same cases, episode+show join).
func TestPlaybackEpisodesPagination(t *testing.T) {
	db := setupTestDB(t)
	seedEpisodePlaybackN(t, db, "user-1", 3)
	app := makeAuthedApp("user-1")

	cases := []struct {
		name                                                                string
		query                                                               string
		wantPage, wantLimit, wantTotalPages, wantTotalItems, wantItemsCount int
		wantNextLink                                                        string
	}{
		{"defaults", "", 1, 25, 1, 3, 3, ""},
		{"page 1 limit 2 advertises a next link", "?page=1&limit=2", 1, 2, 2, 3, 2, `</sync/playback/episodes?page=2&limit=2>; rel="next"`},
		{"page 2 limit 2 returns the tail row", "?page=2&limit=2", 2, 2, 2, 3, 1, ""},
		{"out-of-range page is empty but totals hold", "?page=99&limit=2", 99, 2, 2, 3, 0, ""},
		{"limit above the 100 cap clamps", "?limit=1000", 1, 100, 1, 3, 3, ""},
		{"non-positive/invalid page and limit fall back", "?page=0&limit=abc", 1, 25, 1, 3, 3, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doGET(t, app, "/sync/playback/episodes"+tc.query)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
			assertPagination(t, resp,
				itoa(tc.wantPage), itoa(tc.wantLimit), itoa(tc.wantTotalPages),
				itoa(tc.wantTotalItems), itoa(tc.wantItemsCount))
			if got := resp.Header.Get("Link"); got != tc.wantNextLink {
				t.Errorf("Link = %q, want %q", got, tc.wantNextLink)
			}
			var body []json.RawMessage
			decodeBody(t, resp, &body)
			if len(body) != tc.wantItemsCount {
				t.Errorf("body rows = %d, want %d", len(body), tc.wantItemsCount)
			}
		})
	}
}

// TestPlaybackQueryErrors asserts the 500 branch each playback handler takes
// when GORM returns a real query error — distinct from the GetDB() failure
// already covered by TestPlaybackMoviesDBUnavailable. Dropping the table a query
// needs is the portable way to force a non-not-found error on sqlite.
func TestPlaybackQueryErrors(t *testing.T) {
	seedMovies := func(t *testing.T, db *gorm.DB) { seedMoviePlaybackN(t, db, "user-1", 3) }
	seedEpisodes := func(t *testing.T, db *gorm.DB) { seedEpisodePlaybackN(t, db, "user-1", 3) }

	cases := []struct {
		name string
		path string
		seed func(t *testing.T, db *gorm.DB)
		drop string
	}{
		{"movies count query fails", "/sync/playback/movies", seedMovies, "user_playback"},
		{"movies batch-fetch fails", "/sync/playback/movies", seedMovies, "movies"},
		{"episodes count query fails", "/sync/playback/episodes", seedEpisodes, "user_playback"},
		{"episodes triple join fails", "/sync/playback/episodes", seedEpisodes, "seasons"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			tc.seed(t, db)
			if err := db.Exec("DROP TABLE " + tc.drop).Error; err != nil {
				t.Fatalf("drop %s: %v", tc.drop, err)
			}
			app := makeAuthedApp("user-1")
			resp := doGET(t, app, tc.path)
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("expected 500, got %d", resp.StatusCode)
			}
		})
	}
}
