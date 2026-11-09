package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A redirect must be observed, not followed.
//
// /oauth/authorize answers 302 to the registered callback. When the harness
// followed it, the request to the callback host failed with a DNS error and the
// operation was reported with cause `harness` - which hid the real finding (we
// answer 302 where the contract declares 200) behind a checker fault. A harness
// error must never stand in for a handler difference, so the client has to
// return the 3xx itself.
func TestClientDoesNotFollowRedirects(t *testing.T) {
	followed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/local/login":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"session":{"token":"test-token"}}`))
		case "/v1/oauth/authorize":
			w.Header().Set("Location", "https://example.test/callback?code=abc")
			w.WriteHeader(http.StatusFound)
		default:
			// Reaching any other path means the redirect was followed.
			followed = true
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c, err := newClient(srv.URL, 5*time.Second, Fixtures{Email: "a@b.test", Password: "pw"}, nil, nil)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	resp, err := c.doJSON(http.MethodGet, "/v1/oauth/authorize", nil, false)
	if err != nil {
		t.Fatalf("doJSON followed the redirect instead of returning it: %v", err)
	}
	if followed {
		t.Fatal("the redirect was followed; a 3xx must be graded, not chased")
	}
	if resp.status != http.StatusFound {
		t.Fatalf("status = %d, want %d (the 302 itself)", resp.status, http.StatusFound)
	}
}
