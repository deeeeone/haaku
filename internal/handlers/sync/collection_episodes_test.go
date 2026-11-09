package sync

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const collectionTestUser = "user-collection-test"

// setupCollectionDB mirrors the other sync handler tests: real PostgreSQL when
// DATABASE_URL is set, in-memory sqlite otherwise. Collection writes also emit
// outbox rows, hence that model is migrated too.
func setupCollectionDB(t *testing.T) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	var err error
	models := []interface{}{
		&database.Movie{}, &database.Show{}, &database.Season{}, &database.Episode{},
		&database.ExternalID{}, &database.UserCollection{}, &database.EventsOutbox{},
	}
	if url := os.Getenv("DATABASE_URL"); url != "" {
		db, err = gorm.Open(postgres.Open(url), &gorm.Config{})
		if err != nil {
			t.Fatalf("open PostgreSQL: %v", err)
		}
		if err := db.Migrator().DropTable(models...); err != nil {
			t.Fatalf("drop stale PG tables: %v", err)
		}
	} else {
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatalf("open in-memory sqlite: %v", err)
		}
		stripPostgresCastsForSQLite(t, db)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	stripPostgresCastsForSQLite(t, db)
	database.SetDBForTest(t, db)
	return db
}

// seedCollectionFixture creates a show with two seasons (three episodes in
// season 1, two in season 2) plus the external ids a client addresses it by.
// It returns the show and both seasons' episode ids in episode-number order.
func seedCollectionFixture(t *testing.T, db *gorm.DB) (showID int64, season1, season2 []int64) {
	t.Helper()
	now := time.Now().UTC()
	firstAired := database.Date{Time: time.Date(2008, 1, 20, 0, 0, 0, 0, time.UTC)}

	show := database.Show{
		Title: "Breaking Bad", TmdbID: 1396, TvdbID: 81189, ImdbID: "tt0903747",
		Slug: "breaking-bad", FirstAired: &firstAired, SeasonCount: 2, EpisodeCount: 5,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}

	for _, ext := range []database.ExternalID{
		{MediaType: "show", MediaID: show.ID, Source: "tvdb", ExternalID: "81189"},
		{MediaType: "show", MediaID: show.ID, Source: "tmdb", ExternalID: "1396"},
	} {
		if err := db.Create(&ext).Error; err != nil {
			t.Fatalf("seed show external id: %v", err)
		}
	}

	seedSeason := func(number, count int) []int64 {
		season := database.Season{ShowID: show.ID, SeasonNumber: number, EpisodeCount: count, CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&season).Error; err != nil {
			t.Fatalf("seed season %d: %v", number, err)
		}
		var ids []int64
		for i := 1; i <= count; i++ {
			ep := database.Episode{
				SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: i,
				Title: "S" + strconv.Itoa(number) + "E" + strconv.Itoa(i), CreatedAt: now, UpdatedAt: now,
			}
			if err := db.Create(&ep).Error; err != nil {
				t.Fatalf("seed episode %d/%d: %v", number, i, err)
			}
			ids = append(ids, ep.ID)
		}
		return ids
	}
	season1 = seedSeason(1, 3)
	season2 = seedSeason(2, 2)

	// The episode's own TVDB id: Trakt-shaped clients address a single episode
	// by it, while our history parser addresses it by show id + season/number.
	if err := db.Create(&database.ExternalID{
		MediaType: "episode", MediaID: season1[0], Source: "tvdb", ExternalID: "349232",
	}).Error; err != nil {
		t.Fatalf("seed episode external id: %v", err)
	}
	return show.ID, season1, season2
}

// newCollectionApp mounts the collection handlers with a stub auth middleware
// pinning user_id, so the tests exercise the handlers without a live token.
func newCollectionApp() *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1")
	v1.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", collectionTestUser)
		return c.Next()
	})
	v1.Get("/sync/collection/episodes", handleGetCollectionEpisodes)
	v1.Post("/sync/collection", handleAddCollection)
	v1.Post("/sync/collection/remove", handleRemoveCollection)
	return app
}

type collectionCountsBody struct {
	Added struct {
		Movies   int `json:"movies"`
		Shows    int `json:"shows"`
		Episodes int `json:"episodes"`
	} `json:"added"`
	Updated struct {
		Episodes int `json:"episodes"`
	} `json:"updated"`
	Existing struct {
		Movies   int `json:"movies"`
		Shows    int `json:"shows"`
		Episodes int `json:"episodes"`
	} `json:"existing"`
	Duplicates struct {
		Episodes int `json:"episodes"`
	} `json:"duplicates"`
	Deleted struct {
		Movies   int `json:"movies"`
		Shows    int `json:"shows"`
		Episodes int `json:"episodes"`
	} `json:"deleted"`
	NotFound struct {
		Movies   []map[string]any `json:"movies"`
		Shows    []map[string]any `json:"shows"`
		Seasons  []map[string]any `json:"seasons"`
		Episodes []map[string]any `json:"episodes"`
	} `json:"not_found"`
}

