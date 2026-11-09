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

// setupStatsDB migrates every table the stats aggregates read and points the
// database singleton at an in-memory sqlite DB.
func setupStatsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	models := []interface{}{
		&database.Movie{}, &database.Show{}, &database.Season{},
		&database.Episode{}, &database.Person{}, &database.ExternalID{},
		&database.UserHistory{}, &database.UserCollection{},
		&database.UserRating{}, &database.UserFavorite{},
		&database.Comment{}, &database.List{}, &database.ListItem{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

func makeStatsApp() *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	RegisterRoutes(app.Group("/v1"))
	return app
}

func getStats(t *testing.T, app *fiber.App, path string) *http.Response {
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

type statsBody struct {
	Watchers    int64  `json:"watchers"`
	Plays       int64  `json:"plays"`
	Collectors  int64  `json:"collectors"`
	Comments    int64  `json:"comments"`
	Lists       int64  `json:"lists"`
	Votes       int64  `json:"votes"`
	Favorited   int64  `json:"favorited"`
	Recommended *int64 `json:"recommended"`
}

func decodeStats(t *testing.T, resp *http.Response) statsBody {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var body statsBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode stats body: %v", err)
	}
	return body
}

func TestMovieStatsAggregatesStoredRows(t *testing.T) {
	db := setupStatsDB(t)
	now := time.Now().UTC()

	// Several genres: a stats query must not join movies.genres, or the
	// per-history counts would fan out by the genre count.
	movie := database.Movie{Title: "Multi Genre", TmdbID: 7001, Genres: database.StringSlice{"Drama", "Crime", "Thriller"}}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	// 2 distinct watchers, 3 plays. Each row needs its own source_id: the
	// production unique index is on (user, media, source, source_id).
	for i, u := range []string{"user-a", "user-a", "user-b"} {
		src := "play-" + strconv.Itoa(i)
		if err := db.Create(&database.UserHistory{
			UserID: u, MediaType: "movie", MediaID: movie.ID, WatchedAt: now, Source: "api", SourceID: &src,
		}).Error; err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}
	if err := db.Create(&database.UserCollection{UserID: "user-a", MediaType: "movie", MediaID: movie.ID, CollectedAt: now}).Error; err != nil {
		t.Fatalf("seed collection: %v", err)
	}
	mid := movie.ID
	if err := db.Create(&database.Comment{UserID: "user-a", MediaType: "movie", MediaID: &mid, CommentType: "shout", Body: "hi", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	lists := []database.List{
		{UserID: "owner", Name: "One", Slug: "one", Privacy: "public", CreatedAt: now, UpdatedAt: now},
		{UserID: "owner", Name: "Two", Slug: "two", Privacy: "public", CreatedAt: now, UpdatedAt: now},
	}
	for i := range lists {
		if err := db.Create(&lists[i]).Error; err != nil {
			t.Fatalf("seed list: %v", err)
		}
		if err := db.Create(&database.ListItem{ListID: lists[i].ID, MediaType: "movie", MediaID: movie.ID, Rank: 1, AddedBy: "owner", AddedAt: now}).Error; err != nil {
			t.Fatalf("seed list item: %v", err)
		}
	}
	for _, r := range []struct {
		user string
		val  int
	}{{"user-a", 8}, {"user-b", 10}} {
		if err := db.Create(&database.UserRating{
			UserID: r.user, MediaType: "movie", MediaID: movie.ID, Rating: r.val, RatedAt: now, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed rating: %v", err)
		}
	}
	if err := db.Create(&database.UserFavorite{UserID: "user-a", MediaType: "movie", MediaID: movie.ID, FavoritedAt: now}).Error; err != nil {
		t.Fatalf("seed favorite: %v", err)
	}

	resp := getStats(t, makeStatsApp(), "/v1/movies/"+strconv.FormatInt(movie.ID, 10)+"/stats")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeStats(t, resp)

	if body.Watchers != 2 {
		t.Errorf("watchers = %d, want 2 distinct users", body.Watchers)
	}
	if body.Plays != 3 {
		t.Errorf("plays = %d, want 3 rows (3 genres must not fan out the count)", body.Plays)
	}
	if body.Collectors != 1 {
		t.Errorf("collectors = %d, want 1", body.Collectors)
	}
	if body.Comments != 1 {
		t.Errorf("comments = %d, want 1", body.Comments)
	}
	if body.Lists != 2 {
		t.Errorf("lists = %d, want 2", body.Lists)
	}
	if body.Votes != 2 {
		t.Errorf("votes = %d, want 2 rating rows", body.Votes)
	}
	if body.Favorited != 1 {
		t.Errorf("favorited = %d, want 1", body.Favorited)
	}
	if body.Recommended != nil {
		t.Errorf("recommended = %v, want null (no recommendations table)", *body.Recommended)
	}
}

func TestShowStatsAggregatesEpisodeHistory(t *testing.T) {
	db := setupStatsDB(t)
	now := time.Now().UTC()

	show := database.Show{Title: "Stats Show", TmdbID: 7101}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	season := database.Season{ShowID: show.ID, SeasonNumber: 1, EpisodeCount: 2}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	eps := []database.Episode{
		{SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: 1, Title: "E1"},
		{SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: 2, Title: "E2"},
	}
	for i := range eps {
		if err := db.Create(&eps[i]).Error; err != nil {
			t.Fatalf("seed episode: %v", err)
		}
	}

	// Episode history: user-a watches E1 twice and E2 once; user-b watches E1.
	history := []struct {
		user string
		ep   int
	}{{"user-a", 0}, {"user-a", 0}, {"user-a", 1}, {"user-b", 0}}
	for i, h := range history {
		seasonID := season.ID
		episodeID := eps[h.ep].ID
		src := "play-" + strconv.Itoa(i)
		if err := db.Create(&database.UserHistory{
			UserID: h.user, MediaType: "episode", MediaID: episodeID,
			SeasonID: &seasonID, EpisodeID: &episodeID, WatchedAt: now, Source: "api", SourceID: &src,
		}).Error; err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}

	if err := db.Create(&database.UserCollection{UserID: "user-a", MediaType: "show", MediaID: show.ID, CollectedAt: now}).Error; err != nil {
		t.Fatalf("seed collection: %v", err)
	}
	sid := show.ID
	if err := db.Create(&database.Comment{UserID: "user-a", MediaType: "show", MediaID: &sid, CommentType: "shout", Body: "hi", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	l := database.List{UserID: "owner", Name: "Shows", Slug: "shows", Privacy: "public", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&l).Error; err != nil {
		t.Fatalf("seed list: %v", err)
	}
	if err := db.Create(&database.ListItem{ListID: l.ID, MediaType: "show", MediaID: show.ID, Rank: 1, AddedBy: "owner", AddedAt: now}).Error; err != nil {
		t.Fatalf("seed list item: %v", err)
	}
	if err := db.Create(&database.UserRating{UserID: "user-a", MediaType: "show", MediaID: show.ID, Rating: 9, RatedAt: now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed rating: %v", err)
	}
	if err := db.Create(&database.UserFavorite{UserID: "user-a", MediaType: "show", MediaID: show.ID, FavoritedAt: now}).Error; err != nil {
		t.Fatalf("seed favorite: %v", err)
	}

	resp := getStats(t, makeStatsApp(), "/v1/shows/"+strconv.FormatInt(show.ID, 10)+"/stats")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeStats(t, resp)

	if body.Watchers != 2 {
		t.Errorf("watchers = %d, want 2 distinct users across the show's episodes", body.Watchers)
	}
	if body.Plays != 4 {
		t.Errorf("plays = %d, want 4 episode plays", body.Plays)
	}
	if body.Collectors != 1 || body.Comments != 1 || body.Lists != 1 || body.Votes != 1 || body.Favorited != 1 {
		t.Errorf("unexpected show stats: %+v", body)
	}
	if body.Recommended != nil {
		t.Errorf("recommended = %v, want null", *body.Recommended)
	}
}

func TestMovieStatsZeroActivityIsRealZero(t *testing.T) {
	db := setupStatsDB(t)
	movie := database.Movie{Title: "Quiet", TmdbID: 7201, Genres: database.StringSlice{"Drama", "Comedy"}}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	resp := getStats(t, makeStatsApp(), "/v1/movies/"+strconv.FormatInt(movie.ID, 10)+"/stats")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for a title with no activity, got %d", resp.StatusCode)
	}
	body := decodeStats(t, resp)
	if body.Watchers != 0 || body.Plays != 0 || body.Collectors != 0 || body.Comments != 0 || body.Lists != 0 || body.Votes != 0 || body.Favorited != 0 {
		t.Errorf("expected all-zero counts, got %+v", body)
	}
	if body.Recommended != nil {
		t.Errorf("recommended = %v, want null", *body.Recommended)
	}
}

func TestMovieAndShowStatsNotFound(t *testing.T) {
	setupStatsDB(t)
	app := makeStatsApp()

	for _, path := range []string{"/v1/movies/999999/stats", "/v1/shows/999999/stats", "/v1/movies/no-such-slug/stats"} {
		resp := getStats(t, app, path)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}
