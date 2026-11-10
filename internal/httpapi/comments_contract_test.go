package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Comment writes and feeds are proved through RegisterRoutes, not a hand-mounted
// handler: the earlier "implemented" grade for POST /comments/ rested on a test
// that called the parser directly, while the registered route answered 400 to
// the contract's own body because the decoder never saw an "item" key. A handler
// test that mounts its own app is not evidence that the route works.
func newCommentsContractApp(t *testing.T) (*fiber.App, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:comments_contract_%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.AutoMigrate(&database.User{}); err != nil {
		t.Fatalf("migrate user: %v", err)
	}
	if err := db.AutoMigrate(database.CommentModels()...); err != nil {
		t.Fatalf("migrate comments: %v", err)
	}
	if err := db.AutoMigrate(&database.UserHistory{}); err != nil {
		t.Fatalf("migrate history: %v", err)
	}
	database.SetDBForTest(t, db)

	now := time.Now().UTC()
	editedAt := now.Add(-time.Hour)
	rows := []any{
		&database.User{ID: "user_test", Username: "tester", Email: "t@example.com", CreatedAt: now},
		&database.User{ID: "user_other", Username: "other", Email: "o@example.com", CreatedAt: now},
		&database.Comment{ID: 1, UserID: "user_test", MediaType: "movie", MediaID: ptrInt64(1),
			CommentType: "shout", Body: "a comment", CreatedAt: now, UpdatedAt: now},
		&database.Comment{ID: 2, UserID: "user_other", MediaType: "movie", MediaID: ptrInt64(2),
			CommentType: "shout", Body: "an edited comment", EditedAt: &editedAt,
			CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: editedAt},
		&database.CommentReaction{CommentID: 1, UserID: "user_other", ReactionType: "like", CreatedAt: now},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}

	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", "user_test")
		return c.Next()
	})
	RegisterRoutes(app)
	return app, db
}

func commentRequest(t *testing.T, app *fiber.App, method, path, body string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: request: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	return res.StatusCode, raw
}

// decodeComment decodes a contract comment object and checks the fields the
// contract requires as integers, which is what the gate failed on: parent_id is
// a required integer, not nullable.
func decodeComment(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("comment is not a JSON object: %s (%v)", string(raw), err)
	}
	for _, field := range []string{"id", "parent_id", "replies", "likes"} {
		if _, ok := obj[field].(float64); !ok {
			t.Errorf("comment.%s = %#v, want a JSON integer (%s)", field, obj[field], string(raw))
		}
	}
	if _, ok := obj["comment"].(string); !ok {
		t.Errorf("comment.comment missing/not a string: %s", string(raw))
	}
	if _, ok := obj["spoiler"].(bool); !ok {
		t.Errorf("comment.spoiler missing/not a bool: %s", string(raw))
	}
	if _, ok := obj["review"].(bool); !ok {
		t.Errorf("comment.review missing/not a bool: %s", string(raw))
	}
	stats, ok := obj["user_stats"].(map[string]any)
	if !ok {
		t.Fatalf("comment.user_stats is not an object: %s", string(raw))
	}
	for _, field := range []string{"play_count", "completed_count"} {
		if _, ok := stats[field].(float64); !ok {
			t.Errorf("comment.user_stats.%s = %#v, want an integer", field, stats[field])
		}
	}
	if _, ok := obj["user"].(map[string]any); !ok {
		t.Errorf("comment.user is not an object: %s", string(raw))
	}
	return obj
}

// TestCreateCommentAcceptsTheContractBody is the regression for the 400: the
// contract's POST body carries the target at the top level
// (`movie: {ids: {trakt}}`), not nested in an `item` key.
func TestCreateCommentAcceptsTheContractBody(t *testing.T) {
	app, db := newCommentsContractApp(t)

	status, raw := commentRequest(t, app, http.MethodPost, "/v1/comments/",
		`{"comment":"from the contract","spoiler":false,"gif":null,"movie":{"ids":{"trakt":1}}}`)
	if status != http.StatusCreated {
		t.Fatalf("POST /v1/comments/ = %d, want 201: %s", status, string(raw))
	}
	obj := decodeComment(t, raw)
	if obj["comment"] != "from the contract" {
		t.Errorf("comment = %#v, want the posted body", obj["comment"])
	}
	if parent, ok := obj["parent_id"].(float64); !ok || parent != 0 {
		t.Errorf("parent_id = %#v, want 0 for a top-level comment", obj["parent_id"])
	}
	if gif, ok := obj["gif"]; !ok || gif != nil {
		t.Errorf("gif = %#v, want an explicit null", gif)
	}

	// The item the contract named is the one stored.
	var stored database.Comment
	if err := db.Where("body = ?", "from the contract").First(&stored).Error; err != nil {
		t.Fatalf("load stored comment: %v", err)
	}
	if stored.MediaType != "movie" || stored.MediaID == nil || *stored.MediaID != 1 {
		t.Errorf("stored target = %s/%v, want movie/1", stored.MediaType, stored.MediaID)
	}
}

