package sync

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const lastActivitiesTestUser = "user-la-test"

// setupLastActivitiesDB mirrors setupRatingsDB: real PostgreSQL when
// DATABASE_URL is set (scripts/dev-pg.sh), in-memory sqlite otherwise, so CI
// runs with no services. last_activities is pure MAX() aggregation over
// user-scoped rows — no jsonb, no partial indexes — but the same harness lets
// the real-cluster run exercise it against PostgreSQL types.
func setupLastActivitiesDB(t *testing.T) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	var err error
	models := []interface{}{
		&database.UserHistory{}, &database.UserCollection{}, &database.UserWatchlist{},
		&database.UserRating{}, &database.UserFavorite{}, &database.UserHidden{},
		&database.Comment{}, &database.CommentLike{}, &database.List{}, &database.ListLike{},
		&database.UserNote{}, &database.UserFollow{}, &database.UserCheckin{}, &database.UserPlayback{},
	}
	// user_checkins and user_playback carry `DEFAULT now()` in their GORM tags;
	// sqlite has no now() function, so on the sqlite path they are created with
	// a portable DDL below instead of via AutoMigrate.
	sqliteModels := []interface{}{
		&database.UserHistory{}, &database.UserCollection{}, &database.UserWatchlist{},
		&database.UserRating{}, &database.UserFavorite{}, &database.UserHidden{},
		&database.Comment{}, &database.CommentLike{}, &database.List{}, &database.ListLike{},
		&database.UserNote{}, &database.UserFollow{},
	}
	if url := os.Getenv("DATABASE_URL"); url != "" {
		db, err = gorm.Open(postgres.Open(url), &gorm.Config{})
		if err != nil {
			t.Fatalf("open PostgreSQL: %v", err)
		}
		if err := db.Migrator().DropTable(models...); err != nil {
			t.Fatalf("drop stale PG tables: %v", err)
		}
		if err := db.AutoMigrate(models...); err != nil {
			t.Fatalf("migrate test DB: %v", err)
		}
	} else {
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatalf("open in-memory sqlite: %v", err)
		}
		if err := db.AutoMigrate(sqliteModels...); err != nil {
			t.Fatalf("migrate test DB: %v", err)
		}
		for _, ddl := range []string{
			`CREATE TABLE user_playback (
				user_id TEXT NOT NULL, media_type TEXT NOT NULL, media_id INTEGER NOT NULL,
				progress REAL NOT NULL DEFAULT 0, paused_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE (user_id, media_type, media_id))`,
			`CREATE TABLE user_checkins (
				id INTEGER PRIMARY KEY AUTOINCREMENT, user_id TEXT NOT NULL, media_type TEXT NOT NULL,
				media_id INTEGER NOT NULL, season_id INTEGER, episode_id INTEGER, message TEXT,
				expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				CONSTRAINT uq_user_checkins UNIQUE (user_id))`,
		} {
			if err := db.Exec(ddl).Error; err != nil {
				t.Fatalf("create sqlite table: %v", err)
			}
		}
	}
	database.SetDBForTest(t, db)
	return db
}

// newLastActivitiesApp mounts only GET /v1/sync/last_activities with a stub auth
// middleware that pins user_id, so the handler is exercised without a token.
func newLastActivitiesApp(userID string) *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1")
	v1.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	})
	v1.Get("/sync/last_activities", handleGetLastActivities)
	return app
}

func doGET(t *testing.T, app *fiber.App, path string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s failed: %v", path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, raw
}

// TestLastActivitiesEmptyAccountAllNulls is the first honesty rule: a caller
// with no rows anywhere gets every section null — never an object of nulls or a
// placeholder "now".
func TestLastActivitiesEmptyAccountAllNulls(t *testing.T) {
	setupLastActivitiesDB(t)
	app := newLastActivitiesApp("user-with-nothing")

	status, raw := doGET(t, app, "/v1/sync/last_activities")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", status, raw)
	}

	var got LastActivitiesResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	// Decode into a raw map too, so a dropped key (vs. an explicit null) is
	// caught: the contract lists every section.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode map: %v", err)
	}
	for _, key := range []string{
		"all", "movies", "episodes", "shows", "seasons", "ratings", "watchlist",
		"favorites", "collection", "history", "comments", "notes", "lists",
		"follows", "checkins", "playback",
	} {
		body, ok := m[key]
		if !ok {
			t.Errorf("section %q missing from response", key)
			continue
		}
		if string(body) != "null" {
			t.Errorf("section %q should be null for an empty account, got %s", key, body)
		}
	}
	if got.All != nil {
		t.Errorf("all should be null, got %v", *got.All)
	}
}

