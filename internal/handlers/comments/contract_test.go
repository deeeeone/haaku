package comments

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newCommentTestDB builds the comment schema on an in-memory sqlite database
// and points database.GetDB at it (handler suites run on sqlite in CI).
func newCommentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(database.CommentModels()...); err != nil {
		t.Fatalf("migrate comment models: %v", err)
	}
	if err := db.AutoMigrate(&database.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// asUser mounts a route that pins the caller's identity and then runs h, so a
// handler that reads c.Locals("user_id") can be exercised without a token.
func asUser(app *fiber.App, method, path, userID string, h fiber.Handler) {
	app.Add(method, path, func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h(c)
	})
}

// newHandlerTestApp installs the server's envelope error handler so a handler
// that writes its own 404 and returns response.ErrHandled stops cleanly.
func newHandlerTestApp() *fiber.App {
	return fiber.New(fiber.Config{ErrorHandler: response.HandleError})
}

func doJSON(t *testing.T, app *fiber.App, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Host = "example.com"
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var raw json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&raw)
	return resp.StatusCode, string(raw)
}

func TestParseCommentItemAcceptsContractLegacyAndWebShapes(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantType string
		wantID   int64
		wantList bool
	}{
		{"contract movie", `{"movie":{"ids":{"trakt":12}}}`, "movie", 12, false},
		{"contract episode", `{"episode":{"ids":{"trakt":34}}}`, "episode", 34, false},
		{"legacy flat", `{"show":56}`, "show", 56, false},
		{"numeric string", `{"season":"78"}`, "season", 78, false},
		{"web shape", `{"type":"movie","id":90}`, "movie", 90, false},
		{"list", `{"list":{"ids":{"trakt":11}}}`, "", 11, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typ, mediaID, listID, err := parseCommentItem(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatalf("parseCommentItem(%s): %v", tc.raw, err)
			}
			if tc.wantList {
				if listID == nil || *listID != tc.wantID || mediaID != nil {
					t.Fatalf("list target = %v/%v, want list %d", mediaID, listID, tc.wantID)
				}
				return
			}
			if typ != tc.wantType || mediaID == nil || *mediaID != tc.wantID {
				t.Fatalf("target = %q/%v, want %q/%d", typ, mediaID, tc.wantType, tc.wantID)
			}
		})
	}
}

func TestParseCommentItemRejectsEmptyObject(t *testing.T) {
	if _, _, _, err := parseCommentItem(json.RawMessage(`{}`)); err == nil {
		t.Fatal("parseCommentItem({}) = nil error, want rejection")
	}
}

func TestParseCommentGifTriState(t *testing.T) {
	url, slug, present, err := parseCommentGif(json.RawMessage(`{"url":"https://klipy/x.gif","slug":"x"}`))
	if err != nil || !present || url == nil || *url != "https://klipy/x.gif" || slug == nil || *slug != "x" {
		t.Fatalf("object gif = %v %v %v %v", url, slug, present, err)
	}
	if _, _, present, err := parseCommentGif(json.RawMessage(`null`)); err != nil || !present {
		t.Fatalf("null gif = present %v err %v, want present true", present, err)
	}
	if _, _, present, err := parseCommentGif(nil); err != nil || present {
		t.Fatalf("absent gif = present %v err %v, want present false", present, err)
	}
	if _, _, _, err := parseCommentGif(json.RawMessage(`{}`)); err == nil {
		t.Fatal("gif without url should be rejected")
	}
}

