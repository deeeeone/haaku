package media

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newStage5bDB migrates the user, social and scrobble tables the watching
// routes read on top of the media tables.
func newStage5bDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.AutoMigrate(database.MediaModels()...); err != nil {
		t.Fatalf("migrate media: %v", err)
	}
	if err := db.AutoMigrate(&database.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	// The model tag carries `DEFAULT now()`, which sqlite rejects; create the
	// real column shape directly, as the profile-read tests do.
	if err := db.Exec(`CREATE TABLE scrobble_sessions (
		id integer PRIMARY KEY AUTOINCREMENT,
		user_id text NOT NULL,
		media_type text NOT NULL,
		media_id integer NOT NULL,
		season_id integer,
		episode_id integer,
		action text NOT NULL,
		progress real NOT NULL DEFAULT 0,
		expires_at datetime NOT NULL,
		created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		t.Fatalf("create scrobble_sessions: %v", err)
	}
	if err := db.AutoMigrate(database.SocialModels()...); err != nil {
		t.Fatalf("migrate social: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// newTranslationsStub serves the two translation endpoints the routes hydrate
// from, counting requests.
func newTranslationsStub(t *testing.T) (*tmdb.Client, *int32) {
	t.Helper()
	var calls int32
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, payload string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			atomic.AddInt32(&calls, 1)
			next(w, r)
		}
	}
	mux.HandleFunc("/movie/603/translations", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, `{"id":603,"translations":[
			{"iso_3166_1":"FR","iso_639_1":"fr","data":{"title":"Matrix","overview":"Bienvenue","tagline":"Bienvenue dans la matrice"}},
			{"iso_3166_1":"US","iso_639_1":"en","data":{"title":"The Matrix","overview":"Welcome"}}
		]}`)
	}))
	mux.HandleFunc("/tv/1396/season/1/episode/1/translations", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, `{"id":1,"translations":[
			{"iso_3166_1":"FR","iso_639_1":"fr","data":{"name":"Pilote","overview":"Pilote FR"}},
			{"iso_3166_1":"US","iso_639_1":"en","data":{"name":"Pilot","overview":"Pilot EN"}}
		]}`)
	}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Logf("unexpected TMDB path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Setenv("TMDB_API_KEY", "test-token")
	client, err := tmdb.New()
	if err != nil {
		t.Fatalf("tmdb.New: %v", err)
	}
	client.SetBaseURL(srv.URL)
	return client, &calls
}

func TestPersonSummaryContractShapeAndTrailingSlash(t *testing.T) {
	db := newMetadataTestDB(t)
	setTMDBClient(t, nil)
	app := newMetadataTestApp(t)

	person := &database.Person{
		ID:                 1,
		TmdbID:             17419,
		ImdbID:             "nm0186505",
		Name:               "Bryan Cranston",
		Biography:          "An actor.",
		Birthday:           &database.Date{Time: time.Date(1956, 3, 7, 0, 0, 0, 0, time.UTC)},
		Gender:             2,
		KnownForDepartment: "Acting",
	}
	if err := db.Create(person).Error; err != nil {
		t.Fatalf("seed person: %v", err)
	}

	for _, path := range []string{"/v1/people/1", "/v1/people/1/"} {
		status, raw := metadataGET(t, app, path)
		if status != http.StatusOK {
			t.Fatalf("%s status = %d, want 200 (%s)", path, status, raw)
		}
		got := decodeObject(t, raw)
		if got["name"] != "Bryan Cranston" {
			t.Fatalf("%s name = %v", path, got["name"])
		}
		ids, ok := got["ids"].(map[string]any)
		if !ok {
			t.Fatalf("%s ids = %v", path, got["ids"])
		}
		if ids["tmdb"] != float64(17419) || ids["imdb"] != "nm0186505" {
			t.Fatalf("%s ids = %v", path, ids)
		}
		if _, ok := ids["trakt"].(float64); !ok {
			t.Fatalf("%s ids.trakt = %#v, want an integer", path, ids["trakt"])
		}
		if got["gender"] != "male" || got["known_for_department"] != "acting" {
			t.Fatalf("%s gender/department = %v/%v", path, got["gender"], got["known_for_department"])
		}
		if got["biography"] != "An actor." || got["birthday"] != "1956-03-07" {
			t.Fatalf("%s biography/birthday = %v/%v", path, got["biography"], got["birthday"])
		}
	}

	if status, raw := metadataGET(t, app, "/v1/people/999"); status != http.StatusNotFound {
		t.Fatalf("unknown person status = %d, want 404 (%s)", status, raw)
	}
}

func TestMovieTranslationsHydrateFilterAndServeStored(t *testing.T) {
	db := newMetadataTestDB(t)
	client, calls := newTranslationsStub(t)
	setTMDBClient(t, client)
	app := newMetadataTestApp(t)
	seedMovie(t, db, database.JSONMap{})

	status, raw := metadataGET(t, app, "/v1/movies/1/translations")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	items := decodeObjectArray(t, raw)
	if len(items) != 2 {
		t.Fatalf("translations = %v", items)
	}
	if items[0]["title"] != "Matrix" || items[0]["overview"] != "Bienvenue" {
		t.Fatalf("first translation = %v", items[0])
	}
	if items[0]["language"] != "fr" || items[0]["country"] != "fr" || items[0]["tagline"] != "Bienvenue dans la matrice" {
		t.Fatalf("first translation fields = %v", items[0])
	}
	if items[1]["language"] != "en" || items[1]["tagline"] != nil {
		t.Fatalf("second translation = %v", items[1])
	}

	// Second read is stored: no provider call.
	if _, raw = metadataGET(t, app, "/v1/movies/1/translations"); len(decodeObjectArray(t, raw)) != 2 {
		t.Fatalf("second read = %s", raw)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("TMDB calls = %d, want 1", got)
	}

	// The language filter matches the language or the country code.
	_, raw = metadataGET(t, app, "/v1/movies/1/translations?language=fr")
	if filtered := decodeObjectArray(t, raw); len(filtered) != 1 || filtered[0]["title"] != "Matrix" {
		t.Fatalf("language filter = %v", filtered)
	}

	var stored database.Movie
	if err := db.First(&stored, 1).Error; err != nil {
		t.Fatalf("reload movie: %v", err)
	}
	if _, ok := stored.Metadata["translations"]; !ok {
		t.Fatalf("metadata has no translations key: %v", stored.Metadata)
	}
}

func TestEpisodeTranslationsHydrateFilterAndServeStored(t *testing.T) {
	db := newMetadataTestDB(t)
	client, calls := newTranslationsStub(t)
	setTMDBClient(t, client)
	app := newMetadataTestApp(t)
	seedShowSeasonEpisode(t, db)

	status, raw := metadataGET(t, app, "/v1/shows/1/seasons/1/episodes/1/translations")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	items := decodeObjectArray(t, raw)
	if len(items) != 2 || items[0]["title"] != "Pilote" || items[0]["language"] != "fr" {
		t.Fatalf("episode translations = %v", items)
	}
	if _, hasTagline := items[0]["tagline"]; !hasTagline {
		t.Fatalf("translation is missing tagline (must be null, not absent): %v", items[0])
	}

	if _, raw = metadataGET(t, app, "/v1/shows/1/seasons/1/episodes/1/translations?language=en"); true {
		filtered := decodeObjectArray(t, raw)
		if len(filtered) != 1 || filtered[0]["title"] != "Pilot" {
			t.Fatalf("episode language filter = %v", filtered)
		}
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("TMDB calls = %d, want 1", got)
	}
}

func TestTranslationsWithoutTMDBReturnEmptyArray(t *testing.T) {
	db := newMetadataTestDB(t)
	setTMDBClient(t, nil)
	app := newMetadataTestApp(t)
	seedMovie(t, db, database.JSONMap{})
	seedShowSeasonEpisode(t, db)

	for _, path := range []string{
		"/v1/movies/1/translations",
		"/v1/shows/1/seasons/1/episodes/1/translations",
	} {
		status, raw := metadataGET(t, app, path)
		if status != http.StatusOK {
			t.Fatalf("%s status = %d, want 200 (%s)", path, status, raw)
		}
		if items := decodeObjectArray(t, raw); len(items) != 0 {
			t.Fatalf("%s = %v, want []", path, items)
		}
	}
}

func TestWatchingExcludesPrivateAndPausedUsers(t *testing.T) {
	db := newStage5bDB(t)
	setTMDBClient(t, nil)
	app := newMetadataTestApp(t)
	seedShowSeasonEpisode(t, db)
	if err := db.Create(&database.Episode{ID: 2, SeasonID: 1, ShowID: 1, EpisodeNumber: 2, Title: "Second"}).Error; err != nil {
		t.Fatalf("seed episode 2: %v", err)
	}

	now := time.Now().UTC()
	for _, user := range []database.User{
		{ID: "u-public", Username: "public", Status: "active"},
		{ID: "u-other", Username: "other", Status: "active"},
		{ID: "u-private", Username: "private", Status: "active", IsPrivate: true},
		{ID: "u-paused", Username: "paused", Status: "active"},
	} {
		if err := db.Create(&user).Error; err != nil {
			t.Fatalf("seed user %s: %v", user.ID, err)
		}
	}

	seasonID := int64(1)
	ep1 := int64(1)
	ep2 := int64(2)
	sessions := []database.ScrobbleSession{
		{UserID: "u-public", MediaType: "episode", MediaID: 1, SeasonID: &seasonID, EpisodeID: &ep1, Action: "start", ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(-1 * time.Minute)},
		{UserID: "u-other", MediaType: "episode", MediaID: 2, SeasonID: &seasonID, EpisodeID: &ep2, Action: "scrobble", ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(-2 * time.Minute)},
		{UserID: "u-private", MediaType: "episode", MediaID: 1, SeasonID: &seasonID, EpisodeID: &ep1, Action: "start", ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(-3 * time.Minute)},
		{UserID: "u-paused", MediaType: "episode", MediaID: 1, SeasonID: &seasonID, EpisodeID: &ep1, Action: "pause", ExpiresAt: now.Add(time.Hour), CreatedAt: now},
	}
	for i := range sessions {
		if err := db.Create(&sessions[i]).Error; err != nil {
			t.Fatalf("seed session: %v", err)
		}
	}

	status, raw := metadataGET(t, app, "/v1/shows/1/seasons/1/watching")
	if status != http.StatusOK {
		t.Fatalf("season watching status = %d, want 200 (%s)", status, raw)
	}
	seasonViewers := decodeObjectArray(t, raw)
	if len(seasonViewers) != 2 {
		t.Fatalf("season viewers = %v, want the two public accounts", seasonViewers)
	}
	seen := map[string]bool{}
	for _, viewer := range seasonViewers {
		seen[viewer["username"].(string)] = true
		if viewer["username"] == "private" || viewer["username"] == "paused" {
			t.Fatalf("hidden account leaked into season watching: %v", viewer)
		}
		ids, _ := viewer["ids"].(map[string]any)
		if _, ok := ids["trakt"].(float64); !ok {
			t.Fatalf("profile ids.trakt = %#v, want an integer", ids["trakt"])
		}
	}
	if !seen["public"] || !seen["other"] {
		t.Fatalf("season viewers = %v", seen)
	}

	status, raw = metadataGET(t, app, "/v1/shows/1/seasons/1/episodes/1/watching")
	if status != http.StatusOK {
		t.Fatalf("episode watching status = %d, want 200 (%s)", status, raw)
	}
	episodeViewers := decodeObjectArray(t, raw)
	if len(episodeViewers) != 1 || episodeViewers[0]["username"] != "public" {
		t.Fatalf("episode viewers = %v, want only the public account on episode 1", episodeViewers)
	}

	for _, path := range []string{
		"/v1/shows/999/seasons/1/watching",
		"/v1/shows/1/seasons/9/watching",
		"/v1/shows/1/seasons/1/episodes/9/watching",
	} {
		if status, raw := metadataGET(t, app, path); status != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404 (%s)", path, status, raw)
		}
	}
}

func TestWatchingWithoutLiveSessionsIsEmptyArray(t *testing.T) {
	db := newStage5bDB(t)
	setTMDBClient(t, nil)
	app := newMetadataTestApp(t)
	seedShowSeasonEpisode(t, db)

	status, raw := metadataGET(t, app, "/v1/shows/1/seasons/1/watching")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	if viewers := decodeObjectArray(t, raw); len(viewers) != 0 {
		t.Fatalf("viewers = %v, want []", viewers)
	}
}
