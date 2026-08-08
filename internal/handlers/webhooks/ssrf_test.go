package webhooks

import (
	"net"
	"strings"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},     // loopback
		{"::1", true},           // loopback
		{"10.0.0.1", true},      // private RFC1918
		{"172.16.0.1", true},    // private RFC1918
		{"192.168.1.1", true},   // private RFC1918
		{"169.254.169.254", true}, // link-local (metadata service)
		{"0.0.0.0", true},       // unspecified
		{"224.0.0.1", true},     // multicast
		{"8.8.8.8", false},      // public
		{"1.1.1.1", false},      // public
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("failed to parse test ip %q", tc.ip)
		}
		if got := isBlockedIP(ip); got != tc.want {
			t.Errorf("isBlockedIP(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}

func TestIsValidHTTPSRejectsNonHTTPS(t *testing.T) {
	for _, u := range []string{
		"http://example.com/webhook",
		"ftp://example.com/webhook",
		"",
		"not-a-url",
	} {
		if err := isValidHTTPS(u); err == nil {
			t.Errorf("isValidHTTPS(%q) expected error", u)
		}
	}
}

func TestIsValidHTTPSRejectsUserinfo(t *testing.T) {
	// Scheme + host are valid, but userinfo is forbidden (SSRF/confusion).
	if err := isValidHTTPS("https://user:pass@example.com/hook"); err == nil {
		t.Error("isValidHTTPS accepted a URL with userinfo")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET_KEY", strings.Repeat("ab", 32))
	secret := "0123456789abcdef"

	stored, err := EncryptSecret(secret)
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	if stored == secret {
		t.Error("stored value must not equal the plaintext")
	}

	got, err := DecryptSecret(stored)
	if err != nil {
		t.Fatalf("DecryptSecret: %v", err)
	}
	if got != secret {
		t.Errorf("round-trip = %q, want %q", got, secret)
	}

	stored2, err := EncryptSecret(secret)
	if err != nil {
		t.Fatalf("EncryptSecret(2): %v", err)
	}
	if stored == stored2 {
		t.Error("encryptions must be non-deterministic (fresh nonce)")
	}
}

func TestDecryptSecretErrors(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET_KEY", strings.Repeat("ab", 32))
	for _, bad := range []string{"", "not-base64!", "aGVsbG8="} {
		if _, err := DecryptSecret(bad); err == nil {
			t.Errorf("DecryptSecret(%q) expected error", bad)
		}
	}

	t.Setenv("WEBHOOK_SECRET_KEY", "")
	if _, err := EncryptSecret("x"); err == nil {
		t.Error("EncryptSecret expected error when key is missing")
	}
}
