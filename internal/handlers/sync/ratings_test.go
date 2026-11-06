package sync

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"trakt-api/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const ratingsTestUser = "user-ratings-test"

// setupRatingsDB opens a throwaway DB, migrates the tables the ratings handlers
// touch, and points the database package singleton at it via the test-only
// SetDBForTest seam so GetDB() returns this handle. It uses the real PostgreSQL
// when DATABASE_URL is set (see scripts/dev-pg.sh), and an in-memory sqlite
// otherwise, so CI runs it with no services.
func setupRatingsDB(t *testing.T) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	var err error
	if url := os.Getenv("DATABASE_URL"); url != "" {
		db, err = gorm.Open(postgres.Open(url), &gorm.Config{})
		if err != nil {
			t.Fatalf("open PostgreSQL: %v", err)
		}
		// Start from a clean slate: the shared throwaway cluster may already
		// carry tables from an earlier run, and AutoMigrate's index/constraint
		// reconciliation is not idempotent across those leftovers.
		if err := db.Migrator().DropTable(
			&database.Show{}, &database.Season{}, &database.Episode{},
			&database.ExternalID{}, &database.UserRating{}, &database.EventsOutbox{},
		); err != nil {
			t.Fatalf("drop stale PG tables: %v", err)
		}
	} else {
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatalf("open in-memory sqlite: %v", err)
		}
		stripPostgresCastsForSQLite(t, db)
	}
	models := []interface{}{
		&database.Show{}, &database.Season{}, &database.Episode{},
		&database.ExternalID{}, &database.UserRating{},
		&database.EventsOutbox{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	stripPostgresCastsForSQLite(t, db)
	database.SetDBForTest(t, db)
	return db
}

// stripPostgresCastsForSQLite removes the PostgreSQL-only "::jsonb" cast that
// outbox.PublishEvent bakes into its raw INSERT — correct on Postgres, a
// syntax error on sqlite. Without this the rating transaction rolls back on the
// outbox write and the handler returns 500. Test-only; the persisted rows are
// engine-independent.
func stripPostgresCastsForSQLite(t *testing.T, db *gorm.DB) {
	t.Helper()
	err := db.Callback().Raw().Before("gorm:raw").
		Register("test:strip_postgres_casts", func(tx *gorm.DB) {
			sql := tx.Statement.SQL.String()
			if strings.Contains(sql, "::jsonb") {
				tx.Statement.SQL.Reset()
				tx.Statement.SQL.WriteString(strings.ReplaceAll(sql, "::jsonb", ""))
			}
		})
	if err != nil {
		t.Fatalf("register sqlite compatibility callback: %v", err)
	}
}

// seedRatingsFixture creates a show with two episodes and the external id that
// maps the SERIES tmdb id (the id an episode payload carries) to the show row.
// Two episodes matter: a show-level write would rate one row, not the episode.
func seedRatingsFixture(t *testing.T, db *gorm.DB) (showID, ep1ID, ep2ID int64) {
	t.Helper()
	now := time.Now().UTC()

	show := database.Show{Title: "Rating Show", TmdbID: 4242, SeasonCount: 1, EpisodeCount: 2}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	season := database.Season{ShowID: show.ID, SeasonNumber: 1, EpisodeCount: 2, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	eps := make([]database.Episode, 2)
	for i := range eps {
		eps[i] = database.Episode{
			SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: i + 1,
			Title: "Episode", CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&eps[i]).Error; err != nil {
			t.Fatalf("seed episode %d: %v", i+1, err)
		}
	}
	if err := db.Create(&database.ExternalID{
		MediaType: "show", MediaID: show.ID, Source: "tmdb", ExternalID: "4242",
	}).Error; err != nil {
		t.Fatalf("seed external id: %v", err)
	}
	return show.ID, eps[0].ID, eps[1].ID
}

// newRatingsApp mounts just the ratings handlers with a stub auth middleware
// that pins user_id, so the test exercises the handlers without a live token.
func newRatingsApp() *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1")
	v1.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", ratingsTestUser)
		return c.Next()
	})
	v1.Post("/sync/ratings", handleAddRatings)
	v1.Post("/sync/ratings/remove", handleRemoveRatings)
	return app
}

