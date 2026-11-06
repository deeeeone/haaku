package users

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"trakt-api/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Harness: in-memory sqlite (real PostgreSQL when DATABASE_URL is set, same
// seam the sync handler tests use) + a stub auth middleware that pins the
// request's user_id so PrivacyFilter/AuthRequired see a caller.
// ---------------------------------------------------------------------------

func profileTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	var err error
	onPostgres := os.Getenv("DATABASE_URL") != ""
	if onPostgres {
		db, err = gorm.Open(postgres.Open(os.Getenv("DATABASE_URL")), &gorm.Config{})
		if err != nil {
			t.Fatalf("open PostgreSQL: %v", err)
		}
	} else {
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatalf("open in-memory sqlite: %v", err)
		}
	}
	models := []any{
		&database.User{},
		&database.UserFollow{},
		&database.UserBlock{},
		&database.UserHistory{},
		&database.UserCollection{},
		&database.UserWatchlist{},
		&database.UserRating{},
		&database.UserNote{},
		&database.Comment{},
		&database.CommentLike{},
		&database.List{},
		&database.ListLike{},
		&database.Movie{},
		&database.Show{},
		&database.ExternalID{},
	}
	if onPostgres {
		// PostgreSQL accepts `DEFAULT now()`; let AutoMigrate own the table.
		models = append(models, &database.ScrobbleSession{})
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	if !onPostgres {
		// scrobble_sessions carries `DEFAULT now()` in the model tags, which
		// sqlite rejects; create the real column shape directly (same approach
		// the export handler tests use for user_playback / user_checkins).
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
	}
	// Start from a clean slate: the shared throwaway cluster may already carry
	// rows from an earlier run, and the visibility assertions depend on there
	// being exactly the rows this test seeds.
	for _, table := range []string{
		"users", "user_follows", "user_blocks", "user_history", "user_collection",
		"user_watchlist", "user_ratings", "user_notes", "comments", "comment_likes",
		"lists", "list_likes", "scrobble_sessions", "external_ids", "movies", "shows",
	} {
		if err := db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	database.SetDBForTest(t, db)
	return db
}

// newProfileReadApp mounts the public-profile read surface exactly as the
// server does, with a stub auth middleware pinning callerID ("" = anonymous).
func newProfileReadApp(callerID string) *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1")
	v1.Use(func(c *fiber.Ctx) error {
		if callerID != "" {
			c.Locals("user_id", callerID)
		}
		return c.Next()
	})
	RegisterProfileReadRoutes(v1)
	return app
}

func doGET(t *testing.T, app *fiber.App, path string) (int, string) {
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
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read body %s: %v", path, err)
	}
	return resp.StatusCode, string(raw)
}

func seedUser(t *testing.T, db *gorm.DB, id string, private bool, status string) {
	t.Helper()
	u := database.User{
		ID:        id,
		Username:  "u-" + id,
		Name:      "User " + id,
		IsPrivate: private,
		Status:    status,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed user %s: %v", id, err)
	}
}

// ---------------------------------------------------------------------------
// Visibility: public / private / block both directions / deleted / unknown
// ---------------------------------------------------------------------------

func TestProfileReadsPublicProfileOKForStranger(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", false, "active")
	app := newProfileReadApp("stranger")

	status, body := doGET(t, app, "/v1/users/target/history")
	if status != http.StatusOK {
		t.Fatalf("public history: want 200, got %d (%s)", status, body)
	}
	if !strings.Contains(body, `"items":[]`) {
		t.Errorf("empty history must serialise as [], got %s", body)
	}
}

func TestProfileReadsPrivateProfileForbiddenForStranger(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", true, "active")
	app := newProfileReadApp("stranger")

	for _, path := range []string{
		"/v1/users/target/history",
		"/v1/users/target/ratings",
		"/v1/users/target/watchlist",
		"/v1/users/target/collection",
		"/v1/users/target/watching",
		"/v1/users/target/comments/all/all",
		"/v1/users/target/notes/all",
		"/v1/users/target/likes/comments",
		"/v1/users/target/likes/lists",
	} {
		status, body := doGET(t, app, path)
		if status != http.StatusForbidden {
			t.Errorf("%s: private profile to stranger: want 403, got %d (%s)", path, status, body)
		}
	}
}

