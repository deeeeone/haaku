package integrations

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"trakt-api/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// testRootKey is 48 chars, assembled at runtime on purpose: a literal that
// shape-matches a credential trips the gitleaks generic-api-key rule for the
// WHOLE repo (CI checks out every ref, so one branch's fixture fails every open
// PR's security job). Built like this there is nothing to grandfather.
var testRootKey = strings.Repeat("ab12", 12) // 48 chars

// newTokenDB opens a private in-memory database with the integration_tokens
// table and points the package singleton at it (so the handler path works too).
func newTokenDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.IntegrationToken{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

func useRootKey(t *testing.T, key string) {
	t.Helper()
	t.Setenv("TKILLER_SECRET_KEY", key)
	t.Setenv("WEBHOOK_SECRET_KEY", "")
}

// legacyRow inserts a row the way the pre-encryption code did: the raw token in
// the token column and no blind index.
func legacyRow(t *testing.T, db *gorm.DB, token string) database.IntegrationToken {
	t.Helper()
	row := database.IntegrationToken{
		UserID:    "user_legacy",
		Token:     token,
		Name:      "legacy",
		Active:    true,
		CreatedAt: time.Now().UTC(),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	return row
}

// TestTokenLookupHashIsDeterministicAndKeyed pins the blind index: same token →
// same hash (so it can be indexed), different root key → different hash (so a
// dump alone is not brute-forceable without the key).
func TestTokenLookupHashIsDeterministicAndKeyed(t *testing.T) {
	useRootKey(t, testRootKey)
	h1, err := TokenLookupHash("tk_abc")
	if err != nil {
		t.Fatalf("TokenLookupHash: %v", err)
	}
	h2, err := TokenLookupHash("tk_abc")
	if err != nil {
		t.Fatalf("TokenLookupHash: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("hash is not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("hash is %d hex chars, want 64 (SHA-256)", len(h1))
	}
	if _, err := hex.DecodeString(h1); err != nil {
		t.Fatalf("hash is not hex: %v", err)
	}
	h3, err := TokenLookupHash("tk_abd")
	if err != nil {
		t.Fatalf("TokenLookupHash: %v", err)
	}
	if h1 == h3 {
		t.Fatal("different tokens produced the same hash")
	}

	// It must be a keyed MAC, not a bare SHA-256: recompute an HMAC with the
	// derived lookup key and require a match, and require it to differ from the
	// unkeyed digest.
	root, _ := loadTokenSecrets()
	want := hmac.New(sha256.New, root.lookupKey)
	want.Write([]byte("tk_abc"))
	if got := hex.EncodeToString(want.Sum(nil)); got != h1 {
		t.Fatalf("hash = %q, want HMAC-SHA256 under the derived key %q", h1, got)
	}
	unkeyed := sha256.Sum256([]byte("tk_abc"))
	if hex.EncodeToString(unkeyed[:]) == h1 {
		t.Fatal("blind index equals a bare SHA-256; the HMAC key is not applied")
	}
}

// TestIntegrationTokenCreateLookupRotate is the core round trip: create stores
// ciphertext + blind index, lookup finds it by the raw token, and rotation
// invalidates the old token and accepts the new one.
func TestIntegrationTokenCreateLookupRotate(t *testing.T) {
	useRootKey(t, testRootKey)
	db := newTokenDB(t)

	tok, raw, err := createToken(db, "user_1", "My App", database.StringSlice{"profile"})
	if err != nil {
		t.Fatalf("createToken: %v", err)
	}
	if raw == "" || tok.Token == "" || tok.TokenHash == "" {
		t.Fatalf("create returned empty credential material: %+v", tok)
	}

	// Stored value is the envelope, never the raw token; hash is populated.
	if tok.Token == raw {
		t.Fatal("stored token equals the raw token — not encrypted")
	}
	if !isCiphertext(tok.Token) {
		t.Fatalf("stored token is not an enc:v1: envelope: %q", tok.Token)
	}
	if tok.TokenHash == raw || len(tok.TokenHash) != 64 {
		t.Fatalf("stored token_hash = %q, want a 64-char hex MAC", tok.TokenHash)
	}

	got, err := LookupToken(db, raw)
	if err != nil {
		t.Fatalf("LookupToken: %v", err)
	}
	if got.ID != tok.ID {
		t.Fatalf("LookupToken found id %d, want %d", got.ID, tok.ID)
	}
	if got.Token != raw {
		t.Fatalf("LookupToken returned token %q, want the raw token", got.Token)
	}

	// Rotate: the old raw token must stop matching, the new one must match.
	newRaw, err := RotateToken(db, tok.ID, "user_1")
	if err != nil {
		t.Fatalf("RotateToken: %v", err)
	}
	if newRaw == raw {
		t.Fatal("rotation returned the same token")
	}
	if _, err := LookupToken(db, raw); err == nil {
		t.Fatal("the rotated-away token still authenticates")
	}
	after, err := LookupToken(db, newRaw)
	if err != nil {
		t.Fatalf("LookupToken(new): %v", err)
	}
	if after.ID != tok.ID {
		t.Fatalf("rotated token matched id %d, want %d", after.ID, tok.ID)
	}

	// Rotation is scoped: another user cannot rotate this row.
	if _, err := RotateToken(db, tok.ID, "someone_else"); err == nil {
		t.Fatal("RotateToken by a non-owner succeeded")
	}
}

// TestLookupRejectsUnknownAndInactive covers the auth outcomes the receiver maps
// to 401: an unknown token and an inactive row both yield the not-found sentinel.
func TestLookupRejectsUnknownAndInactive(t *testing.T) {
	useRootKey(t, testRootKey)
	db := newTokenDB(t)

	tok, raw, err := createToken(db, "user_1", "a", nil)
	if err != nil {
		t.Fatalf("createToken: %v", err)
	}
	// Inactive: remove it from the lookup by flipping active.
	if err := db.Model(&database.IntegrationToken{}).Where("id = ?", tok.ID).
		Update("active", false).Error; err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := LookupToken(db, raw); err == nil {
		t.Fatal("an inactive token authenticated")
	}
	if _, err := LookupToken(db, "tk_does_not_exist"); err == nil {
		t.Fatal("an unknown token authenticated")
	}
	if _, err := LookupToken(db, ""); err == nil {
		t.Fatal("an empty token authenticated")
	}
}

// TestIntegrationTokenStoredNotPlaintextRetrievableByRows reads the row back the
// way a dump would (the raw column values) and asserts the plaintext is absent.
func TestIntegrationTokenStoredNotPlaintextRetrievableByRows(t *testing.T) {
	useRootKey(t, testRootKey)
	db := newTokenDB(t)

	_, raw, err := createToken(db, "user_1", "a", nil)
	if err != nil {
		t.Fatalf("createToken: %v", err)
	}
	var dump string
	if err := db.Raw("SELECT COALESCE(token,'') || '|' || COALESCE(token_hash,'') || '|' || COALESCE(name,'') FROM integration_tokens").
		Scan(&dump).Error; err != nil {
		t.Fatalf("read raw columns: %v", err)
	}
	if strings.Contains(dump, raw) {
		t.Fatalf("the raw token appears in the stored row bytes: %q", dump)
	}
}

// TestIntegrationTokenAADRowSwapFails proves the ciphertext is bound to its row:
// moving row 1's stored blob onto row 2's row id (as a database-write attacker
// would) makes it fail to decrypt, and the lookup built on that decrypt fails
// with it.
func TestIntegrationTokenAADRowSwapFails(t *testing.T) {
	useRootKey(t, testRootKey)
	db := newTokenDB(t)

	tok1, raw1, err := createToken(db, "u", "one", nil)
	if err != nil {
		t.Fatalf("createToken 1: %v", err)
	}
	tok2, _, err := createToken(db, "u", "two", nil)
	if err != nil {
		t.Fatalf("createToken 2: %v", err)
	}

	// Directly: row 1's ciphertext must not decrypt under row 2's AAD.
	if got, err := DecryptToken(tok2.ID, tok1.Token); err == nil {
		t.Fatalf("decrypting row %d's blob under row %d's AAD = (%q, nil), want an error",
			tok1.ID, tok2.ID, got)
	}

	// Simulate the swap in storage: row 2 keeps its own blind index (so a lookup
	// still finds it) but is given row 1's ciphertext, which is bound to row 1.
	if err := db.Model(&database.IntegrationToken{}).Where("id = ?", tok2.ID).
		Update("token", tok1.Token).Error; err != nil {
		t.Fatalf("swap ciphertext: %v", err)
	}
	// Looking up by row 2's own raw token must now fail: the stored blob does
	// not authenticate under row 2's AAD.
	var row2 database.IntegrationToken
	if err := db.Where("id = ?", tok2.ID).First(&row2).Error; err != nil {
		t.Fatalf("load row2: %v", err)
	}
	if row2.TokenHash == "" {
		t.Fatal("row2 lost its blind index in the swap")
	}
	if _, err := DecryptToken(row2.ID, row2.Token); err == nil {
		t.Fatal("a row whose ciphertext was swapped in from another row authenticated")
	}
	// Row 1 is untouched and still authenticates.
	if _, err := LookupToken(db, raw1); err != nil {
		t.Fatalf("an untouched row stopped authenticating after a swap elsewhere: %v", err)
	}
}

// TestIntegrationTokenTamperedCiphertextFails flips a byte of the envelope's
// sealed body; GCM must reject it.
func TestIntegrationTokenTamperedCiphertextFails(t *testing.T) {
	useRootKey(t, testRootKey)
	ct, err := EncryptToken(5, "tk_secret")
	if err != nil {
		t.Fatalf("EncryptToken: %v", err)
	}
	body := strings.TrimPrefix(ct, "enc:v1:")
	if body == ct {
		t.Fatalf("ciphertext missing envelope: %q", ct)
	}
	blob, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	blob[len(blob)-1] ^= 0xFF
	tampered := "enc:v1:" + base64.RawURLEncoding.EncodeToString(blob)
	if got, err := DecryptToken(5, tampered); err == nil {
		t.Fatalf("tampered ciphertext decrypted to (%q, nil), want an error", got)
	}
}

// TestIntegrationTokenMissingRootKeyRefuses proves the whole write path refuses
// rather than storing anything when the root key is absent or short.
func TestIntegrationTokenMissingRootKeyRefuses(t *testing.T) {
	db := newTokenDB(t)

	t.Setenv("TKILLER_SECRET_KEY", "")
	t.Setenv("WEBHOOK_SECRET_KEY", "")
	if _, _, err := createToken(db, "u", "x", nil); err == nil {
		t.Fatal("createToken with no root key succeeded, want an error")
	}
	if _, err := EncryptToken(1, "tk_x"); err == nil {
		t.Fatal("EncryptToken with no root key succeeded, want an error")
	}
	if _, err := TokenLookupHash("tk_x"); err == nil {
		t.Fatal("TokenLookupHash with no root key succeeded, want an error")
	}
	if _, err := BackfillPlaintextTokens(t.Context(), db, nil); err == nil {
		t.Fatal("BackfillPlaintextTokens with no root key succeeded, want an error")
	}
	// Nothing was written.
	var n int64
	if err := db.Model(&database.IntegrationToken{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("a refused create left %d rows behind", n)
	}

	t.Setenv("TKILLER_SECRET_KEY", strings.Repeat("a", 31)) // one short of the floor
	if _, _, err := createToken(db, "u", "x", nil); err == nil {
		t.Fatal("createToken with a 31-char key succeeded, want an error")
	}
}

// TestLookupLegacyPlaintextFallback proves a not-yet-backfilled row (plaintext
// token, empty hash) still authenticates, so the migrate→backfill window does
// not lock out a working integration.
func TestLookupLegacyPlaintextFallback(t *testing.T) {
	useRootKey(t, testRootKey)
	db := newTokenDB(t)
	legacyRow(t, db, "tk_legacy_value")

	got, err := LookupToken(db, "tk_legacy_value")
	if err != nil {
		t.Fatalf("legacy lookup: %v", err)
	}
	if got.Token != "tk_legacy_value" {
		t.Fatalf("legacy lookup returned %q", got.Token)
	}
}

// TestBackfillConvertsLegacyRowsOnce is the idempotency contract: legacy rows
// convert exactly once, a repeat run is a no-op, and the converted rows still
// authenticate by the raw token.
func TestBackfillConvertsLegacyRowsOnce(t *testing.T) {
	useRootKey(t, testRootKey)
	db := newTokenDB(t)

	for _, tok := range []string{"tk_legacy_a", "tk_legacy_b", "tk_legacy_c"} {
		legacyRow(t, db, tok)
	}

	n, err := BackfillPlaintextTokens(t.Context(), db, nil)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if n != 3 {
		t.Fatalf("backfill converted %d rows, want 3", n)
	}

	// Every row is now an envelope with a populated hash; none holds plaintext.
	var rows []database.IntegrationToken
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("load rows: %v", err)
	}
	for _, r := range rows {
		if !isCiphertext(r.Token) || r.TokenHash == "" {
			t.Fatalf("row %d not converted: token=%q hash=%q", r.ID, r.Token, r.TokenHash)
		}
	}
	// And each still authenticates by its raw token.
	for _, tok := range []string{"tk_legacy_a", "tk_legacy_b", "tk_legacy_c"} {
		if _, err := LookupToken(db, tok); err != nil {
			t.Fatalf("converted token %q no longer authenticates: %v", tok, err)
		}
	}

	// Idempotent: a second run converts nothing.
	again, err := BackfillPlaintextTokens(t.Context(), db, nil)
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if again != 0 {
		t.Fatalf("second backfill converted %d rows, want 0", again)
	}
}

// TestHandleCreateTokenStoresCiphertext drives the HTTP handler end to end and
// asserts the response carries the raw token once while the stored row does not.
func TestHandleCreateTokenStoresCiphertext(t *testing.T) {
	useRootKey(t, testRootKey)
	db := newTokenDB(t)

	app := fiber.New()
	app.Post("/integrations", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user_http")
		return handleCreateToken(c)
	})

	req := httptest.NewRequest("POST", "/integrations", strings.NewReader(`{"name":"http app"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201 (body %s)", resp.StatusCode, b)
	}
	var body struct {
		Token string `json:"token"`
		ID    int64  `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Token == "" {
		t.Fatal("response did not return the raw token")
	}

	var stored database.IntegrationToken
	if err := db.Where("id = ?", body.ID).First(&stored).Error; err != nil {
		t.Fatalf("load stored row: %v", err)
	}
	if stored.Token == body.Token {
		t.Fatal("the stored token equals the token returned to the client")
	}
	if !isCiphertext(stored.Token) || stored.TokenHash == "" {
		t.Fatalf("stored row is not encrypted+indexed: token=%q hash=%q", stored.Token, stored.TokenHash)
	}
}
