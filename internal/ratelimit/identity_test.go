package ratelimit

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func mustCIDRs(t *testing.T, raw ...string) []*net.IPNet {
	t.Helper()
	nets := make([]*net.IPNet, 0, len(raw))
	for _, entry := range raw {
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			t.Fatalf("ParseCIDR(%q): %v", entry, err)
		}
		nets = append(nets, network)
	}
	return nets
}

// TestResolveIdentityOverFacts covers the branches a socket cannot reach. The
// rule under test is the one that made every client in the house share one
// bucket: a forwarded header is believed only when the peer is trusted.
func TestResolveIdentityOverFacts(t *testing.T) {
	loopback := mustCIDRs(t, "127.0.0.1/32", "::1/128")
	lan := mustCIDRs(t, "10.0.0.0/8")

	tests := []struct {
		name    string
		peer    string
		xff     string
		xreal   string
		trusted []*net.IPNet
		want    ClientIdentity
	}{
		{
			name: "untrusted peer: a forwarded header is just a header the client sent",
			peer: "198.51.100.9", xff: "203.0.113.5",
			trusted: loopback,
			want:    ClientIdentity{IP: "198.51.100.9"},
		},
		{
			name: "trusted proxy: the forwarded client is the identity",
			peer: "127.0.0.1", xff: "203.0.113.5",
			trusted: loopback,
			want:    ClientIdentity{IP: "203.0.113.5", Proxied: true},
		},
		{
			name: "trusted proxy appends itself: walk from the right and take the first untrusted hop",
			peer: "127.0.0.1", xff: "203.0.113.5, 127.0.0.1",
			trusted: loopback,
			want:    ClientIdentity{IP: "203.0.113.5", Proxied: true},
		},
		{
			name: "every hop trusted (internal caller): leftmost wins",
			peer: "127.0.0.1", xff: "127.0.0.1, 127.0.0.1",
			trusted: loopback,
			want:    ClientIdentity{IP: "127.0.0.1", Proxied: true},
		},
		{
			name: "declared proxy network, not loopback",
			peer: "10.0.0.7", xff: "203.0.113.5",
			trusted: lan,
			want:    ClientIdentity{IP: "203.0.113.5", Proxied: true},
		},
		{
			name: "trusted peer, X-Real-IP only",
			peer: "127.0.0.1", xreal: "203.0.113.9",
			trusted: loopback,
			want:    ClientIdentity{IP: "203.0.113.9", Proxied: true},
		},
		{
			name:    "trusted peer, no forwarded header: local, not proxied",
			peer:    "127.0.0.1",
			trusted: loopback,
			want:    ClientIdentity{IP: "127.0.0.1", Local: true},
		},
		{
			name: "trusted peer, garbage header falls through to the peer",
			peer: "127.0.0.1", xff: "not-an-ip", xreal: "also-not-an-ip",
			trusted: loopback,
			want:    ClientIdentity{IP: "127.0.0.1", Local: true},
		},
		{
			name: "IPv6 client behind a loopback proxy",
			peer: "::1", xff: "2001:db8::5",
			trusted: loopback,
			want:    ClientIdentity{IP: "2001:db8::5", Proxied: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peer := net.ParseIP(tt.peer)
			if peer == nil {
				t.Fatalf("bad peer in fixture: %q", tt.peer)
			}
			got := ResolveIdentity(peer, tt.peer, tt.xff, tt.xreal, tt.trusted)
			if got != tt.want {
				t.Errorf("ResolveIdentity = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestResolveIdentityWithoutAPeer is the unix-socket/test-listener fallback:
// no parseable peer, so fiber's own answer is used and nothing is trusted.
func TestResolveIdentityWithoutAPeer(t *testing.T) {
	got := ResolveIdentity(nil, "0.0.0.0:0", "203.0.113.5", "", mustCIDRs(t, "127.0.0.1/32"))
	want := ClientIdentity{IP: "0.0.0.0:0"}
	if got != want {
		t.Errorf("ResolveIdentity = %+v, want %+v", got, want)
	}
}

// TestClientIPOverARealConnection drives ClientIP through an actual TCP
// listener. Fiber's app.Test serves every request from 0.0.0.0:0, so the peer
// address — the whole point of this file — is only real over a socket.
func TestClientIPOverARealConnection(t *testing.T) {
	trusted := mustCIDRs(t, "127.0.0.1/32")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	app := fiber.New()
	app.Get("/whoami", func(c *fiber.Ctx) error {
		return c.JSON(ClientIP(c, trusted))
	})
	go func() { _ = app.Listener(ln) }()
	defer func() { _ = app.Shutdown() }()

	base := "http://" + ln.Addr().String() + "/whoami"
	waitForListen(t, base)

	tests := []struct {
		name  string
		xff   string
		xreal string
		want  ClientIdentity
	}{
		{
			name: "direct loopback call with no headers is local",
			want: ClientIdentity{IP: "127.0.0.1", Local: true},
		},
		{
			name: "loopback proxy forwards the browser",
			xff:  "203.0.113.5",
			want: ClientIdentity{IP: "203.0.113.5", Proxied: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, base, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xreal != "" {
				req.Header.Set("X-Real-Ip", tt.xreal)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			var got ClientIdentity
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("decode %s: %v", string(body), err)
			}
			if got != tt.want {
				t.Errorf("ClientIP = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// waitForListen blocks until the listener answers, so the test never races the
// server's own startup. A failing read is retried; a slow success is fine.
func waitForListen(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("listener at %s never answered", url)
}