func TestProfileReadsPrivateProfileAllowedForSelfAndFollower(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", true, "active")
	seedUser(t, db, "follower", false, "active")
	if err := db.Create(&database.UserFollow{
		FollowerID: "follower", FollowingID: "target", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed follow: %v", err)
	}

	for _, caller := range []string{"target", "follower"} {
		app := newProfileReadApp(caller)
		status, body := doGET(t, app, "/v1/users/target/collection")
		if status != http.StatusOK {
			t.Errorf("private profile to %s: want 200, got %d (%s)", caller, status, body)
		}
	}
}

func TestProfileReadsBlockedInEitherDirectionIs404(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", false, "active")
	seedUser(t, db, "stranger", false, "active")

	// stranger blocked target.
	if err := db.Create(&database.UserBlock{
		BlockerID: "stranger", BlockedID: "target", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed block: %v", err)
	}
	status, body := doGET(t, newProfileReadApp("stranger"), "/v1/users/target/history")
	if status != http.StatusNotFound {
		t.Errorf("blocker -> blocked: want 404, got %d (%s)", status, body)
	}

	// target blocked stranger (reverse direction, public profile).
	if err := db.Create(&database.UserBlock{
		BlockerID: "target", BlockedID: "stranger", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed reverse block: %v", err)
	}
	status, body = doGET(t, newProfileReadApp("stranger"), "/v1/users/target/history")
	if status != http.StatusNotFound {
		t.Errorf("blocked -> blocker: want 404, got %d (%s)", status, body)
	}
}

func TestProfileReadsDeletedAndPendingDeletionAre404(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "gone", false, "deleted")
	seedUser(t, db, "leaving", false, "pending_deletion")
	app := newProfileReadApp("stranger")

	for _, id := range []string{"gone", "leaving"} {
		status, body := doGET(t, app, "/v1/users/"+id+"/history")
		if status != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d (%s)", id, status, body)
		}
	}
}

func TestProfileReadsUnknownUserIs404(t *testing.T) {
	profileTestDB(t)
	app := newProfileReadApp("stranger")

	for _, path := range []string{
		"/v1/users/nobody/history",
		"/v1/users/nobody/collection",
		"/v1/users/nobody/watching",
	} {
		status, body := doGET(t, app, path)
		if status != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d (%s)", path, status, body)
		}
	}
}

// ---------------------------------------------------------------------------
// Pagination + empty collections
// ---------------------------------------------------------------------------

func seedHistory(t *testing.T, db *gorm.DB, userID string, n int, mediaType string) {
	t.Helper()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		err := db.Create(&database.UserHistory{
			UserID: userID, MediaType: mediaType, MediaID: int64(i + 1),
			WatchedAt: base.Add(time.Duration(i) * time.Hour),
		}).Error
		if err != nil {
			t.Fatalf("seed history %d: %v", i, err)
		}
	}
}

