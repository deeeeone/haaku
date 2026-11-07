package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
)

// --- password KDF -----------------------------------------------------------

func TestHashPasswordProducesArgon2idPHCAndVerifies(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("hash is not the pinned argon2id PHC profile: %q", hash)
	}
	if err := VerifyPassword(hash, "correct-horse-battery"); err != nil {
		t.Fatalf("verify correct password: %v", err)
	}
	if err := VerifyPassword(hash, "correct-horse-batterz"); err != ErrInvalidCredentials {
		t.Fatalf("verify wrong password = %v, want ErrInvalidCredentials", err)
	}
	if err := VerifyPassword("not-a-hash", "x"); err != ErrInvalidHash {
		t.Fatalf("verify malformed hash = %v, want ErrInvalidHash", err)
	}
	// A fresh salt every time: two hashes of the same password must differ.
	other, err := HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hash again: %v", err)
	}
	if other == hash {
		t.Fatal("two hashes of the same password are identical — the salt is not random")
	}
}

func TestValidatePasswordBounds(t *testing.T) {
	if err := ValidatePassword("short"); err == nil {
		t.Fatal("ValidatePassword accepted a 5-character password")
	}
	if err := ValidatePassword(strings.Repeat("a", MaxPasswordLen+1)); err == nil {
		t.Fatal("ValidatePassword accepted an over-long password")
	}
	if err := ValidatePassword(strings.Repeat("a", MinPasswordLen)); err != nil {
		t.Fatalf("ValidatePassword rejected a minimum-length password: %v", err)
	}
}

// --- signup / login / logout ------------------------------------------------

func TestFirstSignupDoesNotBecomeOwner(t *testing.T) {
	db, app := setupLocalAuth(t)

	// Issue #53 follow-up: first-run ownership is gone. Whoever signs up first
	// is an ordinary account following the sign-up policy (here the default
	// approval policy: pending, no session), and is never an admin. The owner
	// comes from HAAKU_ADMIN_EMAIL / HAAKU_ADMIN_PASSWORD at startup.
	r := signup(t, app, "first@example.com", "correct-horse-battery")
	if r.status != 201 {
		t.Fatalf("signup status = %d (%s)", r.status, r.raw)
	}
	if r.body["status"] != database.UserStatusPending {
		t.Fatalf("first signup status = %v, want pending (the owner is env-seeded, not whoever signs up first)", r.body["status"])
	}
	if r.body["session"] != nil {
		t.Fatalf("a first signup that is not the owner was issued a session: %s", r.raw)
	}

	var user database.User
	if err := db.First(&user, "email = ?", "first@example.com").Error; err != nil {
		t.Fatalf("signup row: %v", err)
	}
	if user.IsAdmin {
		t.Error("the first signup became an admin — first-run ownership is supposed to be removed")
	}
	if !strings.HasPrefix(user.ID, LocalIDPrefix) {
		t.Errorf("account id %q is not namespaced with %q", user.ID, LocalIDPrefix)
	}
}

func TestLoginThenLogoutRevokesTheSession(t *testing.T) {
	_, app := setupLocalAuth(t)
	signupToken(t, app, "a@example.com")

	r := login(t, app, "a@example.com", "correct-horse-battery")
	if r.status != 200 {
		t.Fatalf("login = %d (%s)", r.status, r.raw)
	}
	sess, _ := r.body["session"].(map[string]any)
	token, _ := sess["token"].(string)
	if !strings.HasPrefix(token, SessionTokenPrefix) {
		t.Fatalf("session token %q does not carry the %q prefix", token, SessionTokenPrefix)
	}

	if r := call(t, app, "POST", "/v1/auth/local/logout", token, ""); r.status != 204 {
		t.Fatalf("logout = %d (%s)", r.status, r.raw)
	}
	// The route is authenticated, so a second logout with the revoked token is
	// 401 like any other invalid credential — the revoke itself is idempotent
	// (Update ... WHERE revoked_at IS NULL), the gate is not.
	if r := call(t, app, "POST", "/v1/auth/local/logout", token, ""); r.status != 401 {
		t.Fatalf("second logout = %d (%s), want 401", r.status, r.raw)
	}
	if r := call(t, app, "GET", "/v1/admin/users", token, ""); r.status != 401 {
		t.Fatalf("a revoked session still authenticates: %d (%s)", r.status, r.raw)
	}
}

