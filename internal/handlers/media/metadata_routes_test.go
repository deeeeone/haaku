package media

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// --- harness ---

func newMetadataTestDB(t *testing.T) *gorm.DB {
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
	database.SetDBForTest(t, db)
	return db
}

func newMetadataTestApp(t *testing.T) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	RegisterRoutes(app.Group("/v1"))
	return app
}

// setTMDBClient points the package's TMDB singleton at a stub (or at nil, which
// is how "TMDB not configured" is exercised). No test may rely on the env.
func setTMDBClient(t *testing.T, client *tmdb.Client) {
	t.Helper()
	tmdbClientSingleton = client
	tmdbOnce = sync.Once{}
	tmdbOnce.Do(func() {})
	t.Cleanup(func() {
		tmdbClientSingleton = nil
		tmdbOnce = sync.Once{}
	})
}

// newTMDBStub serves the TMDB v3 routes the hydration paths call, counting
// requests so a test can prove a stored read makes none.
func newTMDBStub(t *testing.T) (*tmdb.Client, *int32) {
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

	mux.HandleFunc("/movie/603", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, stubMovieJSON())
	}))
	mux.HandleFunc("/tv/1396", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, stubShowJSON())
	}))
	mux.HandleFunc("/tv/1396/season/1", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, stubSeasonJSON())
	}))
	mux.HandleFunc("/tv/1396/season/1/episode/1", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, stubEpisodeJSON())
	}))
	mux.HandleFunc("/", auth(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("unexpected TMDB path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))

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

func metadataGET(t *testing.T, app *fiber.App, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: read body: %v", path, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, raw
}

func decodeObjectArray(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode array: %v (%s)", err, raw)
	}
	return out
}

func decodeObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode object: %v (%s)", err, raw)
	}
	return out
}

// --- fixtures ---

func seedMovie(t *testing.T, db *gorm.DB, meta database.JSONMap) {
	t.Helper()
	if err := db.Create(&database.Movie{ID: 1, TmdbID: 603, Title: "The Matrix", Slug: "the-matrix", Metadata: meta}).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
}

