package episodes

import (
	"encoding/json"
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

// setupTestDB opens an in-memory sqlite DB, migrates the tables the episode
// handlers query, and points the database package singleton at it via the
// test-only SetDBForTest seam so GetDB() returns this handle.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	models := []interface{}{
		&database.Show{}, &database.Season{}, &database.Episode{},
		&database.ExternalID{},
		&database.UserHistory{}, &database.UserRating{},
		&database.Comment{}, &database.List{}, &database.ListItem{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// seedEpisodeFixture creates a show -> season -> episode and the history /
// rating / list rows the endpoints aggregate, returning the episode id.
func seedEpisodeFixture(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	now := time.Now().UTC()

	show := database.Show{Title: "Breaking Go", TmdbID: 9001, SeasonCount: 1, EpisodeCount: 1}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	season := database.Season{ShowID: show.ID, SeasonNumber: 1, EpisodeCount: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	episode := database.Episode{
		SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: 1,
		Title: "The Empty Interface", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatalf("seed episode: %v", err)
	}

	// Stats input: 2 distinct watchers, 3 plays.
	for _, u := range []string{"user-a", "user-a", "user-b"} {
		h := database.UserHistory{
			UserID: u, MediaType: "episode", MediaID: episode.ID,
			WatchedAt: now, Source: "api", CreatedAt: now,
		}
		if err := db.Create(&h).Error; err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}
	// Ratings input: user-a=7, user-b=9 -> avg 8, votes 2.
	seedRating := func(u string, rating int) {
		r := database.UserRating{
			UserID: u, MediaType: "episode", MediaID: episode.ID,
			Rating: rating, RatedAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&r).Error; err != nil {
			t.Fatalf("seed rating: %v", err)
		}
	}
	seedRating("user-a", 7)
	seedRating("user-b", 9)

	// Lists input: one public (visible) and one private (filtered out).
	lists := []database.List{
		{UserID: "list-owner", Name: "Public Episodes", Slug: "public-episodes", Privacy: "public", CreatedAt: now, UpdatedAt: now},
		{UserID: "list-owner", Name: "Private Episodes", Slug: "private-episodes", Privacy: "private", CreatedAt: now, UpdatedAt: now},
	}
	for _, l := range lists {
		if err := db.Create(&l).Error; err != nil {
			t.Fatalf("seed list: %v", err)
		}
		item := database.ListItem{
			ListID: l.ID, MediaType: "episode", MediaID: episode.ID,
			Rank: 1, AddedBy: "list-owner", AddedAt: now,
		}
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("seed list item: %v", err)
		}
	}

	// Comments input: two comments on the episode (paginated endpoint).
	for _, body := range []string{"first take", "second take"} {
		cm := database.Comment{
			UserID: "user-a", MediaType: "episode",
			CommentType: "shout", Body: body, CreatedAt: now, UpdatedAt: now,
		}
		mid := episode.ID
		cm.MediaID = &mid
		if err := db.Create(&cm).Error; err != nil {
			t.Fatalf("seed comment: %v", err)
		}
	}

	return episode.ID
}

func makeEpisodesApp() *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
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

// decodeBody unmarshals the response body, failing the test on any read/parse error.
func decodeBody(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func TestGetEpisodeHandler(t *testing.T) {
	db := setupTestDB(t)
	epID := seedEpisodeFixture(t, db)
	app := makeEpisodesApp()

	t.Run("200 detail with season, show, stats", func(t *testing.T) {
		resp := doGET(t, app, "/v1/episodes/"+itoa(epID))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var body struct {
			SeasonID      int64  `json:"season_id"`
			ShowID        int64  `json:"show_id"`
			EpisodeNumber int    `json:"episode_number"`
			Title         string `json:"title"`
			Season        *struct {
				SeasonNumber int `json:"season_number"`
			} `json:"season"`
			Show *struct {
				Title string `json:"title"`
			} `json:"show"`
			Stats *struct {
				Watchers int64   `json:"watchers"`
				Plays    int64   `json:"plays"`
				Rating   float64 `json:"rating"`
			} `json:"stats"`
		}
		decodeBody(t, resp, &body)
		if body.Title != "The Empty Interface" {
			t.Errorf("expected title, got %q", body.Title)
		}
		if body.Season == nil || body.Season.SeasonNumber != 1 {
			t.Errorf("expected embedded season number 1, got %+v", body.Season)
		}
		if body.Show == nil || body.Show.Title != "Breaking Go" {
			t.Errorf("expected embedded show, got %+v", body.Show)
		}
		if body.Stats == nil || body.Stats.Watchers != 2 || body.Stats.Plays != 3 {
			t.Errorf("expected watchers=2 plays=3, got %+v", body.Stats)
		}
	})

	t.Run("400 for non-numeric id", func(t *testing.T) {
		resp := doGET(t, app, "/v1/episodes/abc")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
		var body struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		decodeBody(t, resp, &body)
		if body.Error.Code != "invalid_request" {
			t.Errorf("error code = %q, want invalid_request", body.Error.Code)
		}
		if body.Error.Message != "invalid episode id" {
			t.Errorf("error message = %q, want %q", body.Error.Message, "invalid episode id")
		}
	})

	t.Run("404 for missing episode", func(t *testing.T) {
		resp := doGET(t, app, "/v1/episodes/999999")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}

func TestGetEpisodeStats(t *testing.T) {
	db := setupTestDB(t)
	epID := seedEpisodeFixture(t, db)
	app := makeEpisodesApp()

	t.Run("200 aggregates watchers/plays/rating", func(t *testing.T) {
		resp := doGET(t, app, "/v1/episodes/"+itoa(epID)+"/stats")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var body struct {
			Watchers int64   `json:"watchers"`
			Plays    int64   `json:"plays"`
			Rating   float64 `json:"rating"`
		}
		decodeBody(t, resp, &body)
		if body.Watchers != 2 || body.Plays != 3 {
			t.Errorf("expected watchers=2 plays=3, got %+v", body)
		}
		if body.Rating != 8 {
			t.Errorf("expected avg rating 8, got %v", body.Rating)
		}
	})

	t.Run("400 for non-numeric id", func(t *testing.T) {
		resp := doGET(t, app, "/v1/episodes/nope/stats")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("404 for missing episode", func(t *testing.T) {
		resp := doGET(t, app, "/v1/episodes/555/stats")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}

func TestGetEpisodeRatings(t *testing.T) {
	db := setupTestDB(t)
	epID := seedEpisodeFixture(t, db)
	app := makeEpisodesApp()

	t.Run("200 distribution from seeded ratings", func(t *testing.T) {
		resp := doGET(t, app, "/v1/episodes/"+itoa(epID)+"/ratings")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var body struct {
			Rating       float64        `json:"rating"`
			Votes        int64          `json:"votes"`
			Distribution map[string]int `json:"distribution"`
		}
		decodeBody(t, resp, &body)
		if body.Votes != 2 {
			t.Errorf("expected votes 2, got %d", body.Votes)
		}
		if body.Distribution["7"] != 1 || body.Distribution["9"] != 1 {
			t.Errorf("expected dist {7:1,9:1}, got %v", body.Distribution)
		}
	})

	t.Run("404 for missing episode", func(t *testing.T) {
		resp := doGET(t, app, "/v1/episodes/777/ratings")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}

func TestGetEpisodeLists(t *testing.T) {
	db := setupTestDB(t)
	epID := seedEpisodeFixture(t, db)
	app := makeEpisodesApp()

	t.Run("200 returns only visible lists with pagination counts", func(t *testing.T) {
		resp := doGET(t, app, "/v1/episodes/"+itoa(epID)+"/lists")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var body struct {
			Data []struct {
				List struct {
					Name    string `json:"name"`
					Privacy string `json:"privacy"`
				} `json:"list"`
			} `json:"data"`
			TotalCount int `json:"total_count"`
		}
		decodeBody(t, resp, &body)
		if len(body.Data) != 1 || body.Data[0].List.Name != "Public Episodes" {
			t.Errorf("expected only the public list, got %+v", body.Data)
		}
		if body.TotalCount != 1 {
			t.Errorf("expected total_count 1, got %d", body.TotalCount)
		}
	})

	t.Run("404 for missing episode", func(t *testing.T) {
		resp := doGET(t, app, "/v1/episodes/31337/lists")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}

func TestGetEpisodeComments(t *testing.T) {
	db := setupTestDB(t)
	epID := seedEpisodeFixture(t, db)
	app := makeEpisodesApp()

	t.Run("200 returns seeded comments", func(t *testing.T) {
		resp := doGET(t, app, "/v1/episodes/"+itoa(epID)+"/comments")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var body struct {
			Data []struct {
				Body string `json:"body"`
			} `json:"data"`
			TotalCount int `json:"total_count"`
		}
		decodeBody(t, resp, &body)
		if len(body.Data) != 2 {
			t.Errorf("expected 2 comments, got %d", len(body.Data))
		}
		if body.TotalCount != 2 {
			t.Errorf("expected total_count 2, got %d", body.TotalCount)
		}
	})

	t.Run("404 for missing episode", func(t *testing.T) {
		resp := doGET(t, app, "/v1/episodes/424242/comments")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}

// episodeHandlerPaths lists every read route the module registers; the
// error-path tests below drive all of them.
var episodeHandlerPaths = []string{
	"/v1/episodes/1",
	"/v1/episodes/1/stats",
	"/v1/episodes/1/ratings",
	"/v1/episodes/1/comments",
	"/v1/episodes/1/lists",
}

// decodeErrorEnvelope reads the {"error":{"code","message"}} envelope.
func decodeErrorEnvelope(t *testing.T, resp *http.Response) (code, message string) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decodeBody(t, resp, &body)
	return body.Error.Code, body.Error.Message
}

// seedExtraVisibleLists appends n public lists that each contain epID, so the
// lists endpoint has more than one visible row to page through.
func seedExtraVisibleLists(t *testing.T, db *gorm.DB, epID int64, n int) {
	t.Helper()
	base := time.Now().UTC()
	for i := 0; i < n; i++ {
		suffix := strconv.Itoa(i)
		l := database.List{
			UserID: "list-owner", Name: "Extra Public " + suffix,
			Slug: "extra-public-" + suffix, Privacy: "public",
			CreatedAt: base, UpdatedAt: base,
		}
		if err := db.Create(&l).Error; err != nil {
			t.Fatalf("seed extra list: %v", err)
		}
		item := database.ListItem{
			ListID: l.ID, MediaType: "episode", MediaID: epID,
			Rank: 1, AddedBy: "list-owner", AddedAt: base.Add(time.Duration(i) * time.Second),
		}
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("seed extra list item: %v", err)
		}
	}
}

// seedExtraComments appends n additional comments on epID.
func seedExtraComments(t *testing.T, db *gorm.DB, epID int64, n int) {
	t.Helper()
	base := time.Now().UTC()
	for i := 0; i < n; i++ {
		mid := epID
		cm := database.Comment{
			UserID: "user-a", MediaType: "episode", MediaID: &mid,
			CommentType: "shout", Body: "extra take " + strconv.Itoa(i),
			CreatedAt: base.Add(time.Duration(i) * time.Second), UpdatedAt: base,
		}
		if err := db.Create(&cm).Error; err != nil {
			t.Fatalf("seed extra comment: %v", err)
		}
	}
}

// TestGetEpisodeListsPagination drives the page/limit handling of the lists
// endpoint: defaults, the row count on a partial page, an out-of-range page
// (empty page, totals intact), the 100 limit cap, and invalid-input fallback.
func TestGetEpisodeListsPagination(t *testing.T) {
	db := setupTestDB(t)
	epID := seedEpisodeFixture(t, db)     // 1 visible + 1 private list
	seedExtraVisibleLists(t, db, epID, 2) // +2 visible => 3 visible, total_count 3
	app := makeEpisodesApp()

	cases := []struct {
		name                string
		query               string
		wantPage, wantLimit int
		wantTotal, wantRows int
	}{
		{"defaults", "", 1, 25, 3, 3},
		{"page 2 limit 2 returns the tail row", "?page=2&limit=2", 2, 2, 3, 1},
		{"out-of-range page is empty but total_count holds", "?page=99&limit=2", 99, 2, 3, 0},
		{"limit above the 100 cap clamps", "?limit=1000", 1, 100, 3, 3},
		{"non-positive/invalid page and limit fall back", "?page=0&limit=abc", 1, 25, 3, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doGET(t, app, "/v1/episodes/"+itoa(epID)+"/lists"+tc.query)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
			var body struct {
				Data       []json.RawMessage `json:"data"`
				Page       int               `json:"page"`
				Limit      int               `json:"limit"`
				TotalCount int               `json:"total_count"`
			}
			decodeBody(t, resp, &body)
			if body.Page != tc.wantPage || body.Limit != tc.wantLimit {
				t.Errorf("page/limit = %d/%d, want %d/%d", body.Page, body.Limit, tc.wantPage, tc.wantLimit)
			}
			if body.TotalCount != tc.wantTotal {
				t.Errorf("total_count = %d, want %d", body.TotalCount, tc.wantTotal)
			}
			if len(body.Data) != tc.wantRows {
				t.Errorf("row_count = %d, want %d", len(body.Data), tc.wantRows)
			}
		})
	}
}

// TestGetEpisodeCommentsPagination drives the comments endpoint's page/limit
// handling with the same cases as the lists endpoint.
func TestGetEpisodeCommentsPagination(t *testing.T) {
	db := setupTestDB(t)
	epID := seedEpisodeFixture(t, db) // 2 comments
	seedExtraComments(t, db, epID, 3) // +3 => 5 comments
	app := makeEpisodesApp()

	cases := []struct {
		name                string
		query               string
		wantPage, wantLimit int
		wantTotal, wantRows int
	}{
		{"defaults", "", 1, 25, 5, 5},
		{"page 2 limit 2 returns two rows", "?page=2&limit=2", 2, 2, 5, 2},
		{"out-of-range page is empty but total_count holds", "?page=99&limit=2", 99, 2, 5, 0},
		{"limit above the 100 cap clamps", "?limit=1000", 1, 100, 5, 5},
		{"non-positive/invalid page and limit fall back", "?page=-1&limit=abc", 1, 25, 5, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doGET(t, app, "/v1/episodes/"+itoa(epID)+"/comments"+tc.query)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
			var body struct {
				Data       []json.RawMessage `json:"data"`
				Page       int               `json:"page"`
				Limit      int               `json:"limit"`
				TotalCount int               `json:"total_count"`
			}
			decodeBody(t, resp, &body)
			if body.Page != tc.wantPage || body.Limit != tc.wantLimit {
				t.Errorf("page/limit = %d/%d, want %d/%d", body.Page, body.Limit, tc.wantPage, tc.wantLimit)
			}
			if body.TotalCount != tc.wantTotal {
				t.Errorf("total_count = %d, want %d", body.TotalCount, tc.wantTotal)
			}
			if len(body.Data) != tc.wantRows {
				t.Errorf("row_count = %d, want %d", len(body.Data), tc.wantRows)
			}
		})
	}
}

// TestEpisodeHandlersDBUnavailable exercises the GetDB() failure branch on every
// episode route. It is deliberately standalone (no setupTestDB): each other
// test's SetDBForTest cleanup resets the singleton to nil, so GetDB() is
// guaranteed to fail here and each handler returns its 500 envelope.
func TestEpisodeHandlersDBUnavailable(t *testing.T) {
	app := makeEpisodesApp()
	for _, path := range episodeHandlerPaths {
		t.Run(path, func(t *testing.T) {
			resp := doGET(t, app, path)
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("expected 500, got %d", resp.StatusCode)
			}
			code, msg := decodeErrorEnvelope(t, resp)
			if code != "internal" || msg != "database unavailable" {
				t.Errorf("envelope = {%q, %q}, want {internal, database unavailable}", code, msg)
			}
		})
	}
}

// TestEpisodeHandlersDBError forces a genuine (non-ErrRecordNotFound) query
// error by dropping the episodes table after seeding: every route looks the
// episode up first, and a real query failure must map to the 500
// "database query error" envelope rather than a 404.
func TestEpisodeHandlersDBError(t *testing.T) {
	db := setupTestDB(t)
	seedEpisodeFixture(t, db)
	if err := db.Exec("DROP TABLE episodes").Error; err != nil {
		t.Fatalf("drop episodes table: %v", err)
	}
	app := makeEpisodesApp()
	for _, path := range episodeHandlerPaths {
		t.Run(path, func(t *testing.T) {
			resp := doGET(t, app, path)
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("expected 500, got %d", resp.StatusCode)
			}
			code, msg := decodeErrorEnvelope(t, resp)
			if code != "internal" || msg != "database query error" {
				t.Errorf("envelope = {%q, %q}, want {internal, database query error}", code, msg)
			}
		})
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
