package webhooks

import (
	"encoding/base64"
	"strings"
	"testing"
)

// testRootKey is a valid root key: 64 characters (≥32, so the shared root-key
// loader accepts it).
const testRootKey = "abababababababababababababababababababababababababababababababab"

func setRootKey(t *testing.T) {
	t.Helper()
	t.Setenv("TKILLER_SECRET_KEY", testRootKey)
}

func TestWebhookSecretRoundTrip(t *testing.T) {
	setRootKey(t)
	const plaintext = "0123456789abcdef0123456789abcdef"

	stored, err := EncryptSecret(7, plaintext)
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	if stored == plaintext {
		t.Fatal("stored value equals the plaintext")
	}
	if !strings.HasPrefix(stored, "enc:v1:") {
		t.Fatalf("stored value missing enc:v1: envelope: %q", stored)
	}

	got, err := DecryptSecret(7, stored)
	if err != nil {
		t.Fatalf("DecryptSecret: %v", err)
	}
	if got != plaintext {
		t.Fatalf("round-trip = %q, want %q", got, plaintext)
	}

	// Fresh nonce: two encryptions of the same plaintext must differ.
	stored2, err := EncryptSecret(7, plaintext)
	if err != nil {
		t.Fatalf("EncryptSecret(2): %v", err)
	}
	if stored == stored2 {
		t.Fatal("two encryptions of the same secret were identical; nonce is not random")
	}
}

// TestWebhookSecretRowBinding proves the AAD binds a ciphertext to its endpoint:
// decrypting endpoint 1's blob as endpoint 2 fails, so a database-write attacker
// cannot move a secret between rows.
func TestWebhookSecretRowBinding(t *testing.T) {
	setRootKey(t)
	stored, err := EncryptSecret(1, "row-one-secret")
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	if got, err := DecryptSecret(2, stored); err == nil {
		t.Fatalf("decrypting row 1's ciphertext as row 2 = (%q, nil), want error", got)
	}
	if got, err := DecryptSecret(1, stored); err != nil || got != "row-one-secret" {
		t.Fatalf("decrypting under the correct row = (%q, %v), want (\"row-one-secret\", nil)", got, err)
	}
}

// TestWebhookSecretTamperDetection flips a byte of the sealed body; GCM must
// reject it.
func TestWebhookSecretTamperDetection(t *testing.T) {
	setRootKey(t)
	stored, err := EncryptSecret(3, "tamper-me")
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(stored, "enc:v1:"))
	if err != nil {
		t.Fatalf("decode body: %v", err)
	}
	raw[len(raw)-1] ^= 0xFF
	tampered := "enc:v1:" + base64.RawURLEncoding.EncodeToString(raw)
	if got, err := DecryptSecret(3, tampered); err == nil {
		t.Fatalf("tampered ciphertext decrypted to (%q, nil), want error", got)
	}
}

// TestWebhookSecretWrongKey proves a value encrypted under one root key cannot
// be read under another.
func TestWebhookSecretWrongKey(t *testing.T) {
	setRootKey(t)
	stored, err := EncryptSecret(4, "key-specific")
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	t.Setenv("TKILLER_SECRET_KEY", strings.Repeat("cd", 24)) // 48 chars, a different key
	if got, err := DecryptSecret(4, stored); err == nil {
		t.Fatalf("decrypting under the wrong key = (%q, nil), want error", got)
	}
}

// TestWebhookSecretEmptyIsEmpty pins the read-path contract: an absent stored
// value stays empty rather than erroring or becoming a credential.
func TestWebhookSecretEmptyIsEmpty(t *testing.T) {
	setRootKey(t)
	got, err := DecryptSecret(5, "")
	if err != nil || got != "" {
		t.Fatalf("DecryptSecret(\"\") = (%q, %v), want (\"\", nil)", got, err)
	}
}

// TestWebhookSecretUnrecognisedFails proves a value that is not an enc:v1:
// envelope (junk, or a leftover from an earlier format) is an error and is never
// handed back as the signing secret.
func TestWebhookSecretUnrecognisedFails(t *testing.T) {
	setRootKey(t)
	for _, bad := range []string{
		strings.Repeat("ab", 32), // 64 hex chars: a bare HMAC-shaped value
		"not-base64!",
		"aGVsbG8=", // valid base64 but too short / not a sealed blob
		"some-random-plaintext",
	} {
		if got, err := DecryptSecret(1, bad); err == nil {
			t.Errorf("DecryptSecret(%q) = (%q, nil), want error (never return raw bytes)", bad, got)
		}
	}
}

// TestWebhookSecretMissingOrShortKey proves the server-side guarantee: with no
// usable root key, neither encrypt nor decrypt proceeds.
func TestWebhookSecretMissingOrShortKey(t *testing.T) {
	t.Setenv("TKILLER_SECRET_KEY", "")
	if _, err := EncryptSecret(1, "x"); err == nil {
		t.Error("EncryptSecret with no root key succeeded, want error")
	}
	if _, err := DecryptSecret(1, "anything"); err == nil {
		t.Error("DecryptSecret with no root key succeeded, want error")
	}

	t.Setenv("TKILLER_SECRET_KEY", strings.Repeat("a", 31)) // one short of the floor
	if _, err := EncryptSecret(1, "x"); err == nil {
		t.Error("EncryptSecret with a 31-char key succeeded, want error")
	}
}