func TestLoginWrongPasswordIsUnauthorized(t *testing.T) {
	_, app := setupLocalAuth(t)
	signupToken(t, app, "a@example.com")

	r := login(t, app, "a@example.com", "wrong-password-here")
	if r.status != 401 {
		t.Fatalf("wrong password = %d (%s), want 401", r.status, r.raw)
	}
	if code := errorCode(t, r); code != "unauthorized" {
		t.Fatalf("wrong password code = %q, want unauthorized", code)
	}
}

func TestLoginUnknownEmailIsIndistinguishableFromWrongPassword(t *testing.T) {
	_, app := setupLocalAuth(t)
	signupToken(t, app, "a@example.com")

	unknown := login(t, app, "nobody@example.com", "wrong-password-here")
	wrong := login(t, app, "a@example.com", "wrong-password-here")
	if unknown.status != wrong.status {
		t.Fatalf("unknown email status %d != wrong password status %d", unknown.status, wrong.status)
	}
	if uk, wk := errorCode(t, unknown), errorCode(t, wrong); uk != wk {
		t.Fatalf("unknown email code %q != wrong password code %q", uk, wk)
	}
	if unknown.raw != wrong.raw {
		t.Fatalf("responses differ:\n unknown: %s\n wrong:   %s", unknown.raw, wrong.raw)
	}
}

func TestSignupDuplicateEmailIsRejected(t *testing.T) {
	_, app := setupLocalAuth(t)
	signupToken(t, app, "a@example.com")

	r := signup(t, app, "A@Example.com", "correct-horse-battery")
	if r.status != 409 {
		t.Fatalf("duplicate email = %d (%s), want 409", r.status, r.raw)
	}
	if code := errorCode(t, r); code != "conflict" {
		t.Fatalf("duplicate email code = %q, want conflict", code)
	}
}

func TestPendingSignupCannotLogIn(t *testing.T) {
	db, app := setupLocalAuth(t)
	signupToken(t, app, "owner@example.com") // bootstrap owner

	r := signup(t, app, "member@example.com", "correct-horse-battery")
	if r.status != 201 {
		t.Fatalf("second signup = %d (%s)", r.status, r.raw)
	}
	if r.body["status"] != database.UserStatusPending {
		t.Fatalf("second signup status = %v, want pending", r.body["status"])
	}
	if r.body["session"] != nil {
		t.Fatalf("a pending account was issued a session: %s", r.raw)
	}
	var n int64
	if err := db.Model(&database.LocalSession{}).Count(&n).Error; err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 1 {
		t.Fatalf("session rows = %d, want 1 (only the owner's)", n)
	}

	r = login(t, app, "member@example.com", "correct-horse-battery")
	if r.status != 403 || errorCode(t, r) != "account_pending" {
		t.Fatalf("pending login = %d %s, want 403 account_pending", r.status, r.raw)
	}
}

func TestSignupPolicies(t *testing.T) {
	t.Run("open activates immediately", func(t *testing.T) {
		_, app := setupLocalAuth(t)
		if err := InitAuthMode("local", "open"); err != nil {
			t.Fatal(err)
		}
		signupToken(t, app, "owner@example.com")
		r := signup(t, app, "member@example.com", "correct-horse-battery")
		if r.status != 201 || r.body["status"] != database.UserStatusActive {
			t.Fatalf("open-policy signup = %d %s, want 201 active", r.status, r.raw)
		}
		sess, _ := r.body["session"].(map[string]any)
		if tok, _ := sess["token"].(string); tok == "" {
			t.Fatalf("open-policy signup issued no session: %s", r.raw)
		}
	})

	t.Run("closed refuses further signups", func(t *testing.T) {
		_, app := setupLocalAuth(t)
		if err := InitAuthMode("local", "closed"); err != nil {
			t.Fatal(err)
		}
		// The owner is seeded, not signed up: under "closed" no signup is
		// allowed at all, because the owner already exists.
		seedOwner(t, "owner@example.com")
		r := signup(t, app, "member@example.com", "correct-horse-battery")
		if r.status != 403 || errorCode(t, r) != "signup_closed" {
			t.Fatalf("closed-policy signup = %d %s, want 403 signup_closed", r.status, r.raw)
		}
	})
}