type collectedEpisodesBody []struct {
	Type        string `json:"type"`
	CollectedAt string `json:"collected_at"`
	UpdatedAt   string `json:"updated_at"`
	Episode     struct {
		Season int            `json:"season"`
		Number int            `json:"number"`
		Title  string         `json:"title"`
		IDs    map[string]any `json:"ids"`
	} `json:"episode"`
	Show struct {
		Title string         `json:"title"`
		Year  int            `json:"year"`
		IDs   map[string]any `json:"ids"`
	} `json:"show"`
}

func decodeCollectionCounts(t *testing.T, raw []byte) collectionCountsBody {
	t.Helper()
	var body collectionCountsBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode counts: %v (%s)", err, raw)
	}
	return body
}

func getCollectedEpisodes(t *testing.T, app *fiber.App) collectedEpisodesBody {
	t.Helper()
	status, raw := doGET(t, app, "/v1/sync/collection/episodes")
	if status != http.StatusOK {
		t.Fatalf("GET collection episodes: expected 200, got %d (%s)", status, raw)
	}
	var body collectedEpisodesBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode collected episodes: %v (%s)", err, raw)
	}
	return body
}

// TestAddCollectedEpisodeAndReadIt covers the single-episode path and the
// Trakt-shaped read: the row must carry the episode and its show, newest first.
func TestAddCollectedEpisodeAndReadIt(t *testing.T) {
	db := setupCollectionDB(t)
	seedCollectionFixture(t, db)
	app := newCollectionApp()

	payload := `{"episodes":[{"ids":{"tvdb":81189},"season":1,"number":1,"collected_at":"2026-11-09T10:00:00.000Z"}]}`
	status, raw := doPOST(t, app, "/v1/sync/collection", payload)
	if status != http.StatusOK {
		t.Fatalf("add episode: expected 200, got %d (%s)", status, raw)
	}
	counts := decodeCollectionCounts(t, raw)
	if counts.Added.Episodes != 1 || counts.Existing.Episodes != 0 {
		t.Fatalf("added.episodes = %d, existing.episodes = %d; want 1/0 (%s)",
			counts.Added.Episodes, counts.Existing.Episodes, raw)
	}
	if len(counts.NotFound.Episodes) != 0 {
		t.Fatalf("unexpected not_found.episodes: %s", raw)
	}

	got := getCollectedEpisodes(t, app)
	if len(got) != 1 {
		t.Fatalf("GET returned %d episodes, want 1", len(got))
	}
	item := got[0]
	if item.Type != "episode" {
		t.Errorf("type = %q, want episode", item.Type)
	}
	if item.CollectedAt != "2026-11-09T10:00:00.000Z" {
		t.Errorf("collected_at = %q, want the supplied timestamp", item.CollectedAt)
	}
	if item.UpdatedAt == "" {
		t.Error("updated_at must be present")
	}
	if item.Episode.Season != 1 || item.Episode.Number != 1 {
		t.Errorf("episode = S%dE%d, want S1E1", item.Episode.Season, item.Episode.Number)
	}
	if item.Episode.Title == "" {
		t.Error("episode.title is empty")
	}
	if item.Show.Title != "Breaking Bad" || item.Show.Year != 2008 {
		t.Errorf("show = %q (%d), want Breaking Bad (2008)", item.Show.Title, item.Show.Year)
	}
	if got := item.Show.IDs["tvdb"]; got != float64(81189) {
		t.Errorf("show.ids.tvdb = %v, want 81189", got)
	}
}

// TestAddCollectedEpisodeByItsOwnID covers the standalone episode shape: ids
// naming the episode itself, with no season/number to resolve through its show.
func TestAddCollectedEpisodeByItsOwnID(t *testing.T) {
	db := setupCollectionDB(t)
	seedCollectionFixture(t, db)
	app := newCollectionApp()

	payload := `{"episodes":[{"ids":{"tvdb":349232},"collected_at":"2026-11-09T10:00:00.000Z"}]}`
	status, raw := doPOST(t, app, "/v1/sync/collection", payload)
	if status != http.StatusOK {
		t.Fatalf("add episode by id: expected 200, got %d (%s)", status, raw)
	}
	counts := decodeCollectionCounts(t, raw)
	if counts.Added.Episodes != 1 {
		t.Fatalf("added.episodes = %d, want 1 (%s)", counts.Added.Episodes, raw)
	}
	if got := getCollectedEpisodes(t, app); len(got) != 1 || got[0].Episode.Number != 1 {
		t.Fatalf("GET after episode-id add = %+v, want S1E1", got)
	}
}

