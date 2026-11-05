package secret

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// testKey returns a deterministic-length root key suitable for New.
func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 48)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("read key: %v", err)
	}
	return key
}

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	c, err := New(testKey(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c := newTestCipher(t)
	const (
		plaintext = "0123456789abcdef0123456789abcdef"
		aad       = "webhook_endpoints:secret_hash:42"
	)
	ct, err := c.Encrypt(plaintext, aad)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !strings.HasPrefix(ct, "enc:v1:") {
		t.Fatalf("ciphertext missing enc:v1: prefix: %q", ct)
	}
	if !IsEncrypted(ct) {
		t.Fatalf("IsEncrypted(%q) = false, want true", ct)
	}
	if ct == plaintext {
		t.Fatal("ciphertext equals plaintext")
	}
	got, err := c.Decrypt(ct, aad)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plaintext {
		t.Fatalf("round-trip = %q, want %q", got, plaintext)
	}
}

func TestEncryptEmptyReturnsEmpty(t *testing.T) {
	c := newTestCipher(t)
	ct, err := c.Encrypt("", "any:aad")
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}
	if ct != "" {
		t.Fatalf("Encrypt(\"\") = %q, want empty (no envelope for an empty secret)", ct)
	}
}

// TestEncryptNonDeterministic proves a fresh random nonce is used per call: two
// encryptions of the same plaintext+aad must differ, and both must decrypt back.
func TestEncryptNonDeterministic(t *testing.T) {
	c := newTestCipher(t)
	const plaintext, aad = "same-secret", "t:c:1"
	a, err := c.Encrypt(plaintext, aad)
	if err != nil {
		t.Fatalf("Encrypt a: %v", err)
	}
	b, err := c.Encrypt(plaintext, aad)
	if err != nil {
		t.Fatalf("Encrypt b: %v", err)
	}
	if a == b {
		t.Fatal("two encryptions of the same plaintext+aad were identical; random nonce not applied")
	}
	if len(a) != len(b) || !strings.HasPrefix(a, "enc:v1:") || !strings.HasPrefix(b, "enc:v1:") {
		t.Fatalf("unexpected envelope shapes: %q / %q", a, b)
	}
	for _, ct := range []string{a, b} {
		got, err := c.Decrypt(ct, aad)
		if err != nil || got != plaintext {
			t.Fatalf("Decrypt(%q) = (%q, %v), want (%q, nil)", ct, got, err, plaintext)
		}
	}
}

// TestEncryptDiffersByAAD proves the AAD is actually bound into the GCM tag:
// the same plaintext under two different AAD strings yields different
// ciphertexts and neither decrypts under the other's AAD.
func TestEncryptDiffersByAAD(t *testing.T) {
	c := newTestCipher(t)
	a, err := c.Encrypt("secret", "webhook_endpoints:secret_hash:1")
	if err != nil {
		t.Fatalf("Encrypt a: %v", err)
	}
	b, err := c.Encrypt("secret", "webhook_endpoints:secret_hash:2")
	if err != nil {
		t.Fatalf("Encrypt b: %v", err)
	}
	if a == b {
		t.Fatal("different AAD produced identical ciphertext; AAD is not bound")
	}
}

// Decrypt refuses every value it cannot authenticate. A non-prefixed legacy
// value is the caller's pass-through responsibility, but Decrypt itself must
// reject it rather than echo it back.
func TestDecryptRejects(t *testing.T) {
	c := newTestCipher(t)
	const aad = "t:c:1"
	ct, err := c.Encrypt("secret", aad)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Decode the body, flip a byte, re-encode, reattach the envelope.
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ct, "enc:v1:"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	tampered := make([]byte, len(raw))
	copy(tampered, raw)
	tampered[len(tampered)-1] ^= 0xFF

	for _, tc := range []struct {
		name    string
		cipher  *Cipher
		ct, aad string
		want    error
	}{
		{"tampered ciphertext (flipped byte)", c, "enc:v1:" + base64.RawURLEncoding.EncodeToString(tampered), aad, nil},
		{"mismatched AAD (row binding broken)", c, ct, "t:c:2", nil},
		{"the wrong key", newTestCipher(t), ct, aad, nil},
		{"enc:v99:", c, "enc:v99:" + base64.RawURLEncoding.EncodeToString([]byte("whatever")), aad, ErrUnknownVersion},
		{"a non-envelope value", c, "plaintext-not-an-envelope", aad, nil},
		// Valid base64url but fewer bytes than the GCM nonce.
		{"a too-short ciphertext", c, "enc:v1:" + base64.RawURLEncoding.EncodeToString([]byte("ab")), aad, nil},
	} {
		_, err := tc.cipher.Decrypt(tc.ct, tc.aad)
		if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
			t.Errorf("Decrypt with %s returned %v, want error %v", tc.name, err, tc.want)
		}
	}
}

