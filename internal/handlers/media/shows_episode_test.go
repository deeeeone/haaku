package media

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

// setupShowsDB opens an in-memory sqlite DB with every table ResolveID and the
// episode builders touch (movies/people must exist for the media-type lookup),
// and points the database singleton at it.
func setupShowsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	models := []interface{}{
		&database.Movie{}, &database.Show{}, &database.Season{},
		&database.Episode{}, &database.Person{}, &database.ExternalID{},
		&database.UserRating{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// seedShowWithEpisodes creates a show, one season, and one episode per air date.
func seedShowWithEpisodes(t *testing.T, db *gorm.DB, tmdbID int, title string, airdates ...time.Time) (showID int64, episodeIDs []int64) {
	t.Helper()
	show := database.Show{Title: title, TmdbID: tmdbID}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	season := database.Season{ShowID: show.ID, SeasonNumber: 1, EpisodeCount: len(airdates)}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	for i, air := range airdates {
		d := database.Date{Time: air.UTC()}
		ep := database.Episode{
			SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: i + 1,
			Title: title + " E" + string(rune('1'+i)), AiredAt: &d,
		}
		if err := db.Create(&ep).Error; err != nil {
			t.Fatalf("seed episode: %v", err)
		}
		episodeIDs = append(episodeIDs, ep.ID)
	}
	return show.ID, episodeIDs
}

func showsApp() *fiber.App {
	// Wire the application's error handler: the media handlers hand back
	// response.ErrHandled when a helper already wrote the response, and Fiber's
	// default handler turns that non-fiber error into a 500. Production wires
	// the same one (httpapi/server.go), so a bare fiber.New() here would be
	// testing a different error path than the app serves.
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	RegisterRoutes(app.Group("/v1"))
	return app
}

func doShowsGet(t *testing.T, app *fiber.App, path string) *http.Response {
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

func decodeEpisode(t *testing.T, resp *http.Response) EpisodeResponse {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out EpisodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode episode body: %v", err)
	}
	return out
}

func TestShowNextAndLastEpisode(t *testing.T) {
	db := setupShowsDB(t)
	now := time.Now().UTC()

	past := now.AddDate(0, 0, -30)
	future := now.AddDate(0, 0, 30)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	showA, aEps := seedShowWithEpisodes(t, db, 4001, "Alpha", past, future)
	showB, _ := seedShowWithEpisodes(t, db, 4002, "Beta", past)      // nothing upcoming
	showC, _ := seedShowWithEpisodes(t, db, 4003, "Gamma", future)   // nothing aired yet
	showD, dEps := seedShowWithEpisodes(t, db, 4004, "Delta", today) // airs today: both boundaries

	app := showsApp()

	t.Run("next_episode returns the earliest scheduled episode", func(t *testing.T) {
		resp := doShowsGet(t, app, "/v1/shows/"+itoa(showA)+"/next_episode")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		got := decodeEpisode(t, resp)
		if got.IDs.ID != aEps[1] {
			t.Errorf("expected future episode %d, got %d", aEps[1], got.IDs.ID)
		}
	})

	t.Run("last_episode returns the most recently aired episode", func(t *testing.T) {
		resp := doShowsGet(t, app, "/v1/shows/"+itoa(showA)+"/last_episode")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		got := decodeEpisode(t, resp)
		if got.IDs.ID != aEps[0] {
			t.Errorf("expected past episode %d, got %d", aEps[0], got.IDs.ID)
		}
	})

	t.Run("an episode airing today is both next and last (inclusive boundary)", func(t *testing.T) {
		n := doShowsGet(t, app, "/v1/shows/"+itoa(showD)+"/next_episode")
		if n.StatusCode != http.StatusOK {
			t.Fatalf("next: expected 200, got %d", n.StatusCode)
		}
		if got := decodeEpisode(t, n); got.IDs.ID != dEps[0] {
			t.Errorf("next: expected today's episode %d, got %d", dEps[0], got.IDs.ID)
		}
		l := doShowsGet(t, app, "/v1/shows/"+itoa(showD)+"/last_episode")
		if l.StatusCode != http.StatusOK {
			t.Fatalf("last: expected 200, got %d", l.StatusCode)
		}
		if got := decodeEpisode(t, l); got.IDs.ID != dEps[0] {
			t.Errorf("last: expected today's episode %d, got %d", dEps[0], got.IDs.ID)
		}
	})

	t.Run("204 when there is no next episode", func(t *testing.T) {
		resp := doShowsGet(t, app, "/v1/shows/"+itoa(showB)+"/next_episode")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", resp.StatusCode)
		}
	})

	t.Run("204 when nothing has aired yet", func(t *testing.T) {
		resp := doShowsGet(t, app, "/v1/shows/"+itoa(showC)+"/last_episode")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", resp.StatusCode)
		}
	})

	t.Run("unknown or non-show id is 404", func(t *testing.T) {
		r1 := doShowsGet(t, app, "/v1/shows/999999/next_episode")
		defer func() { _ = r1.Body.Close() }()
		if r1.StatusCode != http.StatusNotFound {
			t.Errorf("unknown id: expected 404, got %d", r1.StatusCode)
		}
		r2 := doShowsGet(t, app, "/v1/shows/no-such-slug/last_episode")
		defer func() { _ = r2.Body.Close() }()
		if r2.StatusCode != http.StatusNotFound {
			t.Errorf("unknown slug: expected 404, got %d", r2.StatusCode)
		}
	})
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
