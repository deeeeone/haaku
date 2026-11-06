package episodes

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"trakt-api/internal/database"

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

	t.Run("404 for non-numeric id", func(t *testing.T) {
		// NB: a malformed id writes a 400 via response.ParseIDParam but that
		// helper returns a nil error, so the handler falls through to the DB
		// lookup (id=0) and the NotFound branch overwrites the status to 404.
		// This is the current on-main behavior; a genuine 400 is unreachable
		// here until ParseIDParam stops swallowing the parse error (see note
		// in the module report).
		resp := doGET(t, app, "/v1/episodes/abc")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
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

	t.Run("404 for non-numeric id", func(t *testing.T) {
		// Malformed ids fall through to the 404 not-found branch (see the
		// TestGetEpisodeHandler non-numeric comment for the ParseIDParam detail).
		resp := doGET(t, app, "/v1/episodes/nope/stats")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
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
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
