package ratelimit

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/config"
	projredis "github.com/haaku-tv/haaku/redis"
)

// Small ceilings, so a test can exhaust them. Production numbers live in
// groups.go and are deliberately not env-tunable one-by-one.
var (
	burstTiers = map[string]tier{
		GroupRead:  {BurstPublic: 3, BurstAuth: 5},
		GroupMedia: {BurstPublic: 1, BurstAuth: 5},
		GroupWrite: {BurstPublic: 1, BurstAuth: 5},
	}
	budgetTiers = map[string]tier{
		GroupRead: {BurstPublic: 50, BurstAuth: 50, DailyPublic: 2, DailyAuth: 2},
	}
)

// testConfig declares fiber's test transport peer (0.0.0.0:0) as a trusted
// proxy. That is what lets a test send X-Forwarded-For and get distinct
// anonymous identities out of a single process — the same knob a deployment
// behind a reverse proxy uses.
func testConfig(exemptLocal bool) config.RateLimitConfig {
	_, network, err := net.ParseCIDR("0.0.0.0/32")
	if err != nil {
		panic(err)
	}
	return config.RateLimitConfig{
		Enabled:          true,
		Window:           time.Minute,
		ExemptLocal:      exemptLocal,
		TrustedProxyNets: []*net.IPNet{network},
	}
}

// newLimiterApp builds the middleware chain the server uses — identity first
// (what auth.AttemptAuth does inside the limiter), then the limiter — over the
// handful of routes the tests drive.
func newLimiterApp(h fiber.Handler, signedInAs string) *fiber.App {
	app := fiber.New()
	if signedInAs != "" {
		app.Use(func(c *fiber.Ctx) error {
			c.Locals("user_id", signedInAs)
			return c.Next()
		})
	}
	app.Use(h)
	ok := func(c *fiber.Ctx) error { return c.SendStatus(http.StatusOK) }
	app.Get("/v1/users/me/activity", ok)
	app.Get("/v1/movies/:id", ok)
	app.Post("/v1/comments", ok)
	app.Get("/healthz", ok)
	return app
}

type callResult struct {
	status int
	body   string
	header http.Header
}

func call(t *testing.T, app *fiber.App, method, path, xff string) callResult {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return callResult{status: resp.StatusCode, body: string(body), header: resp.Header}
}

// requireRedis returns a live client with an empty limiter namespace, or skips.
func requireRedis(t *testing.T) *goredis.Client {
	t.Helper()
	if os.Getenv("REDIS_URL") == "" {
		t.Skip("REDIS_URL not set — the Redis-backed limiter tests need a real Redis")
	}
	if err := projredis.InitRedis(); err != nil {
		t.Fatalf("redis: %v", err)
	}
	client := projredis.GetClient()
	if client == nil {
		t.Fatal("redis client is nil after a successful InitRedis")
	}
	ctx := context.Background()
	for _, pattern := range []string{"rate_limit:*", "usage:*"} {
		keys, err := client.Keys(ctx, pattern).Result()
		if err != nil {
			t.Fatalf("keys %s: %v", pattern, err)
		}
		if len(keys) > 0 {
			if err := client.Del(ctx, keys...).Err(); err != nil {
				t.Fatalf("flush limiter keys: %v", err)
			}
		}
	}
	return client
}

func limiterKeys(t *testing.T, client *goredis.Client, pattern string) []string {
	t.Helper()
	keys, err := client.Keys(context.Background(), pattern).Result()
	if err != nil {
		t.Fatalf("keys %s: %v", pattern, err)
	}
	return keys
}

func errorCode(t *testing.T, body string) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("decode error envelope from %q: %v", body, err)
	}
	return envelope.Error.Code
}

