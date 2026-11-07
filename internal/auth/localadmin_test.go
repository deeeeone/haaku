package auth

import (
	"fmt"
	"strings"
	"testing"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Admin panel (issue #53 §4): list, ban/lock, unlock, approve, reset. Every one
// of them is admin-only, and a ban has to reach the sessions the account is
// already holding.

// adminFixture bootstraps the owner (admin) and one approved member, returning
// the app, the owner's token, the member's token and the member's id.
func adminFixture(t *testing.T) (*gorm.DB, *fiber.App, string, string, string) {
	t.Helper()
	db, a := setupLocalAuth(t)
	ownerToken := signupToken(t, a, "owner@example.com")

	member := signup(t, a, "member@example.com", "correct-horse-battery")
	memberID, _ := member.body["user"].(map[string]any)["id"].(string)
	if r := call(t, a, "POST", "/v1/admin/users/"+memberID+"/approve", ownerToken, ""); r.status != 200 {
		t.Fatalf("approve = %d (%s)", r.status, r.raw)
	}
	r := login(t, a, "member@example.com", "correct-horse-battery")
	if r.status != 200 {
		t.Fatalf("member login = %d (%s)", r.status, r.raw)
	}
	memberToken, _ := r.body["session"].(map[string]any)["token"].(string)
	return db, a, ownerToken, memberToken, memberID
}

func TestAdminEndpointsRejectNonAdminsWith403(t *testing.T) {
	_, a, _, memberToken, memberID := adminFixture(t)

	calls := []struct{ method, path, body string }{
		{"GET", "/v1/admin/users", ""},
		{"POST", "/v1/admin/users/" + memberID + "/ban", ""},
		{"POST", "/v1/admin/users/" + memberID + "/unlock", ""},
		{"POST", "/v1/admin/users/" + memberID + "/approve", ""},
		{"POST", "/v1/admin/users/" + memberID + "/password-reset", ""},
	}
	for _, tc := range calls {
		if r := call(t, a, tc.method, tc.path, memberToken, tc.body); r.status != 403 {
			t.Errorf("non-admin %s %s = %d (%s), want 403", tc.method, tc.path, r.status, r.raw)
		}
		if r := call(t, a, tc.method, tc.path, "", tc.body); r.status != 401 {
			t.Errorf("anonymous %s %s = %d (%s), want 401", tc.method, tc.path, r.status, r.raw)
		}
	}
}

func TestAdminBanReachesAnExistingSession(t *testing.T) {
	db, a, ownerToken, memberToken, memberID := adminFixture(t)

	// The member's session is live before the ban (403 not 401: they are
	// authenticated, just not an admin).
	if r := call(t, a, "GET", "/v1/admin/users", memberToken, ""); r.status != 403 {
		t.Fatalf("pre-ban call with the member session = %d (%s), want 403", r.status, r.raw)
	}

	ban := call(t, a, "POST", "/v1/admin/users/"+memberID+"/ban", ownerToken, "")
	if ban.status != 200 || ban.body["status"] != database.UserStatusBanned {
		t.Fatalf("ban = %d %s", ban.status, ban.raw)
	}

	// The ban writes the revocation itself — not merely a status the read path
	// happens to check afterwards.
	var live database.LocalSession
	if err := db.Where("user_id = ?", memberID).Order("created_at DESC").First(&live).Error; err != nil {
		t.Fatalf("banned user's session row: %v", err)
	}
	if live.RevokedAt == nil {
		t.Fatal("ban left the account's session row unrevoked")
	}

	// The token the member is holding is dead.
	if r := call(t, a, "GET", "/v1/admin/users", memberToken, ""); r.status != 401 {
		t.Fatalf("banned account's existing session = %d (%s), want 401", r.status, r.raw)
	}
	// And a fresh login is refused, with a code the UI can explain.
	r := login(t, a, "member@example.com", "correct-horse-battery")
	if r.status != 403 || errorCode(t, r) != "account_banned" {
		t.Fatalf("banned login = %d %s, want 403 account_banned", r.status, r.raw)
	}
}

func TestAdminUnlockRestoresLogin(t *testing.T) {
	_, a, ownerToken, _, memberID := adminFixture(t)

	if r := call(t, a, "POST", "/v1/admin/users/"+memberID+"/ban", ownerToken, ""); r.status != 200 {
		t.Fatalf("ban = %d (%s)", r.status, r.raw)
	}
	if r := login(t, a, "member@example.com", "correct-horse-battery"); r.status != 403 {
		t.Fatalf("login while banned = %d (%s), want 403", r.status, r.raw)
	}
	if r := call(t, a, "POST", "/v1/admin/users/"+memberID+"/unlock", ownerToken, ""); r.status != 200 {
		t.Fatalf("unlock = %d (%s)", r.status, r.raw)
	}
	if r := login(t, a, "member@example.com", "correct-horse-battery"); r.status != 200 {
		t.Fatalf("login after unlock = %d (%s), want 200", r.status, r.raw)
	}
}

func TestAdminCannotLockTheirOwnAccount(t *testing.T) {
	db, a := setupLocalAuth(t)
	ownerToken := signupToken(t, a, "owner@example.com")

	var owner database.User
	if err := db.First(&owner).Error; err != nil {
		t.Fatalf("owner row: %v", err)
	}
	if r := call(t, a, "POST", "/v1/admin/users/"+owner.ID+"/ban", ownerToken, ""); r.status != 400 {
		t.Fatalf("self-ban = %d (%s), want 400", r.status, r.raw)
	}
}

func TestAdminListPaginationAndFields(t *testing.T) {
	_, a := setupLocalAuth(t)
	ownerToken := signupToken(t, a, "owner@example.com")
	for i := 0; i < 3; i++ {
		signup(t, a, fmt.Sprintf("member%d@example.com", i), "correct-horse-battery")
	}

	r := call(t, a, "GET", "/v1/admin/users?limit=2&offset=0", ownerToken, "")
	if r.status != 200 {
		t.Fatalf("list = %d (%s)", r.status, r.raw)
	}
	if total, _ := r.body["total"].(float64); total != 4 {
		t.Fatalf("total = %v, want 4", r.body["total"])
	}
	users, _ := r.body["users"].([]any)
	if len(users) != 2 {
		t.Fatalf("page size = %d, want 2", len(users))
	}
	first, _ := users[0].(map[string]any)
	for _, key := range []string{"id", "email", "username", "created_at", "status"} {
		if _, ok := first[key]; !ok {
			t.Errorf("list row is missing %q: %v", key, first)
		}
	}

	// The status filter is what the approval queue renders.
	pend := call(t, a, "GET", "/v1/admin/users?status=pending", ownerToken, "")
	if pend.status != 200 {
		t.Fatalf("filtered list = %d (%s)", pend.status, pend.raw)
	}
	if total, _ := pend.body["total"].(float64); total != 3 {
		t.Fatalf("pending total = %v, want 3", pend.body["total"])
	}
}

func TestAdminUnknownUserIs404(t *testing.T) {
	_, a := setupLocalAuth(t)
	ownerToken := signupToken(t, a, "owner@example.com")
	for _, action := range []string{"ban", "unlock", "approve", "password-reset"} {
		if r := call(t, a, "POST", "/v1/admin/users/local_missing/"+action, ownerToken, ""); r.status != 404 {
			t.Errorf("%s unknown user = %d (%s), want 404", action, r.status, r.raw)
		}
	}
}

func TestAdminResetTokenIsNotStoredRaw(t *testing.T) {
	db, a := setupLocalAuth(t)
	ownerToken := signupToken(t, a, "owner@example.com")

	var owner database.User
	if err := db.First(&owner).Error; err != nil {
		t.Fatalf("owner row: %v", err)
	}
	r := call(t, a, "POST", "/v1/admin/users/"+owner.ID+"/password-reset", ownerToken, "")
	if r.status != 200 {
		t.Fatalf("issue reset = %d (%s)", r.status, r.raw)
	}
	raw, _ := r.body["token"].(string)

	var rows []database.LocalAuthToken
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("load tokens: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("token rows = %d, want 1", len(rows))
	}
	if rows[0].TokenHash == raw || strings.Contains(rows[0].TokenHash, ResetTokenPrefix) {
		t.Fatalf("the raw reset token is stored: %+v", rows[0])
	}
	if rows[0].CreatedBy != owner.ID {
		t.Errorf("created_by = %q, want the issuing admin %q", rows[0].CreatedBy, owner.ID)
	}
}
