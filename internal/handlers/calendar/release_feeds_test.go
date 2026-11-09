package calendar

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupReleaseDB opens an in-memory sqlite DB with the tables the new calendar
// feeds read: movies plus their release rows, shows/seasons/episodes, and the
// per-user signals the my/hot feeds aggregate.
func setupReleaseDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	models := []interface{}{
		&database.Movie{}, &database.ExternalID{}, &database.MovieRelease{},
		&database.Show{}, &database.Season{}, &database.Episode{},
		&database.UserHistory{}, &database.UserWatchlist{}, &database.UserCollection{},
		&database.UserHidden{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// makeFeedsApp registers the new handlers directly with a stubbed user so the
// tests exercise the handlers, not Clerk.
func makeFeedsApp() *fiber.App {
	app := fiber.New()
	cal := app.Group("/v1/calendars")

	cal.Get("/all/dvd/:start_date/:days", pinCalendarTarget("all"), handleReleaseCalendar(calendarReleasePhysical))
	cal.Get("/my/dvd/:start_date/:days", stubUser("user-1"), pinCalendarTarget("my"), handleReleaseCalendar(calendarReleasePhysical))
	cal.Get("/all/streaming/:start_date/:days", pinCalendarTarget("all"), handleReleaseCalendar(calendarReleaseDigital))
	cal.Get("/my/streaming/:start_date/:days", stubUser("user-1"), pinCalendarTarget("my"), handleReleaseCalendar(calendarReleaseDigital))
	cal.Get("/all/media/:start_date/:days", pinCalendarTarget("all"), handleMediaCalendar)
	cal.Get("/my/media/:start_date/:days", stubUser("user-1"), pinCalendarTarget("my"), handleMediaCalendar)
	cal.Get("/releases/hot/:start_date/:days", handleHotCalendar)

	cal.Get("/all/shows/:start_date/:days", pinCalendarFeed("all", ""), handleCalendarShows)
	cal.Get("/my/shows/:start_date/:days", stubUser("user-1"), pinCalendarFeed("my", ""), handleCalendarShows)
	cal.Get("/all/shows/new/:start_date/:days", pinCalendarFeed("all", "new"), handleCalendarShows)
	cal.Get("/all/shows/premieres/:start_date/:days", pinCalendarFeed("all", "premieres"), handleCalendarShows)
	cal.Get("/all/shows/finales/:start_date/:days", pinCalendarFeed("all", "finale"), handleCalendarShows)
	return app
}

func decodeEntries(t *testing.T, resp *http.Response) []map[string]any {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return out
}

func entryMovieTitle(t *testing.T, entry map[string]any) string {
	t.Helper()
	movie, ok := entry["movie"].(map[string]any)
	if !ok {
		t.Fatalf("entry has no movie object: %+v", entry)
	}
	title, _ := movie["title"].(string)
	return title
}

func entryEpisode(t *testing.T, entry map[string]any) map[string]any {
	t.Helper()
	episode, ok := entry["episode"].(map[string]any)
	if !ok {
		t.Fatalf("entry has no episode object: %+v", entry)
	}
	return episode
}

func entryShowTitle(t *testing.T, entry map[string]any) string {
	t.Helper()
	show, ok := entry["show"].(map[string]any)
	if !ok {
		t.Fatalf("entry has no show object: %+v", entry)
	}
	title, _ := show["title"].(string)
	return title
}

// seedReleaseMovies creates four movies with physical and digital releases
// spread around the 2031-06-01 window.
func seedReleaseMovies(t *testing.T, db *gorm.DB) map[string]int64 {
	t.Helper()
	movies := map[string]database.Movie{
		"physical": {TmdbID: 201, Title: "Physical Only", Released: testDate(2031, 6, 2)},
		"digital":  {TmdbID: 202, Title: "Digital Only", Released: testDate(2031, 6, 3)},
		"both":     {TmdbID: 203, Title: "Both Types", Released: testDate(2031, 6, 4)},
		"outside":  {TmdbID: 204, Title: "Outside Week", Released: testDate(2031, 6, 10)},
	}
	ids := make(map[string]int64, len(movies))
	for key := range movies {
		movie := movies[key]
		if err := db.Create(&movie).Error; err != nil {
			t.Fatalf("seed movie %q: %v", movie.Title, err)
		}
		ids[key] = movie.ID
	}
	releases := []database.MovieRelease{
		{MovieID: ids["physical"], Country: "us", ReleaseType: calendarReleasePhysical, ReleaseDate: *testDate(2031, 6, 2)},
		{MovieID: ids["physical"], Country: "gb", ReleaseType: calendarReleasePhysical, ReleaseDate: *testDate(2031, 6, 6)},
		{MovieID: ids["digital"], Country: "us", ReleaseType: calendarReleaseDigital, ReleaseDate: *testDate(2031, 6, 3)},
		{MovieID: ids["both"], Country: "us", ReleaseType: calendarReleasePhysical, ReleaseDate: *testDate(2031, 6, 4)},
		{MovieID: ids["both"], Country: "us", ReleaseType: calendarReleaseDigital, ReleaseDate: *testDate(2031, 6, 5)},
		{MovieID: ids["outside"], Country: "us", ReleaseType: calendarReleasePhysical, ReleaseDate: *testDate(2031, 6, 10)},
	}
	if err := db.Create(&releases).Error; err != nil {
		t.Fatalf("seed releases: %v", err)
	}
	return ids
}

func TestReleaseCalendarDVD(t *testing.T) {
	db := setupReleaseDB(t)
	seedReleaseMovies(t, db)
	app := makeFeedsApp()

	t.Run("physical releases only, one row per movie, ordered by date", func(t *testing.T) {
		entries := decodeEntries(t, doGet(t, app, "/v1/calendars/all/dvd/2031-06-01/7"))
		if len(entries) != 2 {
			t.Fatalf("expected 2 physical releases in window, got %d (%+v)", len(entries), entries)
		}
		if entryMovieTitle(t, entries[0]) != "Physical Only" || entries[0]["released"] != "2031-06-02" {
			t.Errorf("first entry wrong: %+v", entries[0])
		}
		if entryMovieTitle(t, entries[1]) != "Both Types" || entries[1]["released"] != "2031-06-04" {
			t.Errorf("second entry wrong: %+v", entries[1])
		}
	})

	t.Run("a digital-only release never leaks into the DVD feed", func(t *testing.T) {
		entries := decodeEntries(t, doGet(t, app, "/v1/calendars/all/dvd/2031-06-01/7"))
		for _, entry := range entries {
			if entryMovieTitle(t, entry) == "Digital Only" {
				t.Fatalf("digital-only movie appeared in the DVD calendar: %+v", entry)
			}
		}
	})

	t.Run("days is the window length and the start date is inclusive", func(t *testing.T) {
		entries := decodeEntries(t, doGet(t, app, "/v1/calendars/all/dvd/2031-06-02/1"))
		if len(entries) != 1 || entries[0]["released"] != "2031-06-02" {
			t.Fatalf("expected exactly the start day's release, got %+v", entries)
		}
		if got := decodeEntries(t, doGet(t, app, "/v1/calendars/all/dvd/2031-06-01/1")); len(got) != 0 {
			t.Fatalf("expected empty one-day window, got %+v", got)
		}
	})

	t.Run("countries filter narrows the feed and earliest date wins", func(t *testing.T) {
		// Physical Only is 06-02 in us and 06-06 in gb; without a filter the
		// earlier date is the one row. With ?countries=gb it moves to 06-06.
		us := decodeEntries(t, doGet(t, app, "/v1/calendars/all/dvd/2031-06-01/7?countries=us"))
		if len(us) != 2 || us[0]["released"] != "2031-06-02" {
			t.Fatalf("us filter wrong: %+v", us)
		}
		gb := decodeEntries(t, doGet(t, app, "/v1/calendars/all/dvd/2031-06-01/7?countries=gb"))
		if len(gb) != 1 || gb[0]["released"] != "2031-06-06" || entryMovieTitle(t, gb[0]) != "Physical Only" {
			t.Fatalf("gb filter wrong: %+v", gb)
		}
		if got := decodeEntries(t, doGet(t, app, "/v1/calendars/all/dvd/2031-06-01/7?countries=zz")); len(got) != 0 {
			t.Fatalf("unknown country should be empty, got %+v", got)
		}
		if got := decodeEntries(t, doGet(t, app, "/v1/calendars/all/dvd/2031-06-01/7?countries=notacountry")); len(got) != 0 {
			t.Fatalf("invalid country filter should be empty, got %+v", got)
		}
	})

	t.Run("invalid start_date is 400", func(t *testing.T) {
		resp := doGet(t, app, "/v1/calendars/all/dvd/not-a-date/7")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})
}

func TestReleaseCalendarStreamingAndMy(t *testing.T) {
	db := setupReleaseDB(t)
	ids := seedReleaseMovies(t, db)
	now := time.Now().UTC()
	if err := db.Create(&database.UserHistory{
		UserID: "user-1", MediaType: "movie", MediaID: ids["physical"], WatchedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed history: %v", err)
	}
	if err := db.Create(&database.UserWatchlist{
		UserID: "user-1", MediaType: "movie", MediaID: ids["digital"], ListedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed watchlist: %v", err)
	}
	if err := db.Create(&database.UserHidden{
		UserID: "user-1", Section: "calendar", MediaType: "movie", MediaID: ids["both"], HiddenAt: now,
	}).Error; err != nil {
		t.Fatalf("seed hidden: %v", err)
	}
	app := makeFeedsApp()

	entries := decodeEntries(t, doGet(t, app, "/v1/calendars/all/streaming/2031-06-01/7"))
	if len(entries) != 2 {
		t.Fatalf("expected 2 digital releases, got %d (%+v)", len(entries), entries)
	}
	if entryMovieTitle(t, entries[0]) != "Digital Only" || entries[0]["released"] != "2031-06-03" {
		t.Errorf("digital release wrong: %+v", entries[0])
	}

	my := decodeEntries(t, doGet(t, app, "/v1/calendars/my/streaming/2031-06-01/7"))
	if len(my) != 1 || entryMovieTitle(t, my[0]) != "Digital Only" {
		t.Fatalf("my streaming should only include the watchlisted title, got %+v", my)
	}

	myDVD := decodeEntries(t, doGet(t, app, "/v1/calendars/my/dvd/2031-06-01/7"))
	if len(myDVD) != 1 || entryMovieTitle(t, myDVD[0]) != "Physical Only" {
		t.Fatalf("my dvd should only include the watched title (and not the hidden one), got %+v", myDVD)
	}
}

// seedShows creates show A with three seasons: one complete, one still airing
// (stored max below episode_count), one complete again; and show B with one
// stored episode of a season whose known count is higher (also incomplete).
func seedShows(t *testing.T, db *gorm.DB) (showA, showB int64, err error) {
	t.Helper()
	a := database.Show{TmdbID: 9001, Title: "Show A"}
	b := database.Show{TmdbID: 9002, Title: "Show B"}
	for _, show := range []*database.Show{&a, &b} {
		if e := db.Create(show).Error; e != nil {
			t.Fatalf("seed show: %v", e)
		}
	}

	seasons := []database.Season{
		{ShowID: a.ID, SeasonNumber: 1, EpisodeCount: 2},
		{ShowID: a.ID, SeasonNumber: 2, EpisodeCount: 3},
		{ShowID: a.ID, SeasonNumber: 3, EpisodeCount: 3},
		{ShowID: b.ID, SeasonNumber: 1, EpisodeCount: 2},
	}
	if e := db.Create(&seasons).Error; e != nil {
		t.Fatalf("seed seasons: %v", e)
	}

	episodes := []database.Episode{
		{ShowID: a.ID, SeasonID: seasons[0].ID, EpisodeNumber: 1, Title: "A S01E01", AiredAt: testDate(2031, 6, 2)},
		{ShowID: a.ID, SeasonID: seasons[0].ID, EpisodeNumber: 2, Title: "A S01E02", AiredAt: testDate(2031, 6, 3)},
		{ShowID: a.ID, SeasonID: seasons[1].ID, EpisodeNumber: 1, Title: "A S02E01", AiredAt: testDate(2031, 6, 3)},
		{ShowID: a.ID, SeasonID: seasons[1].ID, EpisodeNumber: 2, Title: "A S02E02", AiredAt: testDate(2031, 6, 4)},
		{ShowID: a.ID, SeasonID: seasons[2].ID, EpisodeNumber: 1, Title: "A S03E01", AiredAt: testDate(2031, 6, 4)},
		{ShowID: a.ID, SeasonID: seasons[2].ID, EpisodeNumber: 3, Title: "A S03E03", AiredAt: testDate(2031, 6, 5)},
		{ShowID: b.ID, SeasonID: seasons[3].ID, EpisodeNumber: 1, Title: "B S01E01", AiredAt: testDate(2031, 6, 2)},
	}
	if e := db.Create(&episodes).Error; e != nil {
		t.Fatalf("seed episodes: %v", e)
	}
	return a.ID, b.ID, nil
}

func TestContractShowFeeds(t *testing.T) {
	db := setupReleaseDB(t)
	showA, _, _ := seedShows(t, db)
	app := makeFeedsApp()

	t.Run("the plain calendar is the contract array", func(t *testing.T) {
		entries := decodeEntries(t, doGet(t, app, "/v1/calendars/all/shows/2031-06-01/7"))
		if len(entries) != 7 {
			t.Fatalf("expected all 7 episodes, got %d", len(entries))
		}
		if entries[0]["first_aired"] != "2031-06-02" {
			t.Errorf("first_aired wrong: %+v", entries[0])
		}
	})

	t.Run("new is S01E01", func(t *testing.T) {
		entries := decodeEntries(t, doGet(t, app, "/v1/calendars/all/shows/new/2031-06-01/7"))
		if len(entries) != 2 {
			t.Fatalf("expected 2 series premieres (A and B), got %d (%+v)", len(entries), entries)
		}
		for _, entry := range entries {
			episode := entryEpisode(t, entry)
			if episode["season"].(float64) != 1 || episode["number"].(float64) != 1 {
				t.Errorf("new must be S01E01, got %+v", episode)
			}
		}
	})

	t.Run("premieres is episode 1 of a later season", func(t *testing.T) {
		entries := decodeEntries(t, doGet(t, app, "/v1/calendars/all/shows/premieres/2031-06-01/7"))
		if len(entries) != 2 {
			t.Fatalf("expected S02E01 and S03E01, got %+v", entries)
		}
		for _, entry := range entries {
			episode := entryEpisode(t, entry)
			if episode["number"].(float64) != 1 || episode["season"].(float64) <= 1 {
				t.Errorf("premieres wrong: %+v", episode)
			}
		}
	})

	t.Run("finales only claims a last episode when the season is known complete", func(t *testing.T) {
		entries := decodeEntries(t, doGet(t, app, "/v1/calendars/all/shows/finales/2031-06-01/7"))
		if len(entries) != 2 {
			t.Fatalf("expected S01E02 and S03E03 only, got %+v", entries)
		}
		got := map[string]bool{}
		for _, entry := range entries {
			episode := entryEpisode(t, entry)
			got[entry["first_aired"].(string)] = true
			if episode["number"].(float64) == 2 && episode["season"].(float64) == 2 {
				t.Errorf("S02E02 is not a finale: the season's known count is 3: %+v", episode)
			}
		}
		if !got["2031-06-03"] || !got["2031-06-05"] {
			t.Errorf("unexpected finale dates: %+v", got)
		}
	})

	t.Run("my scopes the contract calendar to the caller's shows", func(t *testing.T) {
		now := time.Now().UTC()
		if err := db.Create(&database.UserWatchlist{
			UserID: "user-1", MediaType: "show", MediaID: showA, ListedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed show watchlist: %v", err)
		}
		entries := decodeEntries(t, doGet(t, app, "/v1/calendars/my/shows/2031-06-01/7"))
		if len(entries) != 6 {
			t.Fatalf("expected only show A's 6 episodes, got %d (%+v)", len(entries), entries)
		}
		for _, entry := range entries {
			if entryShowTitle(t, entry) != "Show A" {
				t.Errorf("my calendar leaked another show: %+v", entry)
			}
		}
	})
}

func TestMediaCalendar(t *testing.T) {
	db := setupReleaseDB(t)
	seedReleaseMovies(t, db)
	if _, _, err := seedShows(t, db); err != nil {
		t.Fatal(err)
	}
	app := makeFeedsApp()

	t.Run("movies and episodes merge into one date-ordered stream", func(t *testing.T) {
		entries := decodeEntries(t, doGet(t, app, "/v1/calendars/all/media/2031-06-01/7"))
		if len(entries) != 3+7 {
			t.Fatalf("expected 3 movies and 7 episodes, got %d", len(entries))
		}
		if entries[0]["released"] != "2031-06-02" {
			t.Errorf("movie entry missing its released date: %+v", entries[0])
		}
	})

	t.Run("type filter selects one kind", func(t *testing.T) {
		movies := decodeEntries(t, doGet(t, app, "/v1/calendars/all/media/2031-06-01/7?type=movie"))
		if len(movies) != 3 {
			t.Fatalf("expected 3 movies, got %d", len(movies))
		}
		shows := decodeEntries(t, doGet(t, app, "/v1/calendars/all/media/2031-06-01/7?type=show"))
		if len(shows) != 7 {
			t.Fatalf("expected 7 episodes, got %d", len(shows))
		}
	})

	t.Run("invalid type is 400", func(t *testing.T) {
		resp := doGet(t, app, "/v1/calendars/all/media/2031-06-01/7?type=person")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})
}

func TestHotCalendarRanking(t *testing.T) {
	db := setupReleaseDB(t)
	ids := seedReleaseMovies(t, db)
	showA, showB, _ := seedShows(t, db)
	now := time.Now().UTC()

	// movie "physical": 2 distinct users on watchlist
	// movie "digital": 1 user's collection
	// movie "both": no signal -> not hot
	for _, user := range []string{"u1", "u2"} {
		if err := db.Create(&database.UserWatchlist{
			UserID: user, MediaType: "movie", MediaID: ids["physical"], ListedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed movie watchlist: %v", err)
		}
	}
	if err := db.Create(&database.UserCollection{
		UserID: "u3", MediaType: "movie", MediaID: ids["digital"], CollectedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed movie collection: %v", err)
	}

	// show A: 2 users on watchlist. show B: 1 user's episode history.
	for _, user := range []string{"u1", "u2"} {
		if err := db.Create(&database.UserWatchlist{
			UserID: user, MediaType: "show", MediaID: showA, ListedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed show watchlist: %v", err)
		}
	}
	var episodeB database.Episode
	if err := db.Where("show_id = ?", showB).First(&episodeB).Error; err != nil {
		t.Fatalf("find show B episode: %v", err)
	}
	if err := db.Create(&database.UserHistory{
		UserID: "u4", MediaType: "episode", MediaID: episodeB.ID, WatchedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed episode history: %v", err)
	}

	entries := decodeEntries(t, doGet(t, makeFeedsApp(), "/v1/calendars/releases/hot/2031-06-01/7"))
	if len(entries) != 4 {
		t.Fatalf("expected 4 hot titles (2 movies, 2 shows), got %d (%+v)", len(entries), entries)
	}
	// Score 2 first: the movie and the show tie, and the movie sorts first on
	// the date (both 06-02) then kind tie-break. Score 1 next: show B airs on
	// 06-02, before the 06-03 digital movie.
	if entryMovieTitle(t, entries[0]) != "Physical Only" {
		t.Errorf("rank 1 should be the 2-user movie, got %+v", entries[0])
	}
	if entryShowTitle(t, entries[1]) != "Show A" {
		t.Errorf("rank 2 should be the 2-user show, got %+v", entries[1])
	}
	if entryShowTitle(t, entries[2]) != "Show B" {
		t.Errorf("rank 3 should be the 1-user show, got %+v", entries[2])
	}
	if entryMovieTitle(t, entries[3]) != "Digital Only" {
		t.Errorf("rank 4 should be the 1-user movie, got %+v", entries[3])
	}
	for _, entry := range entries {
		if movie, ok := entry["movie"].(map[string]any); ok && movie["title"] == "Both Types" {
			t.Errorf("a title with no user signal must not be hot: %+v", entry)
		}
	}
}