// TestAddingWholeSeasonDoesNotDoubleCount covers the hierarchy: the episode
// added on its own is "existing" when its season is added, and GET has no
// duplicate rows.
func TestAddingWholeSeasonDoesNotDoubleCount(t *testing.T) {
	db := setupCollectionDB(t)
	seedCollectionFixture(t, db)
	app := newCollectionApp()

	status, raw := doPOST(t, app, "/v1/sync/collection",
		`{"episodes":[{"ids":{"tvdb":81189},"season":1,"number":1}]}`)
	if status != http.StatusOK {
		t.Fatalf("add one episode: %d (%s)", status, raw)
	}

	status, raw = doPOST(t, app, "/v1/sync/collection",
		`{"shows":[{"ids":{"tvdb":81189},"seasons":[{"number":1}]}]}`)
	if status != http.StatusOK {
		t.Fatalf("add whole season: %d (%s)", status, raw)
	}
	counts := decodeCollectionCounts(t, raw)
	if counts.Added.Episodes != 2 {
		t.Errorf("added.episodes = %d, want 2", counts.Added.Episodes)
	}
	if counts.Existing.Episodes != 1 {
		t.Errorf("existing.episodes = %d, want 1", counts.Existing.Episodes)
	}

	got := getCollectedEpisodes(t, app)
	if len(got) != 3 {
		t.Fatalf("GET returned %d episodes, want 3", len(got))
	}
	seen := map[int]bool{}
	for _, item := range got {
		if seen[item.Episode.Number] {
			t.Errorf("duplicate episode number %d in GET", item.Episode.Number)
		}
		seen[item.Episode.Number] = true
	}
}

// TestTopLevelSeasonExpandsToItsEpisodes covers the body's `seasons` array:
// a season referenced by its show ids plus its number expands to every episode
// in that season.
func TestTopLevelSeasonExpandsToItsEpisodes(t *testing.T) {
	db := setupCollectionDB(t)
	seedCollectionFixture(t, db)
	app := newCollectionApp()

	status, raw := doPOST(t, app, "/v1/sync/collection",
		`{"seasons":[{"ids":{"tvdb":81189},"number":1}]}`)
	if status != http.StatusOK {
		t.Fatalf("add season: %d (%s)", status, raw)
	}
	counts := decodeCollectionCounts(t, raw)
	if counts.Added.Episodes != 3 {
		t.Fatalf("added.episodes = %d, want 3 (%s)", counts.Added.Episodes, raw)
	}
	if got := getCollectedEpisodes(t, app); len(got) != 3 {
		t.Fatalf("GET returned %d episodes, want 3", len(got))
	}

	status, raw = doPOST(t, app, "/v1/sync/collection/remove",
		`{"seasons":[{"ids":{"tvdb":81189},"number":1}]}`)
	if status != http.StatusOK {
		t.Fatalf("remove season: %d (%s)", status, raw)
	}
	counts = decodeCollectionCounts(t, raw)
	if counts.Deleted.Episodes != 3 {
		t.Fatalf("deleted.episodes = %d, want 3 (%s)", counts.Deleted.Episodes, raw)
	}
}

// TestRemovingOneEpisodeKeepsTheSeasonAndSeasonRemovalKeepsOtherSeasons covers
// both removal granularities.
func TestRemovingOneEpisodeKeepsTheSeasonAndSeasonRemovalKeepsOtherSeasons(t *testing.T) {
	db := setupCollectionDB(t)
	seedCollectionFixture(t, db)
	app := newCollectionApp()

	status, raw := doPOST(t, app, "/v1/sync/collection",
		`{"shows":[{"ids":{"tvdb":81189},"seasons":[{"number":1},{"number":2}]}]}`)
	if status != http.StatusOK {
		t.Fatalf("add seasons: %d (%s)", status, raw)
	}
	if got := getCollectedEpisodes(t, app); len(got) != 5 {
		t.Fatalf("GET after adding both seasons = %d episodes, want 5", len(got))
	}

	status, raw = doPOST(t, app, "/v1/sync/collection/remove",
		`{"episodes":[{"ids":{"tvdb":81189},"season":1,"number":2}]}`)
	if status != http.StatusOK {
		t.Fatalf("remove one episode: %d (%s)", status, raw)
	}
	counts := decodeCollectionCounts(t, raw)
	if counts.Deleted.Episodes != 1 {
		t.Errorf("deleted.episodes = %d, want 1", counts.Deleted.Episodes)
	}
	got := getCollectedEpisodes(t, app)
	if len(got) != 4 {
		t.Fatalf("GET after removing one episode = %d, want 4", len(got))
	}
	for _, item := range got {
		if item.Episode.Season == 1 && item.Episode.Number == 2 {
			t.Error("removed episode S1E2 is still collected")
		}
	}

	status, raw = doPOST(t, app, "/v1/sync/collection/remove",
		`{"shows":[{"ids":{"tvdb":81189},"seasons":[{"number":1}]}]}`)
	if status != http.StatusOK {
		t.Fatalf("remove season: %d (%s)", status, raw)
	}
	counts = decodeCollectionCounts(t, raw)
	if counts.Deleted.Episodes != 2 {
		t.Errorf("deleted.episodes = %d, want 2 (the remaining season-1 episodes)", counts.Deleted.Episodes)
	}
	got = getCollectedEpisodes(t, app)
	if len(got) != 2 {
		t.Fatalf("GET after removing season 1 = %d, want season 2's 2 episodes", len(got))
	}
	for _, item := range got {
		if item.Episode.Season != 2 {
			t.Errorf("season %d survived season-1 removal, want only season 2", item.Episode.Season)
		}
	}
}