// TestFailOpenWithoutRedis is declared first in this file on purpose: Go runs a
// package's tests in source order, and the Redis-backed tests below initialise
// the client singleton, which cannot be un-initialised. With no client the
// limiter must let traffic through — an instance with a dead Redis still
// serves, which on a self-hosted box is the lesser failure.
//
// The Redis path: run the suite with REDIS_URL unset (what CI's make test-go
// does) and this assertion is the one that runs.
func TestFailOpenWithoutRedis(t *testing.T) {
	if projredis.GetClient() != nil {
		t.Skip("a Redis client is already initialised in this test binary — see redis.SlidingWindow's ErrClientUnavailable for the fail-open contract")
	}
	app := newLimiterApp(limiter(testConfig(true), burstTiers), "")

	for i := 0; i < 10; i++ {
		got := call(t, app, fiber.MethodGet, "/v1/movies/550", "203.0.113.5")
		if got.status != http.StatusOK {
			t.Fatalf("request %d: status %d (body %s), want 200 — the limiter must fail open when Redis is unavailable", i+1, got.status, got.body)
		}
	}
}

// TestBurstRefusesAndReportsHeaders is the whole contract of the burst layer:
// the (max+1)th request in the window is a 429 with the rate_limit_exceeded
// code, and every response — allowed or refused — says how much of the
// allowance is left.
func TestBurstRefusesAndReportsHeaders(t *testing.T) {
	requireRedis(t)
	app := newLimiterApp(limiter(testConfig(true), burstTiers), "")
	client := "203.0.113.5"

	for i := 1; i <= 3; i++ {
		got := call(t, app, fiber.MethodGet, "/v1/users/me/activity", client)
		if got.status != http.StatusOK {
			t.Fatalf("request %d: status %d (body %s), want 200", i, got.status, got.body)
		}
		if limit := got.header.Get("X-RateLimit-Limit"); limit != "3" {
			t.Errorf("request %d: X-RateLimit-Limit = %q, want 3", i, limit)
		}
		if remaining := got.header.Get("X-RateLimit-Remaining"); remaining != strconv.Itoa(3-i) {
			t.Errorf("request %d: X-RateLimit-Remaining = %q, want %d", i, remaining, 3-i)
		}
		if got.header.Get("X-RateLimit-Reset") == "" {
			t.Errorf("request %d: no X-RateLimit-Reset header", i)
		}
	}

	refused := call(t, app, fiber.MethodGet, "/v1/users/me/activity", client)
	if refused.status != http.StatusTooManyRequests {
		t.Fatalf("4th request: status %d (body %s), want 429", refused.status, refused.body)
	}
	if code := errorCode(t, refused.body); code != "rate_limit_exceeded" {
		t.Errorf("429 code = %q, want rate_limit_exceeded", code)
	}
	if retry := refused.header.Get("Retry-After"); retry == "" || retry == "0" {
		t.Errorf("Retry-After = %q, want a positive number of seconds", retry)
	}
	if remaining := refused.header.Get("X-RateLimit-Remaining"); remaining != "0" {
		t.Errorf("refused: X-RateLimit-Remaining = %q, want 0", remaining)
	}
}

// TestTwoClientsDoNotShareABucket is the regression the logs showed: every
// browser on the box arrived from 127.0.0.1 and shared one counter, so one
// busy client throttled the rest.
func TestTwoClientsDoNotShareABucket(t *testing.T) {
	requireRedis(t)
	app := newLimiterApp(limiter(testConfig(true), burstTiers), "")

	for i := 0; i < 3; i++ {
		if got := call(t, app, fiber.MethodGet, "/v1/users/me/activity", "203.0.113.5"); got.status != http.StatusOK {
			t.Fatalf("client A request %d: status %d", i+1, got.status)
		}
	}
	if got := call(t, app, fiber.MethodGet, "/v1/users/me/activity", "203.0.113.5"); got.status != http.StatusTooManyRequests {
		t.Fatalf("client A 4th request: status %d, want 429", got.status)
	}
	if got := call(t, app, fiber.MethodGet, "/v1/users/me/activity", "203.0.113.6"); got.status != http.StatusOK {
		t.Fatalf("client B's first request: status %d, want 200 — it shares client A's bucket", got.status)
	}
}