// TestCreateCommentKeepsLegacyAndWebItemShapes pins the two older spellings the
// in-repo web client still sends.
func TestCreateCommentKeepsLegacyAndWebItemShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"legacy nested item", `{"item":{"show":2},"comment":"legacy","spoiler":true}`},
		{"web typed item", `{"item":{"type":"episode","id":3},"comment":"web","spoiler":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, _ := newCommentsContractApp(t)
			status, raw := commentRequest(t, app, http.MethodPost, "/v1/comments/", tc.body)
			if status != http.StatusCreated {
				t.Fatalf("POST /v1/comments/ = %d, want 201: %s", status, string(raw))
			}
			decodeComment(t, raw)
		})
	}
}

func TestCreateCommentRejectsABodyWithNoItem(t *testing.T) {
	app, _ := newCommentsContractApp(t)
	status, raw := commentRequest(t, app, http.MethodPost, "/v1/comments/",
		`{"comment":"no target","spoiler":false}`)
	if status != http.StatusBadRequest {
		t.Fatalf("POST /v1/comments/ without an item = %d, want 400: %s", status, string(raw))
	}
}

// TestEditCommentParentIDIsAnInteger covers the PUT row the gate failed on.
func TestEditCommentParentIDIsAnInteger(t *testing.T) {
	app, _ := newCommentsContractApp(t)
	status, raw := commentRequest(t, app, http.MethodPut, "/v1/comments/1/",
		`{"comment":"edited through the contract path","spoiler":false}`)
	if status != http.StatusOK {
		t.Fatalf("PUT /v1/comments/1/ = %d, want 200: %s", status, string(raw))
	}
	obj := decodeComment(t, raw)
	if obj["comment"] != "edited through the contract path" {
		t.Errorf("comment = %#v, want the edited body", obj["comment"])
	}
}

// TestCommentFeedsReturnIntegerParentIDs covers the recent feed and the updates
// feed, which was empty because no seeded comment had an edit.
func TestCommentFeedsReturnIntegerParentIDs(t *testing.T) {
	app, _ := newCommentsContractApp(t)

	for _, path := range []string{
		"/v1/comments/recent/shouts/movies",
		"/v1/comments/updates/shouts/movies",
	} {
		status, raw := commentRequest(t, app, http.MethodGet, path, "")
		if status != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200: %s", path, status, string(raw))
		}
		var arr []map[string]any
		if err := json.Unmarshal(raw, &arr); err != nil {
			t.Fatalf("GET %s is not a JSON array: %s", path, string(raw))
		}
		if len(arr) == 0 {
			t.Fatalf("GET %s returned an empty array — the assertion would validate nothing", path)
		}
		for _, item := range arr {
			if _, ok := item["parent_id"].(float64); !ok {
				t.Errorf("GET %s parent_id = %#v, want a JSON integer", path, item["parent_id"])
			}
		}
	}
}

// TestReactionsListRendersAStoredRow covers the reaction read: an empty array
// was a fixture gap, so the test seeds the row the case asks for.
func TestReactionsListRendersAStoredRow(t *testing.T) {
	app, _ := newCommentsContractApp(t)

	status, raw := commentRequest(t, app, http.MethodGet, "/v1/comments/1/reactions/", "")
	if status != http.StatusOK {
		t.Fatalf("GET /v1/comments/1/reactions/ = %d, want 200: %s", status, string(raw))
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("reactions is not a JSON array: %s", string(raw))
	}
	if len(arr) != 1 {
		t.Fatalf("reactions = %s, want the one seeded row", string(raw))
	}
	reaction, _ := arr[0]["reaction"].(map[string]any)
	if reaction["type"] != "like" {
		t.Errorf("reaction.type = %#v, want like", arr[0]["reaction"])
	}
	if _, ok := arr[0]["user"].(map[string]any); !ok {
		t.Errorf("reaction.user is not an object: %s", string(raw))
	}
}

// TestRemoveReactionSucceedsOnALiveComment pins the fixture ordering defect: a
// reaction DELETE on an existing comment is a 204 even when the reaction row is
// not there to remove; only a missing comment is a 404.
func TestRemoveReactionSucceedsOnALiveComment(t *testing.T) {
	app, _ := newCommentsContractApp(t)
	status, raw := commentRequest(t, app, http.MethodDelete, "/v1/comments/1/reactions/like/", "")
	if status != http.StatusNoContent {
		t.Fatalf("DELETE /v1/comments/1/reactions/like/ = %d, want 204: %s", status, string(raw))
	}
}
