package comments

import (
	"fmt"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// listCommentTestDB builds the comment schema plus the lists table the
// list-target validation reads, and points the package's DB handle at it.
func listCommentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newCommentTestDB(t)
	if err := db.AutoMigrate(&database.List{}); err != nil {
		t.Fatalf("migrate lists: %v", err)
	}
	return db
}

// seedCommentList inserts one list row.
func seedCommentList(t *testing.T, db *gorm.DB, id int64, owner, privacy string, allowComments bool) {
	t.Helper()
	l := database.List{
		ID: id, UserID: owner, Name: fmt.Sprintf("List %d", id),
		Slug: fmt.Sprintf("list-%d", id), Privacy: privacy,
		SortBy: "rank", SortHow: "asc", AllowComments: allowComments,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(&l).Error; err != nil {
		t.Fatalf("seed list %d: %v", id, err)
	}
	// AllowComments carries a DB default of true, and GORM omits the zero value
	// of a field with a default tag on insert — so force the stored value.
	if err := db.Model(&database.List{}).Where("id = ?", id).
		UpdateColumn("allow_comments", allowComments).Error; err != nil {
		t.Fatalf("set allow_comments on list %d: %v", id, err)
	}
}

// postContractComment drives handleCreateComment as userID and returns the
// status and body.
func postContractComment(t *testing.T, userID, body string) (int, string) {
	t.Helper()
	app := newHandlerTestApp()
	asUser(app, "POST", "/comments/", userID, handleCreateComment)
	return doJSON(t, app, "POST", "/comments/", body)
}

func commentRowCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&database.Comment{}).Count(&n).Error; err != nil {
		t.Fatalf("count comments: %v", err)
	}
	return n
}

// A comment must not be created against a private list the caller cannot see.
func TestCreateCommentOnPrivateListIsRefused(t *testing.T) {
	db := listCommentTestDB(t)
	seedCommentList(t, db, 1, "owner", "private", true)

	status, body := postContractComment(t, "intruder", `{"comment":"hi","list":{"ids":{"trakt":1}}}`)
	if status != 403 {
		t.Fatalf("status = %d, want 403 on a private list the caller cannot see (%s)", status, body)
	}
	if n := commentRowCount(t, db); n != 0 {
		t.Fatalf("comments stored = %d, want 0 (refused write must not insert)", n)
	}
}

// A comment must not be created against a list that does not exist.
func TestCreateCommentOnMissingListIsRefused(t *testing.T) {
	db := listCommentTestDB(t)

	status, body := postContractComment(t, "u1", `{"comment":"hi","list":{"ids":{"trakt":424242}}}`)
	if status != 404 {
		t.Fatalf("status = %d, want 404 on a nonexistent list (%s)", status, body)
	}
	if n := commentRowCount(t, db); n != 0 {
		t.Fatalf("comments stored = %d, want 0 (refused write must not insert)", n)
	}
}

// A comment must not be created against a list that has disabled comments.
func TestCreateCommentOnListWithCommentsDisabledIsRefused(t *testing.T) {
	db := listCommentTestDB(t)
	seedCommentList(t, db, 1, "owner", "public", false)

	status, body := postContractComment(t, "visitor", `{"comment":"hi","list":{"ids":{"trakt":1}}}`)
	if status != 403 {
		t.Fatalf("status = %d, want 403 on a list with comments disabled (%s)", status, body)
	}
	if n := commentRowCount(t, db); n != 0 {
		t.Fatalf("comments stored = %d, want 0 (refused write must not insert)", n)
	}
}

// The ordinary allowed case still creates the comment.
func TestCreateCommentOnAllowedListSucceeds(t *testing.T) {
	db := listCommentTestDB(t)
	seedCommentList(t, db, 1, "owner", "public", true)

	status, body := postContractComment(t, "visitor", `{"comment":"nice list","list":{"ids":{"trakt":1}}}`)
	if status != 201 {
		t.Fatalf("status = %d, want 201 on an allowed list (%s)", status, body)
	}
	if n := commentRowCount(t, db); n != 1 {
		t.Fatalf("comments stored = %d, want 1", n)
	}
}

// An empty or over-length body is a 400 written once, not a 400 overwritten by
// a 201 after the insert carried on.
func TestCreateCommentRejectsInvalidBodyWithoutInserting(t *testing.T) {
	db := listCommentTestDB(t)
	seedCommentList(t, db, 1, "owner", "public", true)

	status, body := postContractComment(t, "u1", `{"comment":"","list":{"ids":{"trakt":1}}}`)
	if status != 400 {
		t.Fatalf("empty body status = %d, want 400 (%s)", status, body)
	}
	if n := commentRowCount(t, db); n != 0 {
		t.Fatalf("comments stored = %d, want 0 (invalid body must not insert)", n)
	}

	tooLong := fmt.Sprintf("%q", repeatString("a", maxCommentLength+1))
	status, body = postContractComment(t, "u1", `{"comment":`+tooLong+`,"list":{"ids":{"trakt":1}}}`)
	if status != 400 {
		t.Fatalf("over-length body status = %d, want 400 (%s)", status, body)
	}
	if n := commentRowCount(t, db); n != 0 {
		t.Fatalf("comments stored = %d, want 0 (over-length body must not insert)", n)
	}
}

func repeatString(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
