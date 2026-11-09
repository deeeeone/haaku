package discovery

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var rankingNow = time.Date(2032, 1, 15, 12, 0, 0, 0, time.UTC)

func setupRankingsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Movie{}, &database.Show{}, &database.Season{}, &database.Episode{},
		&database.UserHistory{}, &database.UserCollection{}, &database.UserFavorite{},
		&database.UserWatchlist{},
	); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	// user_checkins and scrobble_sessions carry `DEFAULT now()` in their GORM
	// tags, which sqlite rejects; create the same columns with portable DDL.
	for _, ddl := range []string{
		`CREATE TABLE user_checkins (
			id INTEGER PRIMARY KEY AUTOINCREMENT, user_id TEXT NOT NULL, media_type TEXT NOT NULL,
			media_id INTEGER NOT NULL, season_id INTEGER, episode_id INTEGER, message TEXT,
			expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT uq_user_checkins UNIQUE (user_id))`,
		`CREATE TABLE scrobble_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, user_id TEXT NOT NULL, media_type TEXT NOT NULL,
			media_id INTEGER NOT NULL, season_id INTEGER, episode_id INTEGER, action TEXT NOT NULL,
			progress REAL NOT NULL DEFAULT 0, expires_at DATETIME NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create sqlite table: %v", err)
		}
	}
	database.SetDBForTest(t, db)

	previous := rankingsNow
	rankingsNow = func() time.Time { return rankingNow }
	t.Cleanup(func() { rankingsNow = previous })
	return db
}

func rankingsApp() *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	RegisterRoutes(app.Group("/v1"))
	return app
}

func getRankings(t *testing.T, app *fiber.App, path string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	return resp
}

func seedMovie(t *testing.T, db *gorm.DB, tmdb int, title string, popularity float64) database.Movie {
	t.Helper()
	movie := database.Movie{TmdbID: tmdb, Title: title, Popularity: popularity}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	return movie
}

func seedShow(t *testing.T, db *gorm.DB, tmdb int, title string, popularity float64) database.Show {
	t.Helper()
	show := database.Show{TmdbID: tmdb, Title: title, Popularity: popularity}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	return show
}

func seedEpisode(t *testing.T, db *gorm.DB, showID int64, number int) database.Episode {
	t.Helper()
	season := database.Season{ShowID: showID, SeasonNumber: 1, EpisodeCount: 1}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	episode := database.Episode{SeasonID: season.ID, ShowID: showID, EpisodeNumber: number, Title: "E" + strconv.Itoa(number)}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	return episode
}

func addHistory(t *testing.T, db *gorm.DB, user, mediaType string, mediaID int64, at time.Time) {
	t.Helper()
	if err := db.Create(&database.UserHistory{
		UserID: user, MediaType: mediaType, MediaID: mediaID, WatchedAt: at, Source: "api",
	}).Error; err != nil {
		t.Fatalf("seed history: %v", err)
	}
}

func addCollection(t *testing.T, db *gorm.DB, user, mediaType string, mediaID int64, at time.Time) {
	t.Helper()
	if err := db.Create(&database.UserCollection{
		UserID: user, MediaType: mediaType, MediaID: mediaID, CollectedAt: at,
	}).Error; err != nil {
		t.Fatalf("seed collection: %v", err)
	}
}

func addFavorite(t *testing.T, db *gorm.DB, user, mediaType string, mediaID int64, at time.Time) {
	t.Helper()
	if err := db.Create(&database.UserFavorite{
		UserID: user, MediaType: mediaType, MediaID: mediaID, FavoritedAt: at,
	}).Error; err != nil {
		t.Fatalf("seed favorite: %v", err)
	}
}

func addWatchlist(t *testing.T, db *gorm.DB, user, mediaType string, mediaID int64, at time.Time) {
	t.Helper()
	if err := db.Create(&database.UserWatchlist{
		UserID: user, MediaType: mediaType, MediaID: mediaID, ListedAt: at,
	}).Error; err != nil {
		t.Fatalf("seed watchlist: %v", err)
	}
}

func daysAgo(days int) time.Time   { return rankingNow.AddDate(0, 0, -days) }
func hoursAgo(hours int) time.Time { return rankingNow.Add(-time.Duration(hours) * time.Hour) }

type movieRankingItem struct {
	WatcherCount   int64 `json:"watcher_count"`
	PlayCount      int64 `json:"play_count"`
	CollectedCount int64 `json:"collected_count"`
	Movie          struct {
		Title string `json:"title"`
		IDs   struct {
			Trakt int64 `json:"trakt"`
		} `json:"ids"`
	} `json:"movie"`
}

type showRankingItem struct {
	WatcherCount   int64 `json:"watcher_count"`
	PlayCount      int64 `json:"play_count"`
	CollectedCount int64 `json:"collected_count"`
	CollectorCount int64 `json:"collector_count"`
	Show           struct {
		Title string `json:"title"`
		IDs   struct {
			Trakt int64 `json:"trakt"`
		} `json:"ids"`
	} `json:"show"`
}

func decodeMovies(t *testing.T, resp *http.Response) []movieRankingItem {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out []movieRankingItem
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func decodeShows(t *testing.T, resp *http.Response) []showRankingItem {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out []showRankingItem
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func titlesOfMovies(items []movieRankingItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Movie.Title)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func seedMovieRankingData(t *testing.T, db *gorm.DB) {
	t.Helper()
	alpha := seedMovie(t, db, 801, "Alpha", 10)
	beta := seedMovie(t, db, 802, "Beta", 20)
	gamma := seedMovie(t, db, 803, "Gamma", 30)

	addHistory(t, db, "u1", "movie", alpha.ID, hoursAgo(1))
	addHistory(t, db, "u2", "movie", alpha.ID, daysAgo(2))
	addHistory(t, db, "u3", "movie", alpha.ID, daysAgo(10))

	addHistory(t, db, "u1", "movie", beta.ID, daysAgo(2))
	addHistory(t, db, "u2", "movie", beta.ID, daysAgo(2))
	addHistory(t, db, "u3", "movie", beta.ID, daysAgo(2))
	addHistory(t, db, "u4", "movie", beta.ID, daysAgo(2))

	addHistory(t, db, "u1", "movie", gamma.ID, daysAgo(400))

	addCollection(t, db, "u1", "movie", alpha.ID, daysAgo(2))
	addCollection(t, db, "u2", "movie", alpha.ID, daysAgo(2))
	addCollection(t, db, "u1", "movie", beta.ID, daysAgo(2))
	addCollection(t, db, "u1", "movie", gamma.ID, daysAgo(2))

	addFavorite(t, db, "u1", "movie", alpha.ID, daysAgo(2))
	addFavorite(t, db, "u2", "movie", alpha.ID, daysAgo(2))
	addFavorite(t, db, "u3", "movie", beta.ID, daysAgo(2))
}

func TestMoviePeriodRankings(t *testing.T) {
	db := setupRankingsDB(t)
	seedMovieRankingData(t, db)
	app := rankingsApp()

	cases := []struct {
		period string
		kind   string
		want   []string
	}{
		{"weekly", "watched", []string{"Beta", "Alpha"}},
		{"monthly", "watched", []string{"Beta", "Alpha"}},
		{"weekly", "played", []string{"Beta", "Alpha"}},
		{"weekly", "collected", []string{"Alpha", "Beta", "Gamma"}},
		{"weekly", "favorited", []string{"Alpha", "Beta"}},
		{"daily", "watched", []string{"Alpha"}},
		{"yearly", "watched", []string{"Beta", "Alpha"}},
		{"all", "watched", []string{"Beta", "Alpha", "Gamma"}},
	}

	for _, tc := range cases {
		path := "/v1/movies/" + tc.kind + "/" + tc.period
		got := titlesOfMovies(decodeMovies(t, getRankings(t, app, path)))
		if !equalStrings(got, tc.want) {
			t.Errorf("%s = %v, want %v", path, got, tc.want)
		}
	}

	// Every contract count field is computed, not just the ranked one.
	items := decodeMovies(t, getRankings(t, app, "/v1/movies/watched/weekly"))
	if len(items) != 2 {
		t.Fatalf("weekly watched rows = %d, want 2", len(items))
	}
	beta := items[0]
	if beta.WatcherCount != 4 || beta.PlayCount != 4 || beta.CollectedCount != 1 {
		t.Errorf("Beta counts = watchers %d plays %d collected %d, want 4/4/1",
			beta.WatcherCount, beta.PlayCount, beta.CollectedCount)
	}
	alpha := items[1]
	if alpha.WatcherCount != 2 || alpha.PlayCount != 2 || alpha.CollectedCount != 2 {
		t.Errorf("Alpha counts = watchers %d plays %d collected %d, want 2/2/2",
			alpha.WatcherCount, alpha.PlayCount, alpha.CollectedCount)
	}
}

func TestShowPeriodRankings(t *testing.T) {
	db := setupRankingsDB(t)
	one := seedShow(t, db, 901, "One", 50)
	two := seedShow(t, db, 902, "Two", 40)
	three := seedShow(t, db, 903, "Three", 30)

	e1 := seedEpisode(t, db, one.ID, 1)
	e2 := seedEpisode(t, db, one.ID, 2)
	e3 := seedEpisode(t, db, two.ID, 1)
	e4 := seedEpisode(t, db, three.ID, 1)

	addHistory(t, db, "u1", "episode", e1.ID, hoursAgo(1))
	addHistory(t, db, "u2", "episode", e2.ID, daysAgo(2))
	addHistory(t, db, "u3", "episode", e1.ID, daysAgo(10))

	addHistory(t, db, "u1", "episode", e3.ID, daysAgo(2))
	addHistory(t, db, "u2", "episode", e3.ID, daysAgo(2))
	addHistory(t, db, "u3", "episode", e3.ID, daysAgo(2))
	addHistory(t, db, "u4", "episode", e3.ID, daysAgo(2))

	addHistory(t, db, "u1", "episode", e4.ID, daysAgo(400))

	addCollection(t, db, "u1", "episode", e1.ID, daysAgo(2))
	addCollection(t, db, "u2", "episode", e2.ID, daysAgo(2))
	addCollection(t, db, "u1", "episode", e3.ID, daysAgo(2))
	addCollection(t, db, "u1", "episode", e4.ID, daysAgo(2))

	addFavorite(t, db, "u1", "show", one.ID, daysAgo(2))
	addFavorite(t, db, "u2", "show", one.ID, daysAgo(2))
	addFavorite(t, db, "u3", "show", two.ID, daysAgo(2))

	app := rankingsApp()
	watched := decodeShows(t, getRankings(t, app, "/v1/shows/watched/weekly"))
	if len(watched) != 2 || watched[0].Show.Title != "Two" || watched[1].Show.Title != "One" {
		t.Fatalf("weekly show watched = %+v, want Two then One", watched)
	}
	if watched[1].WatcherCount != 2 || watched[1].PlayCount != 2 {
		t.Errorf("One watchers/plays = %d/%d, want 2/2", watched[1].WatcherCount, watched[1].PlayCount)
	}

	collected := decodeShows(t, getRankings(t, app, "/v1/shows/collected/weekly"))
	if len(collected) != 3 || collected[0].Show.Title != "One" {
		t.Fatalf("weekly show collected = %+v, want One first", collected)
	}
	if collected[0].CollectedCount != 2 || collected[0].CollectorCount != 2 {
		t.Errorf("One collected/collectors = %d/%d, want 2/2", collected[0].CollectedCount, collected[0].CollectorCount)
	}

	all := decodeShows(t, getRankings(t, app, "/v1/shows/watched/all"))
	if len(all) != 3 || all[2].Show.Title != "Three" {
		t.Errorf("all-period show watched = %+v, want Three included last", all)
	}

	favorited := decodeShows(t, getRankings(t, app, "/v1/shows/favorited/weekly"))
	if len(favorited) != 2 || favorited[0].Show.Title != "One" {
		t.Errorf("weekly show favorited = %+v, want One first", favorited)
	}
}

func TestRankingsBadPeriodIs400(t *testing.T) {
	setupRankingsDB(t)
	app := rankingsApp()
	for _, path := range []string{
		"/v1/movies/watched/fortnightly",
		"/v1/shows/played/never",
	} {
		resp := getRankings(t, app, path)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

func TestRankingsPaginationHeaders(t *testing.T) {
	db := setupRankingsDB(t)
	seedMovieRankingData(t, db)
	app := rankingsApp()

	resp := getRankings(t, app, "/v1/movies/watched/weekly?page=2&limit=1")
	items := decodeMovies(t, resp)
	if len(items) != 1 || items[0].Movie.Title != "Alpha" {
		t.Fatalf("page 2 = %+v, want Alpha", items)
	}
	headers := map[string]string{
		"X-Pagination-Page":        "2",
		"X-Pagination-Limit":       "1",
		"X-Pagination-Total-Items": "2",
		"X-Pagination-Items-Count": "1",
	}
	for key, want := range headers {
		if got := resp.Header.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestMovieHotRanking(t *testing.T) {
	db := setupRankingsDB(t)
	alpha := seedMovie(t, db, 811, "Hot Alpha", 10)
	beta := seedMovie(t, db, 812, "Hot Beta", 100)
	seedMovie(t, db, 813, "Cold", 200)

	addHistory(t, db, "u1", "movie", alpha.ID, hoursAgo(1))
	addWatchlist(t, db, "u1", "movie", alpha.ID, hoursAgo(2))
	addWatchlist(t, db, "u2", "movie", alpha.ID, hoursAgo(2))
	if err := db.Create(&database.UserCheckin{UserID: "u3", MediaType: "movie", MediaID: alpha.ID, ExpiresAt: rankingNow.Add(time.Hour), CreatedAt: hoursAgo(1)}).Error; err != nil {
		t.Fatalf("seed checkin: %v", err)
	}
	if err := db.Create(&database.ScrobbleSession{UserID: "u4", MediaType: "movie", MediaID: alpha.ID, Action: "start", ExpiresAt: rankingNow.Add(time.Hour), CreatedAt: hoursAgo(1)}).Error; err != nil {
		t.Fatalf("seed scrobble: %v", err)
	}
	// Old activity must not count.
	addHistory(t, db, "u9", "movie", alpha.ID, daysAgo(2))

	addHistory(t, db, "u1", "movie", beta.ID, hoursAgo(1))
	addHistory(t, db, "u2", "movie", beta.ID, hoursAgo(1))
	addHistory(t, db, "u3", "movie", beta.ID, hoursAgo(1))

	var items []struct {
		ListCount int64 `json:"list_count"`
		Movie     struct {
			Title string `json:"title"`
		} `json:"movie"`
	}
	resp := getRankings(t, rankingsApp(), "/v1/movies/hot")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 2 || items[0].Movie.Title != "Hot Alpha" || items[1].Movie.Title != "Hot Beta" {
		t.Fatalf("hot = %+v, want Hot Alpha then Hot Beta", items)
	}
	if items[0].ListCount != 2 || items[1].ListCount != 0 {
		t.Errorf("list_count = %d/%d, want 2/0", items[0].ListCount, items[1].ListCount)
	}
}

func TestShowHotRanking(t *testing.T) {
	db := setupRankingsDB(t)
	one := seedShow(t, db, 921, "Hot Show", 10)
	two := seedShow(t, db, 922, "Warm Show", 100)
	e1 := seedEpisode(t, db, one.ID, 1)
	e2 := seedEpisode(t, db, two.ID, 1)

	addHistory(t, db, "u1", "episode", e1.ID, hoursAgo(1))
	addWatchlist(t, db, "u1", "show", one.ID, hoursAgo(2))
	addWatchlist(t, db, "u2", "show", one.ID, hoursAgo(2))
	if err := db.Create(&database.UserCheckin{UserID: "u3", MediaType: "episode", MediaID: e1.ID, EpisodeID: &e1.ID, ExpiresAt: rankingNow.Add(time.Hour), CreatedAt: hoursAgo(1)}).Error; err != nil {
		t.Fatalf("seed episode checkin: %v", err)
	}

	addHistory(t, db, "u1", "episode", e2.ID, hoursAgo(1))
	addHistory(t, db, "u2", "episode", e2.ID, hoursAgo(1))
	addHistory(t, db, "u3", "episode", e2.ID, hoursAgo(1))

	var items []struct {
		ListCount int64 `json:"list_count"`
		Show      struct {
			Title string `json:"title"`
		} `json:"show"`
	}
	resp := getRankings(t, rankingsApp(), "/v1/shows/hot")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 2 || items[0].Show.Title != "Hot Show" || items[1].Show.Title != "Warm Show" {
		t.Fatalf("hot shows = %+v, want Hot Show then Warm Show", items)
	}
	if items[0].ListCount != 2 {
		t.Errorf("Hot Show list_count = %d, want 2", items[0].ListCount)
	}
}

func TestMediaTrendingAndPopularMergeTypes(t *testing.T) {
	db := setupRankingsDB(t)
	m1 := seedMovie(t, db, 821, "Movie One", 100)
	seedMovie(t, db, 822, "Movie Two", 50)
	s1 := seedShow(t, db, 921, "Show One", 75)
	seedShow(t, db, 922, "Show Two", 25)
	e1 := seedEpisode(t, db, s1.ID, 1)

	addHistory(t, db, "u1", "movie", m1.ID, hoursAgo(1))
	addHistory(t, db, "u2", "movie", m1.ID, hoursAgo(1))
	addHistory(t, db, "u1", "episode", e1.ID, hoursAgo(1))
	addHistory(t, db, "u2", "show", s1.ID, hoursAgo(1))

	app := rankingsApp()

	var popular []map[string]any
	resp := getRankings(t, app, "/v1/media/popular?limit=3")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("popular status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&popular); err != nil {
		t.Fatalf("decode popular: %v", err)
	}
	_ = resp.Body.Close()
	if len(popular) != 3 {
		t.Fatalf("popular items = %d, want 3", len(popular))
	}
	if popular[0]["title"] != "Movie One" || popular[1]["title"] != "Show One" || popular[2]["title"] != "Movie Two" {
		t.Errorf("popular order = %v, %v, %v", popular[0]["title"], popular[1]["title"], popular[2]["title"])
	}
	if _, hasWatchers := popular[0]["watchers"]; hasWatchers {
		t.Error("popular must carry the bare media object, not a watchers envelope")
	}
	if got := resp.Header.Get("X-Pagination-Total-Items"); got != "4" {
		t.Errorf("popular total items = %q, want 4", got)
	}

	var trending []map[string]any
	resp = getRankings(t, app, "/v1/media/trending?limit=3")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trending status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&trending); err != nil {
		t.Fatalf("decode trending: %v", err)
	}
	_ = resp.Body.Close()
	if len(trending) != 3 {
		t.Fatalf("trending items = %d, want 3", len(trending))
	}
	if trending[0]["movie"] == nil || trending[1]["show"] == nil {
		t.Fatalf("trending must identify each item by its movie/show key: %+v", trending)
	}
	if watchers, _ := trending[0]["watchers"].(float64); watchers != 2 {
		t.Errorf("Movie One watchers = %v, want 2", trending[0]["watchers"])
	}
	// Episode play (u1) and show-level play (u2) are one distinct watcher each.
	if watchers, _ := trending[1]["watchers"].(float64); watchers != 2 {
		t.Errorf("Show One watchers = %v, want 2", trending[1]["watchers"])
	}
}