// TestShowLevelCollectionStillWorks locks the untouched movie/show behaviour:
// a show with no seasons stores a show row and no episode rows.
func TestShowLevelCollectionStillWorks(t *testing.T) {
	db := setupCollectionDB(t)
	showID, _, _ := seedCollectionFixture(t, db)
	app := newCollectionApp()

	status, raw := doPOST(t, app, "/v1/sync/collection", `{"shows":[{"ids":{"tvdb":81189}}]}`)
	if status != http.StatusOK {
		t.Fatalf("add show: %d (%s)", status, raw)
	}
	counts := decodeCollectionCounts(t, raw)
	if counts.Added.Shows != 1 || counts.Added.Episodes != 0 {
		t.Fatalf("added.shows/episodes = %d/%d, want 1/0 (%s)",
			counts.Added.Shows, counts.Added.Episodes, raw)
	}

	var showRows, episodeRows int64
	db.Model(&database.UserCollection{}).Where("user_id = ? AND media_type = ?", collectionTestUser, "show").Count(&showRows)
	db.Model(&database.UserCollection{}).Where("user_id = ? AND media_type = ?", collectionTestUser, "episode").Count(&episodeRows)
	if showRows != 1 || episodeRows != 0 {
		t.Fatalf("stored show/episode rows = %d/%d, want 1/0", showRows, episodeRows)
	}
	var row database.UserCollection
	if err := db.Where("user_id = ? AND media_type = ? AND media_id = ?", collectionTestUser, "show", showID).First(&row).Error; err != nil {
		t.Fatalf("show collection row missing: %v", err)
	}

	status, raw = doPOST(t, app, "/v1/sync/collection/remove", `{"shows":[{"ids":{"tvdb":81189}}]}`)
	if status != http.StatusOK {
		t.Fatalf("remove show: %d (%s)", status, raw)
	}
	counts = decodeCollectionCounts(t, raw)
	if counts.Deleted.Shows != 1 {
		t.Errorf("deleted.shows = %d, want 1 (%s)", counts.Deleted.Shows, raw)
	}
}

// TestUnknownCollectionEpisodeIsReported covers the not_found contract.
func TestUnknownCollectionEpisodeIsReported(t *testing.T) {
	db := setupCollectionDB(t)
	seedCollectionFixture(t, db)
	app := newCollectionApp()

	status, raw := doPOST(t, app, "/v1/sync/collection",
		`{"episodes":[{"ids":{"tvdb":999999},"season":99,"number":1}]}`)
	if status != http.StatusOK {
		t.Fatalf("add unknown episode: %d (%s)", status, raw)
	}
	counts := decodeCollectionCounts(t, raw)
	if counts.Added.Episodes != 0 {
		t.Errorf("added.episodes = %d, want 0", counts.Added.Episodes)
	}
	if len(counts.NotFound.Episodes) != 1 {
		t.Errorf("not_found.episodes has %d entries, want 1 (%s)", len(counts.NotFound.Episodes), raw)
	}
	if got := getCollectedEpisodes(t, app); len(got) != 0 {
		t.Errorf("GET returned %d episodes after an unknown add, want 0", len(got))
	}
}

// TestCollectionEpisodesRequiresAuth runs the real middleware on the route.
func TestCollectionEpisodesRequiresAuth(t *testing.T) {
	setupCollectionDB(t)
	app := fiber.New()
	v1 := app.Group("/v1")
	v1.Use(auth.AuthRequired())
	v1.Get("/sync/collection/episodes", handleGetCollectionEpisodes)

	status, raw := doGET(t, app, "/v1/sync/collection/episodes")
	if status != http.StatusUnauthorized {
		t.Fatalf("GET without auth = %d, want 401 (%s)", status, raw)
	}
}
