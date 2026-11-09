package tmdbmeta

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setTestClient points the package's provider client at an httptest server.
func setTestClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	t.Setenv("TMDB_API_KEY", "test-token")
	c, err := tmdb.New()
	if err != nil {
		t.Fatalf("tmdb.New: %v", err)
	}
	c.SetBaseURL(srv.URL)
	tmdbOnce.Do(func() {})
	tmdbClient = c
	tmdbClientErr = nil
}

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	models := []any{
		&database.Movie{}, &database.Show{}, &database.Season{}, &database.Episode{},
		&database.Person{}, &database.User{},
		&database.ContentReport{}, &database.MediaMetadata{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// scrobble_sessions declares DEFAULT now(), which sqlite cannot parse;
	// build its table by hand (the production schema comes from the migration).
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
		created_at datetime
	)`).Error; err != nil {
		t.Fatalf("create scrobble_sessions: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// fixtureServer counts requests per path so tests can prove the store serves
// the second request.
type fixtureServer struct {
	*httptest.Server
	mu     sync.Mutex
	counts map[string]int
}

func newFixtureServer(t *testing.T) *fixtureServer {
	t.Helper()
	fs := &fixtureServer{counts: map[string]int{}}
	mux := http.NewServeMux()

	video := func(key, site string) string {
		return fmt.Sprintf(`{"id":"v1","key":%q,"site":%q,"type":"Trailer","name":"Official Trailer","official":true,"iso_639_1":"en","iso_3166_1":"US","size":1080,"published_at":"2020-01-02T03:04:05.000Z"}`, key, site)
	}
	mux.HandleFunc("/movie/603/videos", jsonHandler(fs, func(w http.ResponseWriter) {
		_, _ = fmt.Fprintf(w, `{"results":[%s,%s]}`, video("abc123", "YouTube"), video("vimeo1", "Vimeo"))
	}))
	mux.HandleFunc("/tv/1396/videos", jsonHandler(fs, func(w http.ResponseWriter) {
		_, _ = fmt.Fprintf(w, `{"results":[%s]}`, video("showvid", "YouTube"))
	}))
	mux.HandleFunc("/tv/1396/season/1/videos", jsonHandler(fs, func(w http.ResponseWriter) {
		_, _ = fmt.Fprintf(w, `{"results":[%s]}`, video("seasonvid", "YouTube"))
	}))
	mux.HandleFunc("/tv/1396/season/1/episode/1/videos", jsonHandler(fs, func(w http.ResponseWriter) {
		_, _ = fmt.Fprintf(w, `{"results":[%s]}`, video("epvid", "YouTube"))
	}))
	mux.HandleFunc("/movie/603/release_dates", jsonHandler(fs, func(w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, `{"results":[
			{"iso_3166_1":"US","release_dates":[
				{"certification":"PG-13","type":3,"release_date":"2010-07-16T00:00:00.000Z"},
				{"certification":"","type":4,"release_date":"2010-12-01T00:00:00.000Z"}]},
			{"iso_3166_1":"GB","release_dates":[
				{"certification":"12A","type":2,"release_date":"2010-07-16T00:00:00.000Z"}]}]}`)
	}))
	mux.HandleFunc("/movie/603/translations", jsonHandler(fs, func(w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, `{"translations":[
			{"iso_639_1":"en","iso_3166_1":"US","data":{"title":"Inception","overview":"Dreams","tagline":"Your mind is the scene"}},
			{"iso_639_1":"fr","iso_3166_1":"FR","data":{"title":"Inception FR","overview":"Reves","tagline":""}}]}`)
	}))
	mux.HandleFunc("/tv/1396/translations", jsonHandler(fs, func(w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, `{"translations":[{"iso_639_1":"en","iso_3166_1":"US","data":{"name":"Breaking Bad","overview":"Chemistry"}}]}`)
	}))
	mux.HandleFunc("/tv/1396/season/1/episode/1/translations", jsonHandler(fs, func(w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, `{"translations":[{"iso_639_1":"en","iso_3166_1":"US","data":{"name":"Pilot","overview":"Pilot ep"}}]}`)
	}))
	mux.HandleFunc("/tv/1396/season/1", jsonHandler(fs, func(w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, `{"id":3572,"season_number":1,"name":"Season 1","overview":"The first season","air_date":"2008-01-20","vote_average":8.9,"vote_count":120,"episodes":[]}`)
	}))
	mux.HandleFunc("/movie/603", jsonHandler(fs, func(w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, `{"id":603,"title":"Inception","production_companies":[{"id":420,"name":"Warner Bros.","origin_country":"US"}]}`)
	}))
	mux.HandleFunc("/tv/1396", jsonHandler(fs, func(w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, `{"id":1396,"name":"Breaking Bad","networks":[{"id":174,"name":"AMC","origin_country":"US"}]}`)
	}))
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"status_message":"not found"}`, http.StatusNotFound)
	})

	fs.Server = httptest.NewServer(mux)
	t.Cleanup(fs.Close)
	return fs
}