func seedShowSeasonEpisode(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Create(&database.Show{ID: 1, TmdbID: 1396, Title: "Breaking Bad", Slug: "breaking-bad"}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	if err := db.Create(&database.Season{ID: 1, ShowID: 1, TmdbShowID: 1396, SeasonNumber: 1, EpisodeCount: 1}).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	if err := db.Create(&database.Episode{ID: 1, SeasonID: 1, ShowID: 1, EpisodeNumber: 1, Title: "Pilot"}).Error; err != nil {
		t.Fatalf("seed episode: %v", err)
	}
}

func stubMovieJSON() string {
	return `{
		"id": 603,
		"title": "The Matrix",
		"videos": {"results": [
			{"key":"FVI84Dfx2-I","site":"YouTube","type":"Trailer","name":"Official Trailer","official":true,"size":1080,"published_at":"2019-07-18T16:00:00.000Z","iso_639_1":"en","iso_3166_1":"US"},
			{"key":"vimeoClip1","site":"Vimeo","type":"Clip","name":"Vimeo Clip","official":false,"size":720,"published_at":"2019-07-19T16:00:00.000Z","iso_639_1":"en","iso_3166_1":"GB"}
		]},
		"production_companies": [
			{"id":79,"name":"Village Roadshow Pictures","origin_country":"US"}
		],
		"release_dates": {"results": [
			{"iso_3166_1":"US","release_dates":[{"certification":"R","type":3,"release_date":"1999-03-31T00:00:00.000Z"}]},
			{"iso_3166_1":"GB","release_dates":[{"certification":"15","type":4,"release_date":"1999-06-11T00:00:00.000Z"}]}
		]}
	}`
}

func stubShowJSON() string {
	return `{
		"id": 1396,
		"name": "Breaking Bad",
		"videos": {"results": [
			{"key":"HhesaQXLuRY","site":"YouTube","type":"Trailer","name":"Show Trailer","official":true,"size":1080,"published_at":"2013-09-29T00:00:00.000Z","iso_639_1":"en","iso_3166_1":"US"}
		]},
		"production_companies": [
			{"id":1405,"name":"Sony Pictures Television","origin_country":"US"}
		],
		"content_ratings": {"results": [
			{"iso_3166_1":"US","rating":"TV-MA"},
			{"iso_3166_1":"GB","rating":"18"}
		]}
	}`
}

func stubSeasonJSON() string {
	return `{
		"id": 7001,
		"season_number": 1,
		"videos": {"results": [
			{"key":"seasonTeaser1","site":"YouTube","type":"Teaser","name":"Season 1 Teaser","official":true,"size":720,"published_at":"2008-01-01T00:00:00.000Z","iso_639_1":"en","iso_3166_1":"US"}
		]},
		"credits": {
			"cast": [
				{"id":17419,"name":"Bryan Cranston","character":"Walter White","order":0,"profile_path":"/bc.jpg","gender":2,"known_for_department":"Acting","adult":false}
			],
			"crew": [
				{"id":66633,"name":"Vince Gilligan","job":"Creator","department":"Production","profile_path":"/vg.jpg","gender":2,"known_for_department":"Writing","adult":false}
			]
		}
	}`
}

func stubEpisodeJSON() string {
	return `{
		"id": 7002,
		"season_number": 1,
		"episode_number": 1,
		"videos": {"results": [
			{"key":"episodeClip1","site":"YouTube","type":"Clip","name":"Episode Clip","official":false,"size":480,"published_at":"2008-01-20T00:00:00.000Z","iso_639_1":"en","iso_3166_1":"US"}
		]},
		"credits": {
			"cast": [],
			"guest_stars": [
				{"id":1234,"name":"Guest Actor","character":"Guest","order":0,"profile_path":"/ga.jpg","gender":2,"known_for_department":"Acting"}
			],
			"crew": [
				{"id":66634,"name":"Director Person","job":"Director","department":"Directing","profile_path":"/dp.jpg","gender":1,"known_for_department":"Directing"}
			]
		}
	}`
}

// --- videos ---

func TestMovieVideosServeStoredMetadata(t *testing.T) {
	db := newMetadataTestDB(t)
	setTMDBClient(t, nil)
	app := newMetadataTestApp(t)

	seedMovie(t, db, database.JSONMap{
		"videos": []any{map[string]any{
			"title":        "Trailer",
			"url":          "https://youtube.com/watch?v=abc",
			"site":         "YouTube",
			"type":         "trailer",
			"size":         1080,
			"official":     true,
			"published_at": "2020-01-01T00:00:00.000Z",
			"country":      "US",
			"language":     "en",
		}},
	})

	status, raw := metadataGET(t, app, "/v1/movies/1/videos")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	videos := decodeObjectArray(t, raw)
	if len(videos) != 1 {
		t.Fatalf("videos = %d, want 1 (%s)", len(videos), raw)
	}
	video := videos[0]
	if video["title"] != "Trailer" || video["url"] != "https://youtube.com/watch?v=abc" {
		t.Fatalf("video = %v", video)
	}
	if video["site"] != "YouTube" || video["type"] != "trailer" {
		t.Fatalf("site/type = %v/%v", video["site"], video["type"])
	}
	if video["size"] != float64(1080) || video["official"] != true {
		t.Fatalf("size/official = %v/%v", video["size"], video["official"])
	}
	if video["country"] != "US" || video["language"] != "en" || video["published_at"] != "2020-01-01T00:00:00.000Z" {
		t.Fatalf("country/language/published_at = %v/%v/%v", video["country"], video["language"], video["published_at"])
	}
}

func TestMovieVideosHydrateOnce(t *testing.T) {
	db := newMetadataTestDB(t)
	client, calls := newTMDBStub(t)
	setTMDBClient(t, client)
	app := newMetadataTestApp(t)
	seedMovie(t, db, database.JSONMap{})

	status, raw := metadataGET(t, app, "/v1/movies/1/videos")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	videos := decodeObjectArray(t, raw)
	if len(videos) != 2 {
		t.Fatalf("videos = %d, want 2 (YouTube + Vimeo) (%s)", len(videos), raw)
	}
	if videos[0]["type"] != "trailer" || videos[0]["url"] != "https://youtube.com/watch?v=FVI84Dfx2-I" {
		t.Fatalf("first video = %v", videos[0])
	}
	if videos[1]["site"] != "Vimeo" || videos[1]["url"] != "https://vimeo.com/vimeoClip1" {
		t.Fatalf("vimeo video = %v", videos[1])
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("TMDB calls = %d, want 1", got)
	}

	// The second read is served from the stored blob and makes no provider call.
	if status, raw = metadataGET(t, app, "/v1/movies/1/videos"); status != http.StatusOK {
		t.Fatalf("second status = %d (%s)", status, raw)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("second read made a provider call (%d total)", got)
	}

	var stored database.Movie
	if err := db.First(&stored, 1).Error; err != nil {
		t.Fatalf("reload movie: %v", err)
	}
	if _, ok := stored.Metadata["videos"]; !ok {
		t.Fatalf("metadata has no videos key after hydration: %v", stored.Metadata)
	}
}

func TestShowVideosHydrateOnce(t *testing.T) {
	db := newMetadataTestDB(t)
	client, calls := newTMDBStub(t)
	setTMDBClient(t, client)
	app := newMetadataTestApp(t)
	if err := db.Create(&database.Show{ID: 1, TmdbID: 1396, Title: "Breaking Bad"}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}

	status, raw := metadataGET(t, app, "/v1/shows/1/videos")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	videos := decodeObjectArray(t, raw)
	if len(videos) != 1 || videos[0]["title"] != "Show Trailer" {
		t.Fatalf("videos = %v", videos)
	}
	if _, raw = metadataGET(t, app, "/v1/shows/1/videos"); len(decodeObjectArray(t, raw)) != 1 {
		t.Fatalf("second read = %s", raw)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("TMDB calls = %d, want 1", got)
	}
}

func TestSeasonVideosHydrateOnce(t *testing.T) {
	db := newMetadataTestDB(t)
	client, calls := newTMDBStub(t)
	setTMDBClient(t, client)
	app := newMetadataTestApp(t)
	seedShowSeasonEpisode(t, db)

	status, raw := metadataGET(t, app, "/v1/shows/1/seasons/1/videos")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	videos := decodeObjectArray(t, raw)
	if len(videos) != 1 || videos[0]["title"] != "Season 1 Teaser" || videos[0]["type"] != "teaser" {
		t.Fatalf("season videos = %v", videos)
	}
	if _, raw = metadataGET(t, app, "/v1/shows/1/seasons/1/videos"); len(decodeObjectArray(t, raw)) != 1 {
		t.Fatalf("second read = %s", raw)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("TMDB calls = %d, want 1", got)
	}

	var stored database.Season
	if err := db.First(&stored, 1).Error; err != nil {
		t.Fatalf("reload season: %v", err)
	}
	if _, ok := stored.Metadata["videos"]; !ok {
		t.Fatalf("season metadata has no videos key: %v", stored.Metadata)
	}
}

func TestEpisodeVideosHydrateOnce(t *testing.T) {
	db := newMetadataTestDB(t)
	client, calls := newTMDBStub(t)
	setTMDBClient(t, client)
	app := newMetadataTestApp(t)
	seedShowSeasonEpisode(t, db)

	status, raw := metadataGET(t, app, "/v1/shows/1/seasons/1/episodes/1/videos")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	videos := decodeObjectArray(t, raw)
	if len(videos) != 1 || videos[0]["title"] != "Episode Clip" || videos[0]["type"] != "clip" {
		t.Fatalf("episode videos = %v", videos)
	}
	if _, raw = metadataGET(t, app, "/v1/shows/1/seasons/1/episodes/1/videos"); len(decodeObjectArray(t, raw)) != 1 {
		t.Fatalf("second read = %s", raw)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("TMDB calls = %d, want 1", got)
	}
}

// --- studios / releases / certifications ---

func TestMovieStudiosHydrateOnce(t *testing.T) {
	db := newMetadataTestDB(t)
	client, calls := newTMDBStub(t)
	setTMDBClient(t, client)
	app := newMetadataTestApp(t)
	seedMovie(t, db, database.JSONMap{})

	status, raw := metadataGET(t, app, "/v1/movies/1/studios")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	studios := decodeObjectArray(t, raw)
	if len(studios) != 1 || studios[0]["name"] != "Village Roadshow Pictures" {
		t.Fatalf("studios = %v", studios)
	}
	if studios[0]["country"] != "US" {
		t.Fatalf("studio country = %v", studios[0]["country"])
	}
	ids, ok := studios[0]["ids"].(map[string]any)
	if !ok {
		t.Fatalf("studio ids = %v", studios[0]["ids"])
	}
	if ids["tmdb"] != float64(79) || ids["slug"] != "village-roadshow-pictures" {
		t.Fatalf("studio ids = %v", ids)
	}
	if trakt, _ := ids["trakt"].(float64); trakt <= 0 {
		t.Fatalf("studio ids.trakt = %v, want a positive stable number", ids["trakt"])
	}

	if _, raw = metadataGET(t, app, "/v1/movies/1/studios"); len(decodeObjectArray(t, raw)) != 1 {
		t.Fatalf("second read = %s", raw)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("TMDB calls = %d, want 1", got)
	}
}

func TestShowStudiosAndCertificationsHydrateOnce(t *testing.T) {
	db := newMetadataTestDB(t)
	client, calls := newTMDBStub(t)
	setTMDBClient(t, client)
	app := newMetadataTestApp(t)
	if err := db.Create(&database.Show{ID: 1, TmdbID: 1396, Title: "Breaking Bad"}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}

	status, raw := metadataGET(t, app, "/v1/shows/1/studios")
	if status != http.StatusOK {
		t.Fatalf("studios status = %d (%s)", status, raw)
	}
	studios := decodeObjectArray(t, raw)
	if len(studios) != 1 || studios[0]["name"] != "Sony Pictures Television" {
		t.Fatalf("studios = %v", studios)
	}

	// The studios read stored the certifications too; this one costs no call.
	status, raw = metadataGET(t, app, "/v1/shows/1/certifications")
	if status != http.StatusOK {
		t.Fatalf("certifications status = %d (%s)", status, raw)
	}
	certifications := decodeObjectArray(t, raw)
	if len(certifications) != 2 {
		t.Fatalf("certifications = %v", certifications)
	}
	if certifications[0]["country"] != "us" || certifications[0]["certification"] != "TV-MA" {
		t.Fatalf("certifications[0] = %v", certifications[0])
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("TMDB calls = %d, want 1", got)
	}
}

func TestMovieReleasesFilterAndAll(t *testing.T) {
	db := newMetadataTestDB(t)
	setTMDBClient(t, nil)
	app := newMetadataTestApp(t)

	seedMovie(t, db, database.JSONMap{
		"releases": []any{
			map[string]any{"country": "us", "certification": "R", "release_date": "1999-03-31T00:00:00.000Z", "release_type": "theatrical", "note": nil},
			map[string]any{"country": "gb", "certification": "15", "release_date": "1999-06-11T00:00:00.000Z", "release_type": "digital", "note": nil},
		},
	})

	status, raw := metadataGET(t, app, "/v1/movies/1/releases/us")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	us := decodeObjectArray(t, raw)
	if len(us) != 1 || us[0]["country"] != "us" || us[0]["release_type"] != "theatrical" {
		t.Fatalf("us releases = %v", us)
	}
	if us[0]["certification"] != "R" || us[0]["release_date"] != "1999-03-31T00:00:00.000Z" {
		t.Fatalf("us release = %v", us[0])
	}

	_, raw = metadataGET(t, app, "/v1/movies/1/releases/all")
	if all := decodeObjectArray(t, raw); len(all) != 2 {
		t.Fatalf("all releases = %v", all)
	}
	_, raw = metadataGET(t, app, "/v1/movies/1/releases/zz")
	if none := decodeObjectArray(t, raw); len(none) != 0 {
		t.Fatalf("unknown country = %v, want []", none)
	}
}

// --- people ---

func TestSeasonPeopleHydrateOnce(t *testing.T) {
	db := newMetadataTestDB(t)
	client, calls := newTMDBStub(t)
	setTMDBClient(t, client)
	app := newMetadataTestApp(t)
	seedShowSeasonEpisode(t, db)

	status, raw := metadataGET(t, app, "/v1/shows/1/seasons/1/people")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	people := decodeObject(t, raw)
	cast, _ := people["cast"].([]any)
	if len(cast) != 1 {
		t.Fatalf("cast = %v", people["cast"])
	}
	member := cast[0].(map[string]any)
	if member["character"] != "Walter White" {
		t.Fatalf("cast character = %v", member["character"])
	}
	characters, _ := member["characters"].([]any)
	if len(characters) != 1 || characters[0] != "Walter White" {
		t.Fatalf("cast characters = %v", member["characters"])
	}
	person := member["person"].(map[string]any)
	if person["name"] != "Bryan Cranston" {
		t.Fatalf("person = %v", person)
	}
	crew, _ := people["crew"].(map[string]any)
	production, _ := crew["production"].([]any)
	if len(production) != 1 {
		t.Fatalf("crew.production = %v", crew)
	}
	crewMember := production[0].(map[string]any)
	if crewMember["job"] != "Creator" {
		t.Fatalf("crew job = %v", crewMember["job"])
	}
	jobs, _ := crewMember["jobs"].([]any)
	if len(jobs) != 1 || jobs[0] != "Creator" {
		t.Fatalf("crew jobs = %v", crewMember["jobs"])
	}

	// The season fetch stored the credits marker; a second read is free.
	if _, raw = metadataGET(t, app, "/v1/shows/1/seasons/1/people"); len(decodeObject(t, raw)) == 0 {
		t.Fatalf("second read = %s", raw)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("TMDB calls = %d, want 1", got)
	}

	var creditCount int64
	if err := db.Model(&database.Credit{}).Where("media_type = ?", "season").Count(&creditCount).Error; err != nil {
		t.Fatalf("count credits: %v", err)
	}
	if creditCount != 2 {
		t.Fatalf("season credits = %d, want 2 (cast + crew)", creditCount)
	}
}

func TestEpisodePeopleHydrateOnce(t *testing.T) {
	db := newMetadataTestDB(t)
	client, calls := newTMDBStub(t)
	setTMDBClient(t, client)
	app := newMetadataTestApp(t)
	seedShowSeasonEpisode(t, db)

	status, raw := metadataGET(t, app, "/v1/shows/1/seasons/1/episodes/1/people")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	people := decodeObject(t, raw)
	cast, _ := people["cast"].([]any)
	if len(cast) != 1 {
		t.Fatalf("cast = %v", people["cast"])
	}
	member := cast[0].(map[string]any)
	if member["character"] != "Guest" {
		t.Fatalf("guest character = %v", member["character"])
	}
	crew, _ := people["crew"].(map[string]any)
	if _, ok := crew["directing"]; !ok {
		t.Fatalf("crew has no directing department: %v", crew)
	}

	if _, raw = metadataGET(t, app, "/v1/shows/1/seasons/1/episodes/1/people"); len(decodeObject(t, raw)) == 0 {
		t.Fatalf("second read = %s", raw)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("TMDB calls = %d, want 1", got)
	}
}

// --- not configured / not found ---

func TestMetadataRoutesWithoutTMDBReturnComputedEmpty(t *testing.T) {
	db := newMetadataTestDB(t)
	setTMDBClient(t, nil)
	app := newMetadataTestApp(t)
	seedMovie(t, db, database.JSONMap{})
	seedShowSeasonEpisode(t, db)

	for _, path := range []string{
		"/v1/movies/1/videos",
		"/v1/movies/1/studios",
		"/v1/movies/1/releases/all",
		"/v1/shows/1/videos",
		"/v1/shows/1/studios",
		"/v1/shows/1/certifications",
		"/v1/shows/1/seasons/1/videos",
		"/v1/shows/1/seasons/1/episodes/1/videos",
	} {
		status, raw := metadataGET(t, app, path)
		if status != http.StatusOK {
			t.Fatalf("%s status = %d, want 200 (%s)", path, status, raw)
		}
		if items := decodeObjectArray(t, raw); len(items) != 0 {
			t.Fatalf("%s = %v, want []", path, items)
		}
	}

	status, raw := metadataGET(t, app, "/v1/shows/1/seasons/1/people")
	if status != http.StatusOK {
		t.Fatalf("people status = %d, want 200 (%s)", status, raw)
	}
	people := decodeObject(t, raw)
	if cast, _ := people["cast"].([]any); len(cast) != 0 {
		t.Fatalf("people cast = %v, want empty", people["cast"])
	}
	if crew, _ := people["crew"].(map[string]any); len(crew) != 0 {
		t.Fatalf("people crew = %v, want empty", people["crew"])
	}
}

func TestMetadataRoutesUnknownIDsAreNotFound(t *testing.T) {
	db := newMetadataTestDB(t)
	setTMDBClient(t, nil)
	app := newMetadataTestApp(t)
	seedShowSeasonEpisode(t, db)

	cases := []struct {
		path string
		want int
	}{
		{"/v1/movies/999/videos", http.StatusNotFound},
		{"/v1/movies/999/studios", http.StatusNotFound},
		{"/v1/movies/999/releases/all", http.StatusNotFound},
		{"/v1/shows/999/videos", http.StatusNotFound},
		{"/v1/shows/999/certifications", http.StatusNotFound},
		{"/v1/shows/1/seasons/9/videos", http.StatusNotFound},
		{"/v1/shows/1/seasons/not-a-number/videos", http.StatusNotFound},
		{"/v1/shows/1/seasons/1/episodes/9/people", http.StatusNotFound},
		{"/v1/shows/1/seasons/1/episodes/not-a-number/videos", http.StatusNotFound},
	}
	for _, tc := range cases {
		status, raw := metadataGET(t, app, tc.path)
		if status != tc.want {
			t.Fatalf("%s status = %d, want %d (%s)", tc.path, status, tc.want, raw)
		}
	}
}