// TestGroupsDoNotShareABucket is the other half of the same bug: a media page
// load fans out dozens of requests and must not be able to spend the activity
// feed's allowance (or vice versa).
func TestGroupsDoNotShareABucket(t *testing.T) {
	requireRedis(t)
	app := newLimiterApp(limiter(testConfig(true), burstTiers), "")
	client := "203.0.113.5"

	if got := call(t, app, fiber.MethodGet, "/v1/movies/550", client); got.status != http.StatusOK {
		t.Fatalf("first media request: status %d, want 200", got.status)
	}
	if got := call(t, app, fiber.MethodGet, "/v1/movies/551", client); got.status != http.StatusTooManyRequests {
		t.Fatalf("second media request: status %d, want 429 (media burst is 1 in this table)", got.status)
	}

	// The read surface is a different counter namespace and must be untouched.
	for i := 0; i < 3; i++ {
		if got := call(t, app, fiber.MethodGet, "/v1/users/me/activity", client); got.status != http.StatusOK {
			t.Fatalf("read request %d: status %d (body %s), want 200 — the media refusal leaked into the read allowance", i+1, got.status, got.body)
		}
	}
}

// TestDailyBudgetHasItsOwnCodeAndHeaders: the budget is the second layer, so a
// client must be able to tell "slow down for a few seconds" from "you are out
// until tomorrow" without parsing prose.
func TestDailyBudgetHasItsOwnCodeAndHeaders(t *testing.T) {
	requireRedis(t)
	app := newLimiterApp(limiter(testConfig(true), budgetTiers), "")
	client := "203.0.113.5"

	for i := 1; i <= 2; i++ {
		got := call(t, app, fiber.MethodGet, "/v1/users/me/activity", client)
		if got.status != http.StatusOK {
			t.Fatalf("request %d: status %d (body %s), want 200", i, got.status, got.body)
		}
		if limit := got.header.Get("X-UsageLimit-Limit"); limit != "2" {
			t.Errorf("request %d: X-UsageLimit-Limit = %q, want 2", i, limit)
		}
	}

	refused := call(t, app, fiber.MethodGet, "/v1/users/me/activity", client)
	if refused.status != http.StatusTooManyRequests {
		t.Fatalf("over-budget request: status %d (body %s), want 429", refused.status, refused.body)
	}
	if code := errorCode(t, refused.body); code != "usage_limit_exceeded" {
		t.Errorf("429 code = %q, want usage_limit_exceeded — the burst code here would tell the client to retry in seconds", code)
	}
	if remaining := refused.header.Get("X-UsageLimit-Remaining"); remaining != "0" {
		t.Errorf("X-UsageLimit-Remaining = %q, want 0", remaining)
	}
	if retry := refused.header.Get("Retry-After"); retry == "" || retry == "0" {
		t.Errorf("Retry-After = %q, want the seconds until the budget rolls over", retry)
	}
}

// TestSignedInTrafficIsMeteredByAccount: the identity the limiter tiers on comes
// from auth, not from the peer address, so a signed-in client is not charged to
// the bucket its proxy occupies.
func TestSignedInTrafficIsMeteredByAccount(t *testing.T) {
	client := requireRedis(t)
	app := newLimiterApp(limiter(testConfig(true), burstTiers), "account_1")

	for i := 0; i < 4; i++ {
		got := call(t, app, fiber.MethodGet, "/v1/users/me/activity", "203.0.113.5")
		if got.status != http.StatusOK {
			t.Fatalf("request %d: status %d (body %s), want 200 — the account ceiling is 5, the anonymous one is 3", i+1, got.status, got.body)
		}
	}

	keys := limiterKeys(t, client, "rate_limit:read:*")
	if len(keys) != 1 || keys[0] != "rate_limit:read:account_1" {
		t.Errorf("keys = %v, want [rate_limit:read:account_1] — signed-in traffic must not be counted against an IP", keys)
	}
}