func jsonHandler(fs *fixtureServer, body func(http.ResponseWriter)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		fs.counts[r.URL.Path]++
		fs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		body(w)
	}
}

func (fs *fixtureServer) hits(path string) int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.counts[path]
}

func seedMedia(t *testing.T, db *gorm.DB) (database.Movie, database.Show, database.Season, database.Episode) {
	t.Helper()
	now := time.Now().UTC()
	movie := database.Movie{TmdbID: 603, Title: "Inception", Metadata: database.JSONMap{}, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	show := database.Show{TmdbID: 1396, Title: "Breaking Bad", Certification: "TV-MA", Metadata: database.JSONMap{}, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	season := database.Season{ShowID: show.ID, TmdbShowID: 1396, SeasonNumber: 1, EpisodeCount: 7, Images: database.JSONMap{"poster": "https://img/p.jpg"}, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	episode := database.Episode{ShowID: show.ID, SeasonID: season.ID, EpisodeNumber: 1, Title: "Pilot", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	return movie, show, season, episode
}

func makeApp() *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	RegisterRoutes(app.Group("/v1"))
	return app
}

func getJSON(t *testing.T, app *fiber.App, path string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, buf
}

func TestVideos(t *testing.T) {
	db := testDB(t)
	srv := newFixtureServer(t)
	setTestClient(t, srv.Server)
	movie, show, _, _ := seedMedia(t, db)
	app := makeApp()

	cases := []struct {
		name  string
		path  string
		key   string
		count int
	}{
		{"movie", fmt.Sprintf("/v1/movies/%d/videos", movie.ID), "abc123", 1},
		{"show", fmt.Sprintf("/v1/shows/%d/videos", show.ID), "showvid", 1},
		{"season", fmt.Sprintf("/v1/shows/%d/seasons/1/videos", show.ID), "seasonvid", 1},
		{"episode", fmt.Sprintf("/v1/shows/%d/seasons/1/episodes/1/videos", show.ID), "epvid", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := getJSON(t, app, tc.path)
			if status != http.StatusOK {
				t.Fatalf("%s: status %d body %s", tc.path, status, body)
			}
			var out []map[string]any
			if err := json.Unmarshal(body, &out); err != nil {
				t.Fatalf("decode: %v (%s)", err, body)
			}
			if len(out) != tc.count {
				t.Fatalf("%s: got %d videos, want %d (%s)", tc.path, len(out), tc.count, body)
			}
			// A non-YouTube entry is skipped, and the YouTube key becomes a URL.
			if url := out[0]["url"]; url != "https://www.youtube.com/watch?v="+tc.key {
				t.Errorf("url = %v, want key %s", url, tc.key)
			}
			for _, field := range []string{"title", "url", "site", "type", "size", "official", "published_at", "country", "language"} {
				if _, ok := out[0][field]; !ok {
					t.Errorf("missing required field %q", field)
				}
			}
		})
	}

	// Fetch -> store: the second call for the movie must not hit the provider.
	before := srv.hits("/movie/603/videos")
	getJSON(t, app, fmt.Sprintf("/v1/movies/%d/videos", movie.ID))
	if after := srv.hits("/movie/603/videos"); after != before {
		t.Errorf("second movie videos call hit the provider (%d -> %d)", before, after)
	}

	// A missing movie is a 404, not an empty 200.
	if status, _ := getJSON(t, app, "/v1/movies/9999/videos"); status != http.StatusNotFound {
		t.Errorf("missing movie videos status = %d, want 404", status)
	}
}

func TestMovieReleases(t *testing.T) {
	db := testDB(t)
	srv := newFixtureServer(t)
	setTestClient(t, srv.Server)
	movie, _, _, _ := seedMedia(t, db)
	app := makeApp()

	status, body := getJSON(t, app, fmt.Sprintf("/v1/movies/%d/releases/US", movie.ID))
	if status != http.StatusOK {
		t.Fatalf("status %d body %s", status, body)
	}
	var us []map[string]any
	_ = json.Unmarshal(body, &us)
	if len(us) != 2 {
		t.Fatalf("US releases = %d, want 2 (%s)", len(us), body)
	}
	if us[0]["release_type"] != "theatrical" || us[1]["release_type"] != "digital" {
		t.Errorf("release types = %v/%v", us[0]["release_type"], us[1]["release_type"])
	}
	if us[0]["certification"] != "PG-13" {
		t.Errorf("certification = %v", us[0]["certification"])
	}

	_, gbBody := getJSON(t, app, fmt.Sprintf("/v1/movies/%d/releases/GB", movie.ID))
	var gb []map[string]any
	_ = json.Unmarshal(gbBody, &gb)
	if len(gb) != 1 || gb[0]["release_type"] != "theatrical" {
		t.Errorf("GB releases = %s", gbBody)
	}

	// A country with no releases is an empty array, not a placeholder.
	status, emptyBody := getJSON(t, app, fmt.Sprintf("/v1/movies/%d/releases/JP", movie.ID))
	if status != http.StatusOK || string(emptyBody) != "[]" {
		t.Errorf("JP releases = %d %s, want 200 []", status, emptyBody)
	}

	if status, _ := getJSON(t, app, fmt.Sprintf("/v1/movies/%d/releases/USA", movie.ID)); status != http.StatusBadRequest {
		t.Errorf("bad country status = %d, want 400", status)
	}
}

func TestTranslations(t *testing.T) {
	db := testDB(t)
	srv := newFixtureServer(t)
	setTestClient(t, srv.Server)
	movie, show, _, _ := seedMedia(t, db)
	app := makeApp()

	status, body := getJSON(t, app, fmt.Sprintf("/v1/movies/%d/translations", movie.ID))
	if status != http.StatusOK {
		t.Fatalf("status %d body %s", status, body)
	}
	var all []map[string]any
	_ = json.Unmarshal(body, &all)
	if len(all) != 2 {
		t.Fatalf("translations = %d, want 2 (%s)", len(all), body)
	}
	if all[0]["language"] != "en" || all[0]["title"] != "Inception" || all[0]["tagline"] != "Your mind is the scene" {
		t.Errorf("unexpected en translation: %v", all[0])
	}

	_, frBody := getJSON(t, app, fmt.Sprintf("/v1/movies/%d/translations?language=fr", movie.ID))
	var fr []map[string]any
	_ = json.Unmarshal(frBody, &fr)
	if len(fr) != 1 || fr[0]["language"] != "fr" {
		t.Errorf("fr filter = %s", frBody)
	}

	for _, path := range []string{
		fmt.Sprintf("/v1/shows/%d/translations", show.ID),
		fmt.Sprintf("/v1/shows/%d/seasons/1/episodes/1/translations", show.ID),
	} {
		if status, body := getJSON(t, app, path); status != http.StatusOK {
			t.Errorf("%s: status %d body %s", path, status, body)
		}
	}
}

func TestSeasonInfo(t *testing.T) {
	db := testDB(t)
	srv := newFixtureServer(t)
	setTestClient(t, srv.Server)
	_, show, season, _ := seedMedia(t, db)
	app := makeApp()

	status, body := getJSON(t, app, fmt.Sprintf("/v1/shows/%d/seasons/1/info", show.ID))
	if status != http.StatusOK {
		t.Fatalf("status %d body %s", status, body)
	}
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	if out["number"] != float64(1) || out["title"] != "Season 1" || out["overview"] != "The first season" {
		t.Errorf("season info wrong: %s", body)
	}
	ids, _ := out["ids"].(map[string]any)
	if ids["trakt"] != float64(season.ID) || ids["tmdb"] != float64(3572) {
		t.Errorf("season ids = %v", ids)
	}
}

func TestStudios(t *testing.T) {
	db := testDB(t)
	srv := newFixtureServer(t)
	setTestClient(t, srv.Server)
	movie, show, _, _ := seedMedia(t, db)
	app := makeApp()

	status, body := getJSON(t, app, fmt.Sprintf("/v1/movies/%d/studios", movie.ID))
	if status != http.StatusOK {
		t.Fatalf("movie studios status %d body %s", status, body)
	}
	var studios []map[string]any
	_ = json.Unmarshal(body, &studios)
	if len(studios) != 1 || studios[0]["name"] != "Warner Bros." {
		t.Fatalf("movie studios = %s", body)
	}
	ids, _ := studios[0]["ids"].(map[string]any)
	if ids["tmdb"] != float64(420) {
		t.Errorf("movie studio ids.tmdb = %v", ids["tmdb"])
	}
	if ids["trakt"] != nil {
		t.Errorf("movie studio ids.trakt = %v, want null (we hold no Trakt company ids)", ids["trakt"])
	}

	_, showBody := getJSON(t, app, fmt.Sprintf("/v1/shows/%d/studios", show.ID))
	var showStudios []map[string]any
	_ = json.Unmarshal(showBody, &showStudios)
	if len(showStudios) != 1 || showStudios[0]["name"] != "AMC" {
		t.Errorf("show studios = %s", showBody)
	}
}

func TestShowCertifications(t *testing.T) {
	db := testDB(t)
	srv := newFixtureServer(t)
	setTestClient(t, srv.Server)
	_, show, _, _ := seedMedia(t, db)
	app := makeApp()

	status, body := getJSON(t, app, fmt.Sprintf("/v1/shows/%d/certifications", show.ID))
	if status != http.StatusOK || string(body) != `"TV-MA"` {
		t.Errorf("certifications = %d %s, want 200 \"TV-MA\"", status, body)
	}
}

func TestWatching(t *testing.T) {
	db := testDB(t)
	srv := newFixtureServer(t)
	setTestClient(t, srv.Server)
	_, show, season, episode := seedMedia(t, db)

	now := time.Now().UTC()
	for _, u := range []database.User{{ID: "u1", Username: "alice"}, {ID: "u2", Username: "bob"}} {
		if err := db.Create(&u).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}
	epID := episode.ID
	sessions := []database.ScrobbleSession{
		{UserID: "u1", MediaType: "episode", MediaID: epID, EpisodeID: &epID, Action: "start", ExpiresAt: now.Add(time.Hour), CreatedAt: now},
		{UserID: "u2", MediaType: "episode", MediaID: epID, EpisodeID: &epID, Action: "start", ExpiresAt: now.Add(-time.Hour), CreatedAt: now},
	}
	for i := range sessions {
		if err := db.Create(&sessions[i]).Error; err != nil {
			t.Fatalf("seed session: %v", err)
		}
	}

	app := makeApp()
	for _, path := range []string{
		fmt.Sprintf("/v1/shows/%d/seasons/%d/episodes/%d/watching", show.ID, season.SeasonNumber, episode.EpisodeNumber),
		fmt.Sprintf("/v1/shows/%d/seasons/%d/watching", show.ID, season.SeasonNumber),
	} {
		status, body := getJSON(t, app, path)
		if status != http.StatusOK {
			t.Fatalf("%s: status %d body %s", path, status, body)
		}
		var out []map[string]any
		_ = json.Unmarshal(body, &out)
		if len(out) != 1 {
			t.Fatalf("%s: watching = %d, want 1 (expired session excluded) (%s)", path, len(out), body)
		}
		if out[0]["username"] != "alice" {
			t.Errorf("%s: watching user = %v", path, out[0]["username"])
		}
	}
}

func TestMetadataRowWrittenOnce(t *testing.T) {
	db := testDB(t)
	srv := newFixtureServer(t)
	setTestClient(t, srv.Server)
	movie, _, _, _ := seedMedia(t, db)
	app := makeApp()

	getJSON(t, app, fmt.Sprintf("/v1/movies/%d/videos", movie.ID))
	getJSON(t, app, fmt.Sprintf("/v1/movies/%d/videos", movie.ID))
	var count int64
	if err := db.Model(&database.MediaMetadata{}).Where("media_type = ? AND media_id = ? AND kind = ?", "movie", movie.ID, "videos").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("media_metadata rows = %d, want 1 (upserted, not duplicated)", count)
	}
	if n := srv.hits("/movie/603/videos"); n != 1 {
		t.Errorf("provider hits = %d, want 1", n)
	}
}
