package comments

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
)

// newCommentsApp registers a POST /validated route that runs validateCommentBody
// against the request body, so the write-to-context error envelope path is
// exercised through a real Fiber context.
func newCommentsApp() *fiber.App {
	app := fiber.New()
	app.Post("/validated", func(c *fiber.Ctx) error {
		return validateCommentBody(c, string(c.Body()))
	})
	return app
}

func postReq(body string) *http.Request {
	req, _ := http.NewRequest("POST", "/validated", strings.NewReader(body))
	req.Host = "example.com"
	return req
}

func TestValidateCommentBodyRejectsEmpty(t *testing.T) {
	app := newCommentsApp()
	resp, err := app.Test(postReq(""))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (empty body)", resp.StatusCode)
	}
}

func TestValidateCommentBodyRejectsTooLong(t *testing.T) {
	app := newCommentsApp()
	// Exactly one char over the cap (5000).
	resp, err := app.Test(postReq(strings.Repeat("a", maxCommentLength+1)))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body exceeds max length)", resp.StatusCode)
	}
}

func TestValidateCommentBodyAcceptsAtLimit(t *testing.T) {
	app := newCommentsApp()
	// Exactly the cap boundary is valid.
	resp, err := app.Test(postReq(strings.Repeat("a", maxCommentLength)))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body at exact max length)", resp.StatusCode)
	}
}

func TestMaxCommentLengthConstant(t *testing.T) {
	if maxCommentLength != 5000 {
		t.Fatalf("maxCommentLength = %d, want 5000 (Trakt parity)", maxCommentLength)
	}
}

func TestAllowedReactions(t *testing.T) {
	allowed := []string{"\U0001f44d", "\U0001f525", "\U0001f602", "\U0001f62e", "\U0001f622", "\U0001f620"}
	for _, r := range allowed {
		if !allowedReactions[r] {
			t.Errorf("allowedReactions missing expected reaction %q", r)
		}
	}
	for _, r := range []string{"", "like", "love", "\U0001f600"} {
		if allowedReactions[r] {
			t.Errorf("allowedReactions unexpectedly contains %q", r)
		}
	}
}

func TestAllowedReportReasons(t *testing.T) {
	allowed := []string{"spoilers", "language", "abusive", "spam", "bigotry",
		"political", "offtopic", "duplicate", "too_short", "other"}
	for _, r := range allowed {
		if !allowedReportReasons[r] {
			t.Errorf("allowedReportReasons missing expected reason %q", r)
		}
	}
	for _, r := range []string{"", "violence", "SPAM", "spam "} {
		if allowedReportReasons[r] {
			t.Errorf("allowedReportReasons unexpectedly contains %q", r)
		}
	}
}

func TestParseCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{"a, b ,c", []string{"a", "b", "c"}}, // trims whitespace
		{"a,,b", []string{"a", "b"}},         // skips empties
		{"", []string{""}},                   // empty input returns [""] per impl
		{"single", []string{"single"}},
	}
	for _, tc := range cases {
		got := parseCSV(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("parseCSV(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parseCSV(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

func TestToCommentResponse(t *testing.T) {
	mid := int64(42)
	edited := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	c := &database.Comment{
		ID:          7,
		UserID:      "user-1",
		ParentID:    nil,
		MediaType:   "movie",
		MediaID:     &mid,
		CommentType: "shout",
		Body:        "hello",
		Spoiler:     true,
		LikeCount:   3,
		ReplyCount:  1,
		EditedAt:    &edited,
		CreatedAt:   created,
		UpdatedAt:   created,
	}

	got := toCommentResponse(c)
	if got.ID != 7 || got.UserID != "user-1" || got.MediaType != "movie" || got.MediaID == nil || *got.MediaID != 42 {
		t.Errorf("toCommentResponse base fields wrong: %+v", got)
	}
	if got.CommentType != "shout" || got.Body != "hello" || !got.Spoiler {
		t.Errorf("toCommentResponse content fields wrong: %+v", got)
	}
	if got.LikeCount != 3 || got.ReplyCount != 1 {
		t.Errorf("toCommentResponse counts wrong: %+v", got)
	}
	if got.EditedAt == nil || *got.EditedAt != "2026-01-02T03:04:05.000Z" {
		t.Errorf("toCommentResponse EditedAt = %v, want formatted UTC", got.EditedAt)
	}
	if got.CreatedAt != "2026-01-01T00:00:00.000Z" {
		t.Errorf("toCommentResponse CreatedAt = %q", got.CreatedAt)
	}
}

func TestToCommentResponseNilEditedAt(t *testing.T) {
	c := &database.Comment{ID: 1, UserID: "u", CreatedAt: time.Now()}
	got := toCommentResponse(c)
	if got.EditedAt != nil {
		t.Errorf("EditedAt = %v, want nil when Comment.EditedAt is nil", got.EditedAt)
	}
}
