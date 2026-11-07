package users

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Seeded by seedExportData — values we assert must never appear in the archive.
const (
	leakedWebhookSecret = "whsec_TOPSECRET_SHOULD_NOT_LEAK"
	leakedInegrationTok = "tk_INTEGRATION_SECRET_SHOULD_NOT_LEAK"
)

func setupExportDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	models := []any{
		&database.User{},
		&database.UserHistory{}, &database.UserRating{}, &database.UserWatchlist{},
		&database.UserCollection{}, &database.UserFavorite{},
		&database.List{}, &database.ListItem{},
		&database.Comment{},
		&database.UserNote{}, &database.UserHidden{},
		&database.UserFollow{}, &database.UserBlock{},
		&database.WebhookEndpoint{}, &database.IntegrationToken{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	// user_checkins / user_playback carry `DEFAULT now()` in the model tags,
	// which sqlite rejects; create the real column shapes directly.
	for _, ddl := range []string{
		`CREATE TABLE user_playback (
			user_id text NOT NULL,
			media_type text NOT NULL,
			media_id integer NOT NULL,
			progress real NOT NULL DEFAULT 0,
			paused_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT uq_playback_user_media UNIQUE (user_id, media_type, media_id)
		)`,
		`CREATE TABLE user_checkins (
			id integer PRIMARY KEY AUTOINCREMENT,
			user_id text NOT NULL,
			media_type text NOT NULL,
			media_id integer NOT NULL,
			season_id integer,
			episode_id integer,
			message text,
			expires_at datetime NOT NULL,
			created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	database.SetDBForTest(t, db)
	return db
}

// makeExportApp wires the export route behind a stub middleware that injects
// the caller's user id (bypassing Clerk so the handler body runs).
func makeExportApp(userID string) *fiber.App {
	app := fiber.New()
	stub := func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	}
	app.Get("/v1/users/me/export", stub, handleExport)
	return app
}

// makeRealExportApp registers the routes exactly as production does so the auth
// guard (401) can be asserted against the real wiring.
func makeRealExportApp() *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1")
	RegisterRoutes(v1)
	return app
}

func getExport(t *testing.T, app *fiber.App, path string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s failed: %v", path, err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, body
}

func mustCreate(t *testing.T, db *gorm.DB, v any) {
	t.Helper()
	if err := db.Create(v).Error; err != nil {
		t.Fatalf("seed %T: %v", v, err)
	}
}

func seedExportData(t *testing.T, db *gorm.DB, uid string) {
	t.Helper()
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	seasonID, episodeID, mediaID := int64(5), int64(99), int64(205)
	note := "watching this next"
	srcID := "import-src-1"

	mustCreate(t, db, &database.User{
		ID: uid, Username: "alice", Email: "alice@example.com", Name: "Alice",
		Bio: "hi", Location: "NYC", IsPrivate: true, Status: "active", Plan: "free",
		Settings:  `{"privacy":{"profile":"private","history":"friends","ratings":"public","activity":"friends"},"notifications":{"follows":true,"replies":false,"import_complete":true,"weekly_digest":false}}`,
		CreatedAt: now, UpdatedAt: now,
	})
	mustCreate(t, db, &database.User{
		ID: "other-user", Username: "bob", Name: "Bob", Status: "active", Plan: "free",
		CreatedAt: now, UpdatedAt: now,
	})

	mustCreate(t, db, &database.UserHistory{
		UserID: uid, MediaType: "episode", MediaID: 100, SeasonID: &seasonID, EpisodeID: &episodeID,
		WatchedAt: now, Source: "api", SourceID: &srcID, CreatedAt: now,
	})
	mustCreate(t, db, &database.UserRating{UserID: uid, MediaType: "movie", MediaID: 200, Rating: 8, RatedAt: now, CreatedAt: now, UpdatedAt: now})
	mustCreate(t, db, &database.UserWatchlist{UserID: uid, MediaType: "movie", MediaID: 201, Rank: 1, Notes: &note, ListedAt: now, CreatedAt: now})
	mustCreate(t, db, &database.UserCollection{UserID: uid, MediaType: "movie", MediaID: 202, CollectedAt: now, CreatedAt: now})
	mustCreate(t, db, &database.UserFavorite{UserID: uid, MediaType: "show", MediaID: 203, Rank: 2, FavoritedAt: now, CreatedAt: now})

	list := database.List{UserID: uid, Name: "My List", Slug: "my-list-" + uid, Privacy: "public", CreatedAt: now, UpdatedAt: now}
	mustCreate(t, db, &list)
	mustCreate(t, db, &database.ListItem{ListID: list.ID, MediaType: "movie", MediaID: 204, Rank: 1, Notes: "item note", AddedBy: uid, AddedAt: now})

	mustCreate(t, db, &database.Comment{UserID: uid, MediaType: "movie", MediaID: &mediaID, CommentType: "shout", Body: "hello world", CreatedAt: now, UpdatedAt: now})
	mustCreate(t, db, &database.UserNote{UserID: uid, MediaType: "movie", MediaID: 206, Note: "a private note", CreatedAt: now, UpdatedAt: now})
	mustCreate(t, db, &database.UserHidden{UserID: uid, Section: "progress", MediaType: "movie", MediaID: 207, HiddenAt: now})

	mustCreate(t, db, &database.UserFollow{FollowerID: uid, FollowingID: "other-user", CreatedAt: now})
	mustCreate(t, db, &database.UserFollow{FollowerID: "other-user", FollowingID: uid, CreatedAt: now})
	mustCreate(t, db, &database.UserBlock{BlockerID: uid, BlockedID: "other-user", CreatedAt: now})

	mustCreate(t, db, &database.UserCheckin{UserID: uid, MediaType: "episode", MediaID: 100, EpisodeID: &episodeID, Message: "live now", ExpiresAt: now.Add(5 * time.Hour), CreatedAt: now})
	mustCreate(t, db, &database.UserPlayback{UserID: uid, MediaType: "movie", MediaID: 208, Progress: 0.42, PausedAt: now})

	mustCreate(t, db, &database.WebhookEndpoint{UserID: uid, URL: "https://example.com/hook", Events: database.StringSlice{"scrobble"}, SecretHash: leakedWebhookSecret, Active: true, CreatedAt: now, UpdatedAt: now})
	mustCreate(t, db, &database.IntegrationToken{UserID: uid, Token: leakedInegrationTok, Name: "plex", Scopes: database.StringSlice{"scrobble"}, Active: true, CreatedAt: now})

	// A second user's rows prove owner-scoping: none of these may appear.
	mustCreate(t, db, &database.UserHistory{UserID: "other-user", MediaType: "movie", MediaID: 999, WatchedAt: now, Source: "api", CreatedAt: now})
	mustCreate(t, db, &database.UserNote{UserID: "other-user", MediaType: "movie", MediaID: 998, Note: "someone else's note", CreatedAt: now, UpdatedAt: now})
}

// archive is the decoded shape used by assertions. It mirrors the documented
// export format.
type archive struct {
	Version    int    `json:"version"`
	ExportedAt string `json:"exported_at"`
	User       struct {
		ID       string `json:"id"`
		Email    string `json:"email"`
		Username string `json:"username"`
	} `json:"user"`
	Settings map[string]any `json:"settings"`
	History  []struct {
		MediaID int64  `json:"media_id"`
		Source  string `json:"source"`
	} `json:"history"`
	Ratings []struct {
		Rating int `json:"rating"`
	} `json:"ratings"`
	Watchlist []struct {
		MediaID int64 `json:"media_id"`
	} `json:"watchlist"`
	Collection []struct {
		MediaID int64 `json:"media_id"`
	} `json:"collection"`
	Favorites []struct {
		MediaID int64 `json:"media_id"`
	} `json:"favorites"`
	Lists []struct {
		Slug  string `json:"slug"`
		Items []struct {
			MediaID int64 `json:"media_id"`
		} `json:"items"`
	} `json:"lists"`
	Comments []struct {
		Body string `json:"body"`
	} `json:"comments"`
	Notes []struct {
		Note string `json:"note"`
	} `json:"notes"`
	Hidden []struct {
		MediaID int64 `json:"media_id"`
	} `json:"hidden"`
	Follows struct {
		Following []struct {
			UserID string `json:"user_id"`
		} `json:"following"`
		Followers []struct {
			UserID string `json:"user_id"`
		} `json:"followers"`
	} `json:"follows"`
	Blocks []struct {
		BlockedID string `json:"blocked_id"`
	} `json:"blocks"`
	Checkins []struct {
		Message string `json:"message"`
	} `json:"checkins"`
	Playback []struct {
		MediaID  int64   `json:"media_id"`
		Progress float64 `json:"progress"`
	} `json:"playback"`
	Webhooks []struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
	} `json:"webhook_endpoints"`
}

func decodeArchive(t *testing.T, body []byte) archive {
	t.Helper()
	var a archive
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatalf("archive is not valid JSON: %v\nbody: %s", err, truncate(body))
	}
	return a
}

func truncate(b []byte) string {
	if len(b) > 600 {
		return string(b[:600]) + "…"
	}
	return string(b)
}

func TestExportArchiveContainsEverySeededSection(t *testing.T) {
	db := setupExportDB(t)
	seedExportData(t, db, "user-1")
	app := makeExportApp("user-1")

	resp, body := getExport(t, app, "/v1/users/me/export")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, truncate(body))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	a := decodeArchive(t, body)

	if a.User.ID != "user-1" || a.User.Email != "alice@example.com" {
		t.Errorf("user block wrong: %+v", a.User)
	}
	if len(a.Settings) == 0 {
		t.Error("settings block is empty")
	}
	if len(a.History) != 1 || a.History[0].MediaID != 100 || a.History[0].Source != "api" {
		t.Errorf("history = %+v, want one row media_id 100", a.History)
	}
	if len(a.Ratings) != 1 || a.Ratings[0].Rating != 8 {
		t.Errorf("ratings = %+v, want one rating of 8", a.Ratings)
	}
	if len(a.Watchlist) != 1 || a.Watchlist[0].MediaID != 201 {
		t.Errorf("watchlist = %+v", a.Watchlist)
	}
	if len(a.Collection) != 1 || a.Collection[0].MediaID != 202 {
		t.Errorf("collection = %+v", a.Collection)
	}
	if len(a.Favorites) != 1 || a.Favorites[0].MediaID != 203 {
		t.Errorf("favorites = %+v", a.Favorites)
	}
	if len(a.Lists) != 1 || a.Lists[0].Slug != "my-list-user-1" {
		t.Fatalf("lists = %+v, want one list", a.Lists)
	}
	if len(a.Lists[0].Items) != 1 || a.Lists[0].Items[0].MediaID != 204 {
		t.Errorf("list items = %+v, want one item 204", a.Lists[0].Items)
	}
	if len(a.Comments) != 1 || a.Comments[0].Body != "hello world" {
		t.Errorf("comments = %+v", a.Comments)
	}
	if len(a.Notes) != 1 || a.Notes[0].Note != "a private note" {
		t.Errorf("notes = %+v", a.Notes)
	}
	if len(a.Hidden) != 1 || a.Hidden[0].MediaID != 207 {
		t.Errorf("hidden = %+v", a.Hidden)
	}
	if len(a.Follows.Following) != 1 || a.Follows.Following[0].UserID != "other-user" {
		t.Errorf("following = %+v", a.Follows.Following)
	}
	if len(a.Follows.Followers) != 1 || a.Follows.Followers[0].UserID != "other-user" {
		t.Errorf("followers = %+v", a.Follows.Followers)
	}
	if len(a.Blocks) != 1 || a.Blocks[0].BlockedID != "other-user" {
		t.Errorf("blocks = %+v", a.Blocks)
	}
	if len(a.Checkins) != 1 || a.Checkins[0].Message != "live now" {
		t.Errorf("checkins = %+v", a.Checkins)
	}
	if len(a.Playback) != 1 || a.Playback[0].MediaID != 208 || a.Playback[0].Progress != 0.42 {
		t.Errorf("playback = %+v", a.Playback)
	}
	if len(a.Webhooks) != 1 || a.Webhooks[0].URL != "https://example.com/hook" || len(a.Webhooks[0].Events) != 1 {
		t.Errorf("webhook_endpoints = %+v", a.Webhooks)
	}

	// Owner-scoping: other-user's history/note must not leak in.
	if strings.Contains(string(body), "someone else's note") {
		t.Error("another user's note leaked into the archive")
	}
}

func TestExportIsVersioned(t *testing.T) {
	db := setupExportDB(t)
	seedExportData(t, db, "user-1")
	app := makeExportApp("user-1")

	_, body := getExport(t, app, "/v1/users/me/export")
	a := decodeArchive(t, body)

	if a.Version != ExportFormatVersion {
		t.Errorf("version = %d, want %d", a.Version, ExportFormatVersion)
	}
	if a.Version == 0 {
		t.Error("version field missing from archive")
	}
	if a.ExportedAt == "" {
		t.Error("exported_at missing from archive")
	}
}

// TestExportNeverLeaksSecrets asserts on the serialised bytes, not on a struct
// field: the webhook signing secret and an integration token are seeded with
// recognisable values and must be absent from the wire body.
func TestExportNeverLeaksSecrets(t *testing.T) {
	db := setupExportDB(t)
	seedExportData(t, db, "user-1")
	app := makeExportApp("user-1")

	_, body := getExport(t, app, "/v1/users/me/export")
	text := string(body)

	for _, secret := range []string{leakedWebhookSecret, leakedInegrationTok} {
		if strings.Contains(text, secret) {
			t.Errorf("archive leaked secret %q", secret)
		}
	}
	// The secret-bearing columns/keys must not appear at all.
	for _, forbidden := range []string{"secret_hash", "secretHash", "token_hash", "refresh_token"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("archive contains forbidden key %q", forbidden)
		}
	}
}

// TestExportEmptyUserHasNoNulls proves empty sections serialise as [] / {} and
// never null, and that the response stays valid JSON for a brand-new user.
func TestExportEmptyUserHasNoNulls(t *testing.T) {
	db := setupExportDB(t)
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	mustCreate(t, db, &database.User{ID: "fresh", Username: "fresh", Name: "Fresh", Status: "active", Plan: "free", CreatedAt: now, UpdatedAt: now})
	app := makeExportApp("fresh")

	resp, body := getExport(t, app, "/v1/users/me/export")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !json.Valid(body) {
		t.Fatalf("archive is not valid JSON: %s", truncate(body))
	}

	text := string(body)
	for _, key := range []string{"history", "ratings", "watchlist", "collection", "favorites", "comments", "notes", "hidden", "blocks", "checkins", "playback", "webhook_endpoints", "lists"} {
		if !strings.Contains(text, `"`+key+`":[]`) {
			t.Errorf("empty section %q did not serialise as []: %s", key, truncate(body))
		}
	}
	for _, key := range []string{"following", "followers"} {
		if !strings.Contains(text, `"`+key+`":[]`) {
			t.Errorf("empty follows.%s did not serialise as []: %s", key, truncate(body))
		}
	}
	if strings.Contains(text, "null") {
		t.Errorf("archive contains null: %s", truncate(body))
	}
}

func TestExportRequiresAuth(t *testing.T) {
	app := makeRealExportApp()

	resp, body := getExport(t, app, "/v1/users/me/export")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("401 body is not the error envelope: %v (%s)", err, truncate(body))
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("error code = %q, want unauthorized", env.Error.Code)
	}
}

func TestExportDatabaseUnavailable(t *testing.T) {
	// No setupExportDB here: the DB singleton is nil, so GetDB() fails.
	app := makeExportApp("user-1")

	resp, body := getExport(t, app, "/v1/users/me/export")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body: %s", resp.StatusCode, truncate(body))
	}
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("503 body is not the error envelope: %v (%s)", err, truncate(body))
	}
	if env.Error.Code != "db_unavailable" {
		t.Errorf("error code = %q, want db_unavailable", env.Error.Code)
	}
}

// TestExportStreamsAcrossBatches exercises the cursor path with more rows than
// one would ever buffer by hand, proving rows are emitted individually.
func TestExportStreamsAcrossBatches(t *testing.T) {
	db := setupExportDB(t)
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	mustCreate(t, db, &database.User{ID: "heavy", Username: "heavy", Name: "Heavy", Status: "active", Plan: "free", CreatedAt: now, UpdatedAt: now})

	const rows = 2500
	batch := make([]database.UserHistory, 0, 500)
	for i := 0; i < rows; i++ {
		batch = append(batch, database.UserHistory{UserID: "heavy", MediaType: "movie", MediaID: int64(i + 1), WatchedAt: now, Source: "api", CreatedAt: now})
		if len(batch) == 500 {
			if err := db.Create(&batch).Error; err != nil {
				t.Fatalf("seed history batch: %v", err)
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := db.Create(&batch).Error; err != nil {
			t.Fatalf("seed history batch: %v", err)
		}
	}

	app := makeExportApp("heavy")
	_, body := getExport(t, app, "/v1/users/me/export")
	a := decodeArchive(t, body)
	if len(a.History) != rows {
		t.Errorf("history length = %d, want %d", len(a.History), rows)
	}
}