func doPOST(t *testing.T, app *fiber.App, path, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("build request %s: %v", path, err)
	}
	req.Host = "example.com"
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s failed: %v", path, err)
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read body %s: %v", path, err)
	}
	return resp.StatusCode, raw
}

// TestAddEpisodeRatingTargetsEpisodeRow is the regression test for issue #25:
// rating an episode must write a user_ratings row for the EPISODE media id and
// leave the show unrated. It also round-trips the same payload through
// /sync/ratings/remove.
func TestAddEpisodeRatingTargetsEpisodeRow(t *testing.T) {
	db := setupRatingsDB(t)
	showID, _, ep2ID := seedRatingsFixture(t, db)
	app := newRatingsApp()

	payload := `{"episodes":[{"ids":{"tmdb":4242},"season":1,"number":2,"rating":9}]}`

	// Add.
	status, raw := doPOST(t, app, "/v1/sync/ratings", payload)
	if status != http.StatusOK {
		t.Fatalf("add: expected 200, got %d (%s)", status, raw)
	}
	var addBody struct {
		Added struct {
			Episodes int `json:"episodes"`
		} `json:"added"`
	}
	if err := json.Unmarshal(raw, &addBody); err != nil {
		t.Fatalf("decode add body: %v (%s)", err, raw)
	}
	if addBody.Added.Episodes != 1 {
		t.Errorf("expected added.episodes=1, got %d", addBody.Added.Episodes)
	}

	// The rating must land on the episode row.
	var epRating database.UserRating
	if err := db.Where("user_id = ? AND media_type = ? AND media_id = ?",
		ratingsTestUser, "episode", ep2ID).First(&epRating).Error; err != nil {
		t.Fatalf("expected an episode rating for media_id %d: %v", ep2ID, err)
	}
	if epRating.Rating != 9 {
		t.Errorf("expected episode rating 9, got %d", epRating.Rating)
	}

	// The show must NOT be rated — a show-level write is exactly the bug.
	var showCount int64
	if err := db.Model(&database.UserRating{}).
		Where("user_id = ? AND media_type = ? AND media_id = ?",
			ratingsTestUser, "show", showID).Count(&showCount).Error; err != nil {
		t.Fatalf("count show ratings: %v", err)
	}
	if showCount != 0 {
		t.Errorf("expected no rating on the show, found %d", showCount)
	}

	// Round-trip removal with the same payload.
	status, raw = doPOST(t, app, "/v1/sync/ratings/remove", payload)
	if status != http.StatusOK {
		t.Fatalf("remove: expected 200, got %d (%s)", status, raw)
	}
	var removeBody struct {
		Deleted int `json:"deleted"`
	}
	if err := json.Unmarshal(raw, &removeBody); err != nil {
		t.Fatalf("decode remove body: %v (%s)", err, raw)
	}
	if removeBody.Deleted != 1 {
		t.Errorf("expected deleted=1, got %d", removeBody.Deleted)
	}
	var afterCount int64
	if err := db.Model(&database.UserRating{}).
		Where("user_id = ? AND media_type = ? AND media_id = ?",
			ratingsTestUser, "episode", ep2ID).Count(&afterCount).Error; err != nil {
		t.Fatalf("count ratings after remove: %v", err)
	}
	if afterCount != 0 {
		t.Errorf("expected the episode rating to be removed, %d still present", afterCount)
	}
}

// TestAddEpisodeRatingWithoutSeasonNumberRejected guards the second half of the
// fix: a malformed episode reference must be rejected, never fall back to the
// series and rate the whole show.
func TestAddEpisodeRatingWithoutSeasonNumberRejected(t *testing.T) {
	db := setupRatingsDB(t)
	seedRatingsFixture(t, db)
	app := newRatingsApp()

	payload := `{"episodes":[{"ids":{"tmdb":4242},"rating":9}]}`
	status, raw := doPOST(t, app, "/v1/sync/ratings", payload)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", status, raw)
	}
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, raw)
	}
	if env.Error.Code != "invalid_request" {
		t.Errorf("expected error code invalid_request, got %q", env.Error.Code)
	}

	var total int64
	if err := db.Model(&database.UserRating{}).Where("user_id = ?", ratingsTestUser).
		Count(&total).Error; err != nil {
		t.Fatalf("count ratings: %v", err)
	}
	if total != 0 {
		t.Errorf("a malformed episode payload must not write any rating, found %d", total)
	}
}