// TestOperatorIsNeverThrottled: the instance owner (ADMIN_USER_IDS /
// MOD_USER_IDS) is exempt, the same rule TheIntroDB applies to its staff
// allowlist.
func TestOperatorIsNeverThrottled(t *testing.T) {
	client := requireRedis(t)
	auth.InitModerationAllowlists([]string{"operator_1"}, nil)
	t.Cleanup(func() { auth.InitModerationAllowlists(nil, nil) })

	app := newLimiterApp(limiter(testConfig(true), burstTiers), "operator_1")
	for i := 0; i < 20; i++ {
		got := call(t, app, fiber.MethodGet, "/v1/movies/550", "")
		if got.status != http.StatusOK {
			t.Fatalf("operator request %d: status %d (body %s), want 200", i+1, got.status, got.body)
		}
	}
	if keys := limiterKeys(t, client, "rate_limit:*"); len(keys) != 0 {
		t.Errorf("keys = %v, want none — an exempt caller must not be counted", keys)
	}
}

// TestHealthAndOptionsAreNotCounted: a probe or a CORS preflight must never
// look like abuse.
func TestHealthAndOptionsAreNotCounted(t *testing.T) {
	client := requireRedis(t)
	app := newLimiterApp(limiter(testConfig(true), burstTiers), "")

	for i := 0; i < 5; i++ {
		if got := call(t, app, fiber.MethodGet, "/healthz", "203.0.113.5"); got.status != http.StatusOK {
			t.Fatalf("healthz %d: status %d, want 200", i+1, got.status)
		}
	}
	preflight := httptest.NewRequest(fiber.MethodOptions, "/v1/movies/550", nil)
	preflight.Header.Set("X-Forwarded-For", "203.0.113.5")
	resp, err := app.Test(preflight)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if keys := limiterKeys(t, client, "rate_limit:*"); len(keys) != 0 {
		t.Errorf("keys = %v, want none — exempt paths and preflights are not abuse", keys)
	}
}

// TestLocalPeerIsExemptUnlessTheOperatorSaysOtherwise drives the exemption
// behind a real listener, so the peer really is 127.0.0.1: the operator's own
// shell, a supervisor's probe, or a dev server that forwards nothing. With
// RATE_LIMIT_EXEMPT_LOCAL=false (a deployment that terminates its traffic on
// the same box) the limiter applies to it like any other client.
func TestLocalPeerIsExemptUnlessTheOperatorSaysOtherwise(t *testing.T) {
	client := requireRedis(t)

	serve := func(cfg config.RateLimitConfig) func() string {
		app := fiber.New()
		app.Use(limiter(cfg, burstTiers))
		app.Get("/v1/movies/:id", func(c *fiber.Ctx) error { return c.SendStatus(http.StatusOK) })
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		go func() { _ = app.Listener(ln) }()
		t.Cleanup(func() { _ = app.Shutdown() })
		addr := ln.Addr().String()
		waitForPort(t, addr)
		return func() string { return "http://" + addr + "/v1/movies/550" }
	}

	local := serve(testConfig(true))
	for i := 0; i < 5; i++ {
		resp, err := http.Get(local())
		if err != nil {
			t.Fatalf("local request %d: %v", i+1, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("local request %d: status %d, want 200 — a loopback caller is exempt by default", i+1, resp.StatusCode)
		}
	}
	if keys := limiterKeys(t, client, "rate_limit:*"); len(keys) != 0 {
		t.Errorf("keys = %v, want none — the exempt caller was counted", keys)
	}

	limited := serve(testConfig(false))
	url := limited()
	statuses := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("limited request %d: %v", i+1, err)
		}
		_ = resp.Body.Close()
		statuses = append(statuses, resp.StatusCode)
	}
	if want := []int{200, 429, 429}; !equalInts(statuses, want) {
		t.Errorf("RATE_LIMIT_EXEMPT_LOCAL=false statuses = %v, want %v (media burst is 1)", statuses, want)
	}
}

// waitForPort blocks until something accepts a TCP connection on addr. It
// deliberately does not send an HTTP request: against a live limiter that
// request would itself be counted, and the first assertion would then be
// measuring the probe instead of the client.
func waitForPort(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("nothing listening on %s", addr)
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