func TestReactionSummaryCountsStoredRows(t *testing.T) {
	db := newCommentTestDB(t)
	comment := database.Comment{UserID: "author", Body: "hi", CommentType: "shout", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&comment).Error; err != nil {
		t.Fatalf("create comment: %v", err)
	}
	rows := []database.CommentReaction{
		{CommentID: comment.ID, UserID: "u1", ReactionType: "like", CreatedAt: time.Now()},
		{CommentID: comment.ID, UserID: "u2", ReactionType: "like", CreatedAt: time.Now()},
		{CommentID: comment.ID, UserID: "u1", ReactionType: "love", CreatedAt: time.Now()},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed reactions: %v", err)
	}

	app := newHandlerTestApp()
	app.Get("/v1/comments/:id/reactions/summary", handleReactionSummary)

	status, body := doJSON(t, app, "GET", "/v1/comments/"+itoa(comment.ID)+"/reactions/summary", "")
	if status != 200 {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}
	var got struct {
		ReactionCount int64            `json:"reaction_count"`
		UserCount     int64            `json:"user_count"`
		Distribution  map[string]int64 `json:"distribution"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ReactionCount != 3 || got.UserCount != 2 {
		t.Fatalf("summary = %+v, want reaction_count 3 user_count 2", got)
	}
	if got.Distribution["like"] != 2 || got.Distribution["love"] != 1 {
		t.Fatalf("distribution = %v, want like:2 love:1", got.Distribution)
	}
}

func TestReactionAddAndRemoveWritesRows(t *testing.T) {
	db := newCommentTestDB(t)
	comment := database.Comment{UserID: "author", Body: "hi", CommentType: "shout", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&comment).Error; err != nil {
		t.Fatalf("create comment: %v", err)
	}

	app := newHandlerTestApp()
	asUser(app, "POST", "/v1/comments/:id/reactions/:reaction_type", "u1", handleAddContractReaction)
	asUser(app, "DELETE", "/v1/comments/:id/reactions/:reaction_type", "u1", handleRemoveContractReaction)

	path := "/v1/comments/" + itoa(comment.ID) + "/reactions/bravo"
	if status, body := doJSON(t, app, "POST", path, ""); status != 204 {
		t.Fatalf("add reaction status = %d, want 204 (%s)", status, body)
	}
	var n int64
	db.Model(&database.CommentReaction{}).Where("comment_id = ? AND reaction_type = ?", comment.ID, "bravo").Count(&n)
	if n != 1 {
		t.Fatalf("stored reactions = %d, want 1", n)
	}
	// Idempotent: a second add must not create a second row.
	if status, _ := doJSON(t, app, "POST", path, ""); status != 204 {
		t.Fatalf("second add status = %d, want 204", status)
	}
	db.Model(&database.CommentReaction{}).Where("comment_id = ? AND reaction_type = ?", comment.ID, "bravo").Count(&n)
	if n != 1 {
		t.Fatalf("stored reactions after re-add = %d, want 1", n)
	}

	if status, body := doJSON(t, app, "DELETE", path, ""); status != 204 {
		t.Fatalf("remove reaction status = %d, want 204 (%s)", status, body)
	}
	db.Model(&database.CommentReaction{}).Where("comment_id = ?", comment.ID).Count(&n)
	if n != 0 {
		t.Fatalf("stored reactions after remove = %d, want 0", n)
	}
}

func TestReactionRejectsUnknownTypeAndMissingComment(t *testing.T) {
	db := newCommentTestDB(t)
	comment := database.Comment{UserID: "author", Body: "hi", CommentType: "shout", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&comment).Error; err != nil {
		t.Fatalf("create comment: %v", err)
	}
	app := newHandlerTestApp()
	asUser(app, "POST", "/v1/comments/:id/reactions/:reaction_type", "u1", handleAddContractReaction)

	if status, _ := doJSON(t, app, "POST", "/v1/comments/"+itoa(comment.ID)+"/reactions/nope", ""); status != 400 {
		t.Fatalf("unknown reaction type status = %d, want 400", status)
	}
	if status, _ := doJSON(t, app, "POST", "/v1/comments/99999/reactions/like", ""); status != 404 {
		t.Fatalf("missing comment status = %d, want 404", status)
	}
}

func TestGetReactionsListsStoredRowsWithUsers(t *testing.T) {
	db := newCommentTestDB(t)
	comment := database.Comment{UserID: "author", Body: "hi", CommentType: "shout", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&comment).Error; err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if err := db.Create(&database.User{ID: "u1", Username: "one"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	row := database.CommentReaction{CommentID: comment.ID, UserID: "u1", ReactionType: "shocked", CreatedAt: time.Now()}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed reaction: %v", err)
	}

	app := newHandlerTestApp()
	app.Get("/v1/comments/:id/reactions", handleGetReactions)

	status, body := doJSON(t, app, "GET", "/v1/comments/"+itoa(comment.ID)+"/reactions", "")
	if status != 200 {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}
	var got []struct {
		ReactedAt string `json:"reacted_at"`
		Reaction  struct {
			Type string `json:"type"`
		} `json:"reaction"`
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Reaction.Type != "shocked" || got[0].User.Username != "one" || got[0].ReactedAt == "" {
		t.Fatalf("reaction list = %+v, want one shocked reaction by one", got)
	}
}

func TestEditAndDeleteCommentRequireAuthorOrModerator(t *testing.T) {
	db := newCommentTestDB(t)
	if err := db.Create(&database.User{ID: "stranger", Username: "stranger"}).Error; err != nil {
		t.Fatalf("create stranger: %v", err)
	}
	if err := db.Create(&database.User{ID: "mod", Username: "mod", IsModerator: true}).Error; err != nil {
		t.Fatalf("create moderator: %v", err)
	}
	comment := database.Comment{UserID: "author", Body: "hi", CommentType: "shout", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&comment).Error; err != nil {
		t.Fatalf("create comment: %v", err)
	}

	app := newHandlerTestApp()
	asUser(app, "PUT", "/stranger/comments/:id", "stranger", handleEditComment)
	asUser(app, "PUT", "/mod/comments/:id", "mod", handleEditComment)
	app.Delete("/stranger/:id", func(c *fiber.Ctx) error {
		c.Locals("user_id", "stranger")
		return handleDeleteComment(c)
	})
	app.Delete("/mod/:id", func(c *fiber.Ctx) error {
		c.Locals("user_id", "mod")
		return handleDeleteComment(c)
	})

	body := `{"comment":"edited","spoiler":true}`
	if status, resp := doJSON(t, app, "PUT", "/stranger/comments/"+itoa(comment.ID), body); status != 403 {
		t.Fatalf("stranger edit status = %d, want 403 (%s)", status, resp)
	}
	if status, resp := doJSON(t, app, "PUT", "/mod/comments/"+itoa(comment.ID), body); status != 200 {
		t.Fatalf("moderator edit status = %d, want 200 (%s)", status, resp)
	}

	// A second comment authored by the same (non-caller) user for the delete
	// cases.
	strangerComment := database.Comment{UserID: "author", Body: "bye", CommentType: "shout", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&strangerComment).Error; err != nil {
		t.Fatalf("create second comment: %v", err)
	}
	if status, resp := doJSON(t, app, "DELETE", "/stranger/"+itoa(strangerComment.ID), ""); status != 403 {
		t.Fatalf("stranger delete status = %d, want 403 (%s)", status, resp)
	}
	if status, resp := doJSON(t, app, "DELETE", "/mod/"+itoa(strangerComment.ID), ""); status != 204 {
		t.Fatalf("moderator delete status = %d, want 204 (%s)", status, resp)
	}
}

func TestDeleteMissingCommentAnswers404(t *testing.T) {
	newCommentTestDB(t)
	app := newHandlerTestApp()
	app.Delete("/:id", func(c *fiber.Ctx) error {
		c.Locals("user_id", "u1")
		return handleDeleteComment(c)
	})
	if status, _ := doJSON(t, app, "DELETE", "/99999", ""); status != 404 {
		t.Fatalf("delete missing status = %d, want 404", status)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