// TestLastActivitiesSeededSections checks each section against a real aggregate
// over the owning table, that unsupported Trakt sub-fields are explicit nulls,
// that sections with no rows stay null, and that `all` is the latest activity.
// It also seeds a second user with a later timestamp to pin caller scoping.
func TestLastActivitiesSeededSections(t *testing.T) {
	db := setupLastActivitiesDB(t)
	u := lastActivitiesTestUser

	ts := func(d int) time.Time { return time.Date(2026, 1, d, 10, 0, 0, 0, time.UTC) }
	st := func(d int) string { return ts(d).Format("2006-01-02T15:04:05.000Z") }
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	mid := func(v int64) *int64 { return &v }

	// movies
	must(db.Create(&database.UserHistory{UserID: u, MediaType: "movie", MediaID: 1, WatchedAt: ts(1), Source: "api", CreatedAt: ts(1)}).Error)
	must(db.Create(&database.UserCollection{UserID: u, MediaType: "movie", MediaID: 1, CollectedAt: ts(2), CreatedAt: ts(2)}).Error)
	must(db.Create(&database.UserRating{UserID: u, MediaType: "movie", MediaID: 1, Rating: 8, RatedAt: ts(3), CreatedAt: ts(3), UpdatedAt: ts(3)}).Error)
	must(db.Create(&database.UserWatchlist{UserID: u, MediaType: "movie", MediaID: 1, ListedAt: ts(4), CreatedAt: ts(4)}).Error)
	must(db.Create(&database.UserFavorite{UserID: u, MediaType: "movie", MediaID: 1, FavoritedAt: ts(5), CreatedAt: ts(5)}).Error)
	must(db.Create(&database.UserPlayback{UserID: u, MediaType: "movie", MediaID: 1, PausedAt: ts(6)}).Error)
	must(db.Create(&database.UserHidden{UserID: u, Section: "progress", MediaType: "movie", MediaID: 1, HiddenAt: ts(7)}).Error)

	// episodes: rated + collected (no episode history/watchlist/playback)
	must(db.Create(&database.UserRating{UserID: u, MediaType: "episode", MediaID: 2, Rating: 7, RatedAt: ts(8), CreatedAt: ts(8), UpdatedAt: ts(8)}).Error)
	must(db.Create(&database.UserCollection{UserID: u, MediaType: "episode", MediaID: 2, CollectedAt: ts(8), CreatedAt: ts(8)}).Error)

	// shows: rated + watchlisted
	must(db.Create(&database.UserRating{UserID: u, MediaType: "show", MediaID: 3, Rating: 9, RatedAt: ts(9), CreatedAt: ts(9), UpdatedAt: ts(9)}).Error)
	must(db.Create(&database.UserWatchlist{UserID: u, MediaType: "show", MediaID: 3, ListedAt: ts(10), CreatedAt: ts(10)}).Error)

	// seasons: rated only
	must(db.Create(&database.UserRating{UserID: u, MediaType: "season", MediaID: 4, Rating: 6, RatedAt: ts(11), CreatedAt: ts(11), UpdatedAt: ts(11)}).Error)

	// comments: a movie comment, a list comment, a show comment
	must(db.Create(&database.Comment{UserID: u, MediaType: "movie", MediaID: mid(1), CommentType: "shout", Body: "m", CreatedAt: ts(12), UpdatedAt: ts(12)}).Error)
	must(db.Create(&database.Comment{UserID: u, ListID: mid(50), CommentType: "shout", Body: "l", CreatedAt: ts(13), UpdatedAt: ts(13)}).Error)
	must(db.Create(&database.Comment{UserID: u, MediaType: "show", MediaID: mid(3), CommentType: "shout", Body: "s", CreatedAt: ts(14), UpdatedAt: ts(14)}).Error)
	must(db.Create(&database.CommentLike{CommentID: 999, UserID: u, CreatedAt: ts(15)}).Error)

	// lists: updated_at + liked_at
	must(db.Create(&database.List{ID: 50, UserID: u, Name: "L", Slug: "l", Privacy: "public", CreatedAt: ts(16), UpdatedAt: ts(17)}).Error)
	must(db.Create(&database.ListLike{ListID: 50, UserID: u, CreatedAt: ts(18)}).Error)

	// notes
	must(db.Create(&database.UserNote{UserID: u, MediaType: "movie", MediaID: 1, Note: "n", CreatedAt: ts(19), UpdatedAt: ts(20)}).Error)

	// follows / checkins
	must(db.Create(&database.UserFollow{FollowerID: u, FollowingID: "other", CreatedAt: ts(21)}).Error)
	must(db.Create(&database.UserCheckin{UserID: u, MediaType: "movie", MediaID: 1, ExpiresAt: ts(23), CreatedAt: ts(22)}).Error)

	// A DIFFERENT user with far later activity must not leak into the response.
	must(db.Create(&database.UserHistory{UserID: "someone-else", MediaType: "movie", MediaID: 9, WatchedAt: ts(28), Source: "api", CreatedAt: ts(28)}).Error)

	app := newLastActivitiesApp(u)
	status, raw := doGET(t, app, "/v1/sync/last_activities")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", status, raw)
	}
	var got LastActivitiesResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}

	eq := func(name string, p *string, want string) {
		t.Helper()
		if p == nil {
			t.Errorf("%s: expected %s, got nil", name, want)
			return
		}
		if *p != want {
			t.Errorf("%s: expected %s, got %s", name, want, *p)
		}
	}
	isNil := func(name string, p *string) {
		t.Helper()
		if p != nil {
			t.Errorf("%s: expected null, got %s", name, *p)
		}
	}

	if got.Movies == nil {
		t.Fatal("movies section must not be null")
	}
	eq("movies.watched_at", got.Movies.WatchedAt, st(1))
	eq("movies.collected_at", got.Movies.CollectedAt, st(2))
	eq("movies.rated_at", got.Movies.RatedAt, st(3))
	eq("movies.watchlisted_at", got.Movies.WatchlistedAt, st(4))
	eq("movies.favorited_at", got.Movies.FavoritedAt, st(5))
	eq("movies.paused_at", got.Movies.PausedAt, st(6))
	eq("movies.hidden_at", got.Movies.HiddenAt, st(7))
	eq("movies.commented_at", got.Movies.CommentedAt, st(12))

	if got.Episodes == nil {
		t.Error("episodes section must not be null (episode rating seeded)")
	} else {
		isNil("episodes.watched_at", got.Episodes.WatchedAt)
		eq("episodes.rated_at", got.Episodes.RatedAt, st(8))
		eq("episodes.collected_at", got.Episodes.CollectedAt, st(8))
		// Explicitly unsupported: null, not invented.
		isNil("episodes.watchlisted_at", got.Episodes.WatchlistedAt)
	}

	if got.Shows == nil {
		t.Error("shows section must not be null")
	} else {
		eq("shows.rated_at", got.Shows.RatedAt, st(9))
		eq("shows.watchlisted_at", got.Shows.WatchlistedAt, st(10))
		eq("shows.commented_at", got.Shows.CommentedAt, st(14))
	}

	if got.Seasons == nil {
		t.Error("seasons section must not be null")
	} else {
		eq("seasons.rated_at", got.Seasons.RatedAt, st(11))
		isNil("seasons.watchlisted_at", got.Seasons.WatchlistedAt)
	}

	eq("ratings.updated_at", got.Ratings.UpdatedAt, st(11))     // latest rating row updated_at
	eq("watchlist.updated_at", got.Watchlist.UpdatedAt, st(10)) // latest watchlist row created_at
	eq("favorites.updated_at", got.Favorites.UpdatedAt, st(5))
	eq("collection.updated_at", got.Collection.UpdatedAt, st(8)) // newest collection row is the episode
	eq("history.updated_at", got.History.UpdatedAt, st(1))
	eq("comments.liked_at", got.Comments.LikedAt, st(15))
	eq("notes.updated_at", got.Notes.UpdatedAt, st(20))
	eq("lists.liked_at", got.Lists.LikedAt, st(18))
	eq("lists.updated_at", got.Lists.UpdatedAt, st(17))
	eq("lists.commented_at", got.Lists.CommentedAt, st(13))
	eq("follows.updated_at", got.Follows.UpdatedAt, st(21))
	eq("checkins.updated_at", got.Checkins.UpdatedAt, st(22))
	eq("playback.updated_at", got.Playback.UpdatedAt, st(6))

	// `all` is the greatest activity across every section (the checkin).
	eq("all", got.All, st(22))
}