func TestSignupRejectsBadInput(t *testing.T) {
	_, app := setupLocalAuth(t)
	if r := signup(t, app, "not-an-email", "correct-horse-battery"); r.status != 400 {
		t.Fatalf("bad email = %d (%s)", r.status, r.raw)
	}
	if r := signup(t, app, "a@example.com", "short"); r.status != 400 {
		t.Fatalf("short password = %d (%s)", r.status, r.raw)
	}
}

// --- session storage --------------------------------------------------------

func TestSessionTokenIsStoredHashedNotRaw(t *testing.T) {
	db, app := setupLocalAuth(t)
	token := signupToken(t, app, "a@example.com")

	var sess database.LocalSession
	if err := db.First(&sess).Error; err != nil {
		t.Fatalf("session row: %v", err)
	}
	if sess.TokenHash == token {
		t.Fatal("the raw token is stored in the row — sessions are not opaque")
	}
	want, err := localTokenHash(token)
	if err != nil {
		t.Fatalf("hash token: %v", err)
	}
	if sess.TokenHash != want {
		t.Fatalf("stored hash %q != HKDF-keyed HMAC %q", sess.TokenHash, want)
	}
	if strings.Contains(sess.TokenHash, SessionTokenPrefix) {
		t.Errorf("stored hash %q still contains the token prefix", sess.TokenHash)
	}
	// The HMAC key is derived from the root key: a different root key must not
	// reproduce the stored hash.
	t.Setenv("HAAKU_SECRET_KEY", strings.Repeat("other-root-key-", 4))
	other, err := localTokenHash(token)
	if err != nil {
		t.Fatalf("hash token under a different root: %v", err)
	}
	if other == sess.TokenHash {
		t.Fatal("the blind index does not depend on HAAKU_SECRET_KEY")
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	db, app := setupLocalAuth(t)
	token := signupToken(t, app, "a@example.com")

	if err := db.Model(&database.LocalSession{}).
		Where("1 = 1").Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("expire session: %v", err)
	}
	if r := call(t, app, "GET", "/v1/admin/users", token, ""); r.status != 401 {
		t.Fatalf("expired session = %d (%s), want 401", r.status, r.raw)
	}
}

func TestUnknownTokenIsRejected(t *testing.T) {
	_, app := setupLocalAuth(t)
	if r := call(t, app, "GET", "/v1/admin/users", SessionTokenPrefix+"deadbeef", ""); r.status != 401 {
		t.Fatalf("unknown token = %d (%s), want 401", r.status, r.raw)
	}
}

// --- password reset ---------------------------------------------------------