func TestProfileReadsPagination(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", false, "active")
	seedHistory(t, db, "target", 3, "movie")
	app := newProfileReadApp("stranger")

	// page 2, limit 1 → the second-newest row, row_count still 3.
	status, body := doGET(t, app, "/v1/users/target/history?page=2&limit=1")
	if status != http.StatusOK {
		t.Fatalf("page 2: want 200, got %d (%s)", status, body)
	}
	var page struct {
		Items    []map[string]any `json:"items"`
		Page     int              `json:"page"`
		Limit    int              `json:"limit"`
		RowCount int64            `json:"row_count"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if len(page.Items) != 1 || page.Page != 2 || page.Limit != 1 || page.RowCount != 3 {
		t.Errorf("page 2/limit 1: got items=%d page=%d limit=%d row_count=%d",
			len(page.Items), page.Page, page.Limit, page.RowCount)
	}

	// out-of-range page → 200 with an empty (never null) array.
	status, body = doGET(t, app, "/v1/users/target/history?page=99&limit=25")
	if status != http.StatusOK {
		t.Fatalf("out-of-range page: want 200, got %d (%s)", status, body)
	}
	if !strings.Contains(body, `"items":[]`) {
		t.Errorf("out-of-range page must return an empty array, got %s", body)
	}

	// limit is clamped to 100.
	_, body = doGET(t, app, "/v1/users/target/history?limit=5000")
	if !strings.Contains(body, `"limit":100`) {
		t.Errorf("limit=5000 must clamp to 100, got %s", body)
	}

	// limit=0 falls back to the default.
	_, body = doGET(t, app, "/v1/users/target/history?limit=0")
	if !strings.Contains(body, `"limit":25`) {
		t.Errorf("limit=0 must fall back to 25, got %s", body)
	}
}

func TestProfileReadsEmptyCollectionIsEmptyArray(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", false, "active")
	app := newProfileReadApp("stranger")

	for _, path := range []string{
		"/v1/users/target/collection",
		"/v1/users/target/collection/movies",
		"/v1/users/target/ratings",
		"/v1/users/target/watchlist",
		"/v1/users/target/history",
		"/v1/users/target/notes/movies",
		"/v1/users/target/likes/comments",
		"/v1/users/target/likes/lists",
	} {
		status, body := doGET(t, app, path)
		if status != http.StatusOK {
			t.Errorf("%s: want 200, got %d (%s)", path, status, body)
			continue
		}
		if strings.Contains(body, "null") {
			t.Errorf("%s: empty list must never be null: %s", path, body)
		}
	}
}

func TestProfileReadsCommentsEmptyIsArray(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", false, "active")
	app := newProfileReadApp("stranger")

	status, body := doGET(t, app, "/v1/users/target/comments/all/all")
	if status != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", status, body)
	}
	if !strings.Contains(body, `"data":[]`) {
		t.Errorf("empty comments must be [], got %s", body)
	}
}

// ---------------------------------------------------------------------------
// Media-type filters and real data
// ---------------------------------------------------------------------------

func TestProfileReadsHistoryMediaTypeFilter(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", false, "active")
	seedHistory(t, db, "target", 2, "movie")
	seedHistory(t, db, "target", 1, "episode")
	app := newProfileReadApp("stranger")

	status, body := doGET(t, app, "/v1/users/target/history/movies")
	if status != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", status, body)
	}
	var page struct {
		Items    []map[string]any `json:"items"`
		RowCount int64            `json:"row_count"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if page.RowCount != 2 || len(page.Items) != 2 {
		t.Errorf("movies filter: want 2 rows, got row_count=%d items=%d", page.RowCount, len(page.Items))
	}
	if got := page.Items[0]["media_type"]; got != "movie" {
		t.Errorf("media_type = %v, want movie", got)
	}

	// Invalid media type is a 400, not a silent empty list.
	status, body = doGET(t, app, "/v1/users/target/history/books")
	if status != http.StatusBadRequest {
		t.Errorf("invalid media type: want 400, got %d (%s)", status, body)
	}
}

func TestProfileReadsHistoryItem(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", false, "active")
	movie := database.Movie{TmdbID: 10, Title: "Heat"}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	if err := db.Create(&database.UserHistory{
		UserID: "target", MediaType: "movie", MediaID: movie.ID,
		WatchedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatalf("seed history: %v", err)
	}
	if err := db.Create(&database.ExternalID{
		MediaType: "movie", MediaID: movie.ID, Source: "tmdb", ExternalID: "10",
	}).Error; err != nil {
		t.Fatalf("seed external id: %v", err)
	}
	app := newProfileReadApp("stranger")

	status, body := doGET(t, app, "/v1/users/target/history/movies/10?id_type=tmdb")
	if status != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", status, body)
	}
	if !strings.Contains(body, `"media_id":`+itoa(movie.ID)) {
		t.Errorf("history item not returned: %s", body)
	}
}

func TestProfileReadsRatingsAndCollection(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", false, "active")
	if err := db.Create(&database.UserRating{
		UserID: "target", MediaType: "movie", MediaID: 1, Rating: 8,
		RatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatalf("seed rating: %v", err)
	}
	if err := db.Create(&database.UserCollection{
		UserID: "target", MediaType: "show", MediaID: 2,
		CollectedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatalf("seed collection: %v", err)
	}
	app := newProfileReadApp("stranger")

	status, body := doGET(t, app, "/v1/users/target/ratings/movies")
	if status != http.StatusOK || !strings.Contains(body, `"rating":8`) {
		t.Errorf("ratings: got %d (%s)", status, body)
	}
	status, body = doGET(t, app, "/v1/users/target/collection/shows")
	if status != http.StatusOK || !strings.Contains(body, `"media_type":"show"`) {
		t.Errorf("collection: got %d (%s)", status, body)
	}
}

// ---------------------------------------------------------------------------
// Watching (real scrobble-session predicate)
// ---------------------------------------------------------------------------

func TestProfileReadsWatchingUsesLiveScrobbleSession(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", false, "active")
	app := newProfileReadApp("stranger")

	// Nothing playing → empty object.
	status, body := doGET(t, app, "/v1/users/target/watching")
	if status != http.StatusOK || strings.TrimSpace(body) != "{}" {
		t.Fatalf("no session: want 200 {} got %d (%s)", status, body)
	}

	now := time.Now()
	if err := db.Create(&database.ScrobbleSession{
		UserID: "target", MediaType: "episode", MediaID: 7, Action: "start",
		Progress: 42.5, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed start session: %v", err)
	}

	status, body = doGET(t, app, "/v1/users/target/watching")
	if status != http.StatusOK || !strings.Contains(body, `"media_id":7`) {
		t.Fatalf("live session: want 200 with media_id 7, got %d (%s)", status, body)
	}
	if !strings.Contains(body, `"progress":42.5`) {
		t.Errorf("progress missing: %s", body)
	}

	// A newer pause means they are not watching.
	if err := db.Create(&database.ScrobbleSession{
		UserID: "target", MediaType: "episode", MediaID: 7, Action: "pause",
		Progress: 42.5, CreatedAt: now.Add(time.Minute), ExpiresAt: now.Add(2 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed pause session: %v", err)
	}
	status, body = doGET(t, app, "/v1/users/target/watching")
	if status != http.StatusOK || strings.TrimSpace(body) != "{}" {
		t.Errorf("paused: want 200 {}, got %d (%s)", status, body)
	}
}

// ---------------------------------------------------------------------------
// Comments, notes, likes
// ---------------------------------------------------------------------------

func TestProfileReadsCommentsFilter(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", false, "active")
	if err := db.Create(&database.Comment{
		UserID: "target", CommentType: "shout", Body: "nice",
		MediaType: "movie", MediaID: ptrInt64(1),
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	app := newProfileReadApp("stranger")

	status, body := doGET(t, app, "/v1/users/target/comments/shouts/movies")
	if status != http.StatusOK || !strings.Contains(body, `"body":"nice"`) {
		t.Errorf("shouts/movies: got %d (%s)", status, body)
	}
	// Wrong type → empty, never a fabricated row.
	status, body = doGET(t, app, "/v1/users/target/comments/reviews/movies")
	if status != http.StatusOK || !strings.Contains(body, `"data":[]`) {
		t.Errorf("reviews/movies: got %d (%s)", status, body)
	}
	// Bad comment_type → 400.
	status, _ = doGET(t, app, "/v1/users/target/comments/rants/movies")
	if status != http.StatusBadRequest {
		t.Errorf("bad comment_type: want 400, got %d", status)
	}
}

func TestProfileReadsNotesByMediaType(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", false, "active")
	if err := db.Create(&database.UserNote{
		UserID: "target", MediaType: "movie", MediaID: 3, Note: "rewatch",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatalf("seed note: %v", err)
	}
	app := newProfileReadApp("stranger")

	status, body := doGET(t, app, "/v1/users/target/notes/movies")
	if status != http.StatusOK || !strings.Contains(body, `"note":"rewatch"`) {
		t.Errorf("notes/movies: got %d (%s)", status, body)
	}
	status, body = doGET(t, app, "/v1/users/target/notes/shows")
	if status != http.StatusOK || !strings.Contains(body, `"data":[]`) {
		t.Errorf("notes/shows: got %d (%s)", status, body)
	}
}

func TestProfileReadsLikedCommentsAndLists(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "target", false, "active")
	seedUser(t, db, "listowner", false, "active")
	seedUser(t, db, "stranger", false, "active")

	comment := database.Comment{
		UserID: "listowner", CommentType: "shout", Body: "liked one",
		MediaType: "movie", MediaID: ptrInt64(9),
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := db.Create(&comment).Error; err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	if err := db.Create(&database.CommentLike{
		CommentID: comment.ID, UserID: "target",
		CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatalf("seed comment like: %v", err)
	}

	publicList := database.List{
		UserID: "listowner", Name: "Public", Slug: "public-list",
		Privacy: "public", CreatedAt: time.Now(),
	}
	privateList := database.List{
		UserID: "listowner", Name: "Private", Slug: "private-list",
		Privacy: "private", CreatedAt: time.Now(),
	}
	if err := db.Create(&publicList).Error; err != nil {
		t.Fatalf("seed public list: %v", err)
	}
	if err := db.Create(&privateList).Error; err != nil {
		t.Fatalf("seed private list: %v", err)
	}
	for _, listID := range []int64{publicList.ID, privateList.ID} {
		if err := db.Create(&database.ListLike{
			ListID: listID, UserID: "target",
			CreatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
		}).Error; err != nil {
			t.Fatalf("seed list like: %v", err)
		}
	}
	// listowner likes their own private list, so the owner-visibility branch is
	// covered against a real row.
	if err := db.Create(&database.ListLike{
		ListID: privateList.ID, UserID: "listowner",
		CreatedAt: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatalf("seed own list like: %v", err)
	}

	app := newProfileReadApp("stranger")
	status, body := doGET(t, app, "/v1/users/target/likes/comments")
	if status != http.StatusOK || !strings.Contains(body, `"body":"liked one"`) {
		t.Errorf("liked comments: got %d (%s)", status, body)
	}
	if !strings.Contains(body, `"type":"comment"`) {
		t.Errorf("liked comment item shape missing type: %s", body)
	}

	// A private list the viewer does not own is omitted, not leaked.
	status, body = doGET(t, app, "/v1/users/target/likes/lists")
	if status != http.StatusOK {
		t.Fatalf("liked lists: got %d (%s)", status, body)
	}
	if strings.Contains(body, "private-list") {
		t.Errorf("private list leaked to a stranger: %s", body)
	}
	if !strings.Contains(body, "public-list") {
		t.Errorf("public list missing: %s", body)
	}

	// The owner of the private list liking it themselves sees it.
	own := newProfileReadApp("listowner")
	status, body = doGET(t, own, "/v1/users/listowner/likes/lists")
	if status != http.StatusOK || !strings.Contains(body, "private-list") {
		t.Errorf("owner should see own private list like: %d (%s)", status, body)
	}
}

func TestMeLikesRoutes(t *testing.T) {
	db := profileTestDB(t)
	seedUser(t, db, "caller", false, "active")
	comment := database.Comment{
		UserID: "someone", CommentType: "shout", Body: "mine",
		MediaType: "movie", MediaID: ptrInt64(4), CreatedAt: time.Now(),
	}
	if err := db.Create(&comment).Error; err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	if err := db.Create(&database.CommentLike{CommentID: comment.ID, UserID: "caller", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatalf("seed like: %v", err)
	}

	// The wired route sits behind auth.AuthRequired, which parses a real token,
	// so the happy path mounts the handler behind a stub that pins user_id
	// (the same approach the export handler tests use).
	app := fiber.New()
	stub := func(c *fiber.Ctx) error {
		c.Locals("user_id", "caller")
		return c.Next()
	}
	app.Get("/v1/users/me/likes/comments", stub, handleMyLikedComments)
	app.Get("/v1/users/me/likes/lists", stub, handleMyLikedLists)

	status, body := doGET(t, app, "/v1/users/me/likes/comments")
	if status != http.StatusOK || !strings.Contains(body, `"body":"mine"`) {
		t.Errorf("/users/me/likes/comments: got %d (%s)", status, body)
	}
	status, body = doGET(t, app, "/v1/users/me/likes/lists")
	if status != http.StatusOK || !strings.Contains(body, `"items":[]`) {
		t.Errorf("/users/me/likes/lists: got %d (%s)", status, body)
	}

	// The real wiring still refuses an anonymous caller.
	status, _ = doGET(t, newProfileReadApp(""), "/v1/users/me/likes/lists")
	if status != http.StatusUnauthorized {
		t.Errorf("/users/me/likes/lists anonymous: want 401, got %d", status)
	}
}

func ptrInt64(v int64) *int64 { return &v }

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