// TestLastActivitiesScopesToCaller pins that another user's newer rows never
// surface in the caller's timestamps (the aggregates are user-scoped).
func TestLastActivitiesScopesToCaller(t *testing.T) {
	db := setupLastActivitiesDB(t)

	// Another user has activity; the caller has none.
	if err := db.Create(&database.UserHistory{
		UserID: "someone-else", MediaType: "movie", MediaID: 9,
		WatchedAt: time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC), Source: "api",
		CreatedAt: time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatalf("seed other user: %v", err)
	}

	app := newLastActivitiesApp(lastActivitiesTestUser)
	status, raw := doGET(t, app, "/v1/sync/last_activities")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", status, raw)
	}
	var got LastActivitiesResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	if got.Movies != nil {
		t.Errorf("movies must be null; another user's history leaked (watched_at=%v)", got.Movies.WatchedAt)
	}
	if got.All != nil {
		t.Errorf("all must be null for a caller with no rows, got %s", *got.All)
	}
}

// TestLastActivitiesEpisodeCollectionWithoutHistory is the case the episode
// collection writes create: a collection row with no episode history or rating.
// episodeOrNil used to ignore CollectedAt and report the whole section as null.
func TestLastActivitiesEpisodeCollectionWithoutHistory(t *testing.T) {
	db := setupLastActivitiesDB(t)
	u := lastActivitiesTestUser
	at := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	if err := db.Create(&database.UserCollection{
		UserID: u, MediaType: "episode", MediaID: 7, CollectedAt: at, CreatedAt: at,
	}).Error; err != nil {
		t.Fatal(err)
	}

	app := newLastActivitiesApp(u)
	status, raw := doGET(t, app, "/v1/sync/last_activities")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", status, raw)
	}
	var got LastActivitiesResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Episodes == nil {
		t.Fatalf("episodes = null, want the collected episode represented: %s", raw)
	}
	if got.Episodes.CollectedAt == nil {
		t.Errorf("episodes.collected_at = null, want the collection timestamp")
	}
}
