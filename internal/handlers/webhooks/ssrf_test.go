package webhooks

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},       // loopback
		{"::1", true},             // loopback
		{"10.0.0.1", true},        // private RFC1918
		{"172.16.0.1", true},      // private RFC1918
		{"192.168.1.1", true},     // private RFC1918
		{"169.254.169.254", true}, // link-local (metadata service)
		{"0.0.0.0", true},         // unspecified
		{"224.0.0.1", true},       // multicast
		{"8.8.8.8", false},        // public
		{"1.1.1.1", false},        // public
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