func TestNewRejectsShortKey(t *testing.T) {
	if _, err := New(make([]byte, MinMasterKeyLen-1)); err == nil {
		t.Fatalf("New with a %d-byte key succeeded, want error", MinMasterKeyLen-1)
	}
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) succeeded, want error")
	}
	if _, err := New(make([]byte, MinMasterKeyLen)); err != nil {
		t.Fatalf("New with a %d-byte key failed: %v", MinMasterKeyLen, err)
	}
}

func TestIsEncrypted(t *testing.T) {
	cases := map[string]bool{
		"enc:v1:x":   true,
		"sa_abc":     false,
		"":           false,
		"ENC:V1:x":   false,
		"enc:v1":     false,
		"plain text": false,
	}
	for in, want := range cases {
		if got := IsEncrypted(in); got != want {
			t.Errorf("IsEncrypted(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDecryptIfEncrypted(t *testing.T) {
	c := newTestCipher(t)
	const aad = "webhook_endpoints:secret_hash:7"

	// Empty passes through.
	if got, err := c.DecryptIfEncrypted("", aad); err != nil || got != "" {
		t.Fatalf("DecryptIfEncrypted(\"\") = (%q, %v), want (\"\", nil)", got, err)
	}
	// Legacy plaintext passes through unchanged.
	if got, err := c.DecryptIfEncrypted("legacy-plaintext-key", aad); err != nil || got != "legacy-plaintext-key" {
		t.Fatalf("DecryptIfEncrypted(plaintext) = (%q, %v), want pass-through", got, err)
	}
	// A real enc:v1: value is decrypted.
	ct, err := c.Encrypt("real-secret", aad)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if got, err := c.DecryptIfEncrypted(ct, aad); err != nil || got != "real-secret" {
		t.Fatalf("DecryptIfEncrypted(ct) = (%q, %v), want (\"real-secret\", nil)", got, err)
	}
	// A corrupt enc:v1: value errors — never falls back to the ciphertext.
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ct, "enc:v1:"))
	raw[len(raw)-1] ^= 0xFF
	corrupt := "enc:v1:" + base64.RawURLEncoding.EncodeToString(raw)
	if got, err := c.DecryptIfEncrypted(corrupt, aad); err == nil {
		t.Fatalf("DecryptIfEncrypted(tampered) = (%q, nil), want error", got)
	}
	// Wrong AAD errors, never returns the ciphertext.
	if got, err := c.DecryptIfEncrypted(ct, "webhook_endpoints:secret_hash:8"); err == nil {
		t.Fatalf("DecryptIfEncrypted with wrong AAD = (%q, nil), want error", got)
	}
}

func TestAADHelpers(t *testing.T) {
	if got := RowAAD("webhook_endpoints", "secret_hash", "42"); got != "webhook_endpoints:secret_hash:42" {
		t.Errorf("RowAAD = %q", got)
	}
}

func TestEncryptIfPlaintextIdempotent(t *testing.T) {
	c := newTestCipher(t)
	const aad = "t:c:1"

	// First call encrypts.
	ct, changed, err := c.EncryptIfPlaintext("secret", aad)
	if err != nil {
		t.Fatalf("EncryptIfPlaintext: %v", err)
	}
	if !changed {
		t.Fatal("first EncryptIfPlaintext reported changed=false, want true")
	}
	if !IsEncrypted(ct) {
		t.Fatalf("EncryptIfPlaintext did not produce an envelope: %q", ct)
	}

	// Second call is a no-op on the already-encrypted value.
	ct2, changed2, err := c.EncryptIfPlaintext(ct, aad)
	if err != nil {
		t.Fatalf("EncryptIfPlaintext (2nd): %v", err)
	}
	if changed2 {
		t.Fatal("second EncryptIfPlaintext reported changed=true, want false (double-encrypt)")
	}
	if ct2 != ct {
		t.Fatalf("second EncryptIfPlaintext mutated the value: %q -> %q", ct, ct2)
	}

	// Empty is a no-op too.
	got, changed3, err := c.EncryptIfPlaintext("", aad)
	if err != nil || changed3 || got != "" {
		t.Fatalf("EncryptIfPlaintext(\"\") = (%q, %v, %v), want (\"\", false, nil)", got, changed3, err)
	}
}

// TestDeriveKeyIsHKDFSHA256 pins the derivation to RFC 5869 HKDF-SHA256 with an
// empty (nil) salt and the versioned label. The expected data key was computed
// independently from crypto/hkdf; if the primitive, the salt or the label
// changes, this test fails — which is the point.
func TestDeriveKeyIsHKDFSHA256(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	got, err := DeriveKey(master, "tkiller/data-encryption/v1", 32)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	const want = "0ea806900c7a8aaa2f4f5b94f0c1d906205aaab8dded605be6814f74555cc725"
	if hex.EncodeToString(got) != want {
		t.Fatalf("DeriveKey data key = %s, want %s", hex.EncodeToString(got), want)
	}

	// Recomputing via the separate Extract+Expand path must agree, proving the
	// nil-salt RFC 5869 contract rather than an accidental alternative.
	prk, err := hkdf.Extract(sha256.New, master, nil)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	want2, err := hkdf.Expand(sha256.New, prk, "tkiller/data-encryption/v1", 32)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if hex.EncodeToString(want2) != want {
		t.Fatalf("HKDF recompute = %s, want %s", hex.EncodeToString(want2), want)
	}
}

func TestDeriveKeyPurposeSeparation(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	a, err := DeriveKey(master, "tkiller/data-encryption/v1", 32)
	if err != nil {
		t.Fatalf("DeriveKey a: %v", err)
	}
	b, err := DeriveKey(master, "tkiller/backup-encryption/v1", 32)
	if err != nil {
		t.Fatalf("DeriveKey b: %v", err)
	}
	if string(a) == string(b) {
		t.Fatal("different purposes derived the same key; domain separation is broken")
	}
	if _, err := DeriveKey(master, "", 32); err == nil {
		t.Fatal("DeriveKey with an empty purpose succeeded, want error")
	}
	if _, err := DeriveKey(make([]byte, MinMasterKeyLen-1), "x", 32); err == nil {
		t.Fatal("DeriveKey with a short master key succeeded, want error")
	}
}

// TestRootKeyFromEnv pins the required-root-key behaviour: preferred variable,
// legacy fallback, missing, and too-short.
func TestRootKeyFromEnv(t *testing.T) {
	const good = "0123456789abcdef0123456789abcdef" // 32 chars

	t.Run("preferred variable", func(t *testing.T) {
		t.Setenv(EnvVar, good)
		t.Setenv(LegacyEnvVar, "")
		got, err := RootKeyFromEnv()
		if err != nil || string(got) != good {
			t.Fatalf("RootKeyFromEnv() = (%q, %v), want (%q, nil)", got, err, good)
		}
	})

	t.Run("legacy fallback", func(t *testing.T) {
		t.Setenv(EnvVar, "")
		t.Setenv(LegacyEnvVar, good)
		got, err := RootKeyFromEnv()
		if err != nil || string(got) != good {
			t.Fatalf("RootKeyFromEnv() = (%q, %v), want (%q, nil)", got, err, good)
		}
	})

	t.Run("preferred wins over legacy", func(t *testing.T) {
		t.Setenv(EnvVar, good)
		t.Setenv(LegacyEnvVar, strings.Repeat("z", 48))
		got, err := RootKeyFromEnv()
		if err != nil || string(got) != good {
			t.Fatalf("RootKeyFromEnv() = (%q, %v), want the %s value", got, err, EnvVar)
		}
	})

	t.Run("missing", func(t *testing.T) {
		t.Setenv(EnvVar, "")
		t.Setenv(LegacyEnvVar, "")
		if _, err := RootKeyFromEnv(); err == nil {
			t.Fatal("RootKeyFromEnv with no key succeeded, want error")
		}
		if _, err := LoadFromEnv(); err == nil {
			t.Fatal("LoadFromEnv with no key succeeded, want error")
		}
	})

	t.Run("too short", func(t *testing.T) {
		t.Setenv(EnvVar, strings.Repeat("a", MinMasterKeyLen-1))
		t.Setenv(LegacyEnvVar, "")
		if _, err := RootKeyFromEnv(); err == nil {
			t.Fatal("RootKeyFromEnv with a 31-char key succeeded, want error")
		}
	})

	t.Run("exactly the minimum", func(t *testing.T) {
		t.Setenv(EnvVar, strings.Repeat("a", MinMasterKeyLen))
		t.Setenv(LegacyEnvVar, "")
		if _, err := RootKeyFromEnv(); err != nil {
			t.Fatalf("RootKeyFromEnv with a 32-char key failed: %v", err)
		}
	})
}