func TestAdminResetTokenIsSingleUse(t *testing.T) {
	_, app := setupLocalAuth(t)
	ownerToken := signupToken(t, app, "owner@example.com")

	// A second account, approved, so it can log in before the reset.
	member := signup(t, app, "member@example.com", "correct-horse-battery")
	memberID, _ := member.body["user"].(map[string]any)["id"].(string)
	if r := call(t, app, "POST", "/v1/admin/users/"+memberID+"/approve", ownerToken, ""); r.status != 200 {
		t.Fatalf("approve = %d (%s)", r.status, r.raw)
	}
	oldToken := ""
	if r := login(t, app, "member@example.com", "correct-horse-battery"); r.status != 200 {
		t.Fatalf("member login = %d (%s)", r.status, r.raw)
	} else {
		oldToken, _ = r.body["session"].(map[string]any)["token"].(string)
	}

	r := call(t, app, "POST", "/v1/admin/users/"+memberID+"/password-reset", ownerToken, "")
	if r.status != 200 {
		t.Fatalf("issue reset token = %d (%s)", r.status, r.raw)
	}
	resetToken, _ := r.body["token"].(string)
	if !strings.HasPrefix(resetToken, ResetTokenPrefix) {
		t.Fatalf("reset token %q does not carry the %q prefix", resetToken, ResetTokenPrefix)
	}
	if r.body["delivery"] != "admin_handover" {
		t.Fatalf("delivery = %v, want admin_handover (no silent mail dependency)", r.body["delivery"])
	}

	body := `{"token":"` + resetToken + `","new_password":"brand-new-password"}`
	if r := call(t, app, "POST", "/v1/auth/local/password-reset", "", body); r.status != 200 {
		t.Fatalf("redeem reset token = %d (%s)", r.status, r.raw)
	}
	// Second redemption of the same token must fail.
	if r := call(t, app, "POST", "/v1/auth/local/password-reset", "", body); r.status != 400 {
		t.Fatalf("second redemption = %d (%s), want 400", r.status, r.raw)
	}
	// The new password works, the old one does not.
	if r := login(t, app, "member@example.com", "brand-new-password"); r.status != 200 {
		t.Fatalf("login with the new password = %d (%s)", r.status, r.raw)
	}
	if r := login(t, app, "member@example.com", "correct-horse-battery"); r.status != 401 {
		t.Fatalf("login with the old password = %d (%s), want 401", r.status, r.raw)
	}
	// And the sessions held under the old password are gone.
	if r := call(t, app, "POST", "/v1/auth/local/logout", oldToken, ""); r.status != 401 {
		t.Fatalf("pre-reset session = %d (%s), want 401 — a reset must end old sessions", r.status, r.raw)
	}
}

func TestResetWithUnknownTokenIsRejected(t *testing.T) {
	_, app := setupLocalAuth(t)
	body := `{"token":"hr_not-a-real-token","new_password":"brand-new-password"}`
	r := call(t, app, "POST", "/v1/auth/local/password-reset", "", body)
	if r.status != 400 || errorCode(t, r) != "invalid_request" {
		t.Fatalf("unknown reset token = %d %s, want 400 invalid_request", r.status, r.raw)
	}
}

// --- config discovery -------------------------------------------------------

func TestAuthConfigReportsLocalMode(t *testing.T) {
	_, app := setupLocalAuth(t)
	r := call(t, app, "GET", "/v1/auth/config", "", "")
	if r.status != 200 {
		t.Fatalf("config = %d (%s)", r.status, r.raw)
	}
	if r.body["mode"] != "local" || r.body["local_auth"] != true {
		t.Fatalf("config does not report local mode: %s", r.raw)
	}
	if r.body["first_run"] != true {
		t.Fatalf("first_run = %v on an empty instance, want true", r.body["first_run"])
	}
	if r.body["signup_policy"] != "approval" {
		t.Fatalf("signup_policy = %v, want the documented default approval", r.body["signup_policy"])
	}
}

// --- guard helper -----------------------------------------------------------

func TestLocalRoutesAreAbsentFromTheRouterInClerkMode(t *testing.T) {
	t.Setenv("HAAKU_SECRET_KEY", testRootKey)
	if err := InitAuthMode("clerk", "approval"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = InitAuthMode("", "") })

	app := fiber.New()
	api := app.Group("/v1")
	RegisterLocalAuthConfig(api)
	RegisterLocalAuthRoutes(api)
	for _, route := range app.GetRoutes() {
		if strings.HasPrefix(route.Path, "/v1/auth/local") || strings.HasPrefix(route.Path, "/v1/admin") {
			t.Errorf("clerk mode registered %s %s", route.Method, route.Path)
		}
	}
	// And the config route still exists, so a client can learn it is in clerk
	// mode instead of guessing.
	r := call(t, app, "GET", "/v1/auth/config", "", "")
	if r.status != 200 || r.body["mode"] != "clerk" || r.body["local_auth"] != false {
		t.Fatalf("clerk-mode config = %d %s", r.status, r.raw)
	}
	if r := call(t, app, "POST", "/v1/auth/local/login", "", `{}`); r.status != 404 {
		t.Fatalf("clerk-mode login = %d (%s), want 404", r.status, r.raw)
	}
}
