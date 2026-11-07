package auth

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/openapi"
	projredis "github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
)

// The mode switch (issue #53 §1). "clerk" has to genuinely unregister the local
// module — no handler, no RouteDef — and local session tokens must stop being a
// credential. The route registry is the machine-checkable half of that claim,
// which is why the generated openapi.yaml shows it too.

// localRouteKeys is every route the local module owns.
func localRouteKeys() []openapi.RouteKey {
	return []openapi.RouteKey{
		{Method: "POST", Path: "/v1/auth/local/signup"},
		{Method: "POST", Path: "/v1/auth/local/login"},
		{Method: "POST", Path: "/v1/auth/local/logout"},
		{Method: "POST", Path: "/v1/auth/local/password-reset"},
		{Method: "GET", Path: "/v1/admin/users"},
		{Method: "POST", Path: "/v1/admin/users/{id}/ban"},
		{Method: "POST", Path: "/v1/admin/users/{id}/unlock"},
		{Method: "POST", Path: "/v1/admin/users/{id}/approve"},
		{Method: "POST", Path: "/v1/admin/users/{id}/password-reset"},
	}
}

func TestClerkModeRegistersNoLocalRoutes(t *testing.T) {
	t.Setenv("HAAKU_SECRET_KEY", testRootKey)
	// This package's test binary builds no other routes before this point, so
	// clearing the registry here is safe and makes the inventory assertion
	// meaningful: whatever is in it was registered by this test.
	openapi.Reset()
	t.Cleanup(func() {
		openapi.Reset()
		if err := InitAuthMode("", ""); err != nil {
			t.Fatalf("restore auth mode: %v", err)
		}
	})

	if err := InitAuthMode("clerk", "approval"); err != nil {
		t.Fatalf("switch to clerk: %v", err)
	}
	app := fiber.New()
	api := app.Group("/v1")
	RegisterLocalAuthConfig(api)
	RegisterLocalAuthRoutes(api)
	RegisterLocalAuthRoutes(api) // idempotent: no duplicate registrations

	inv := openapi.RegistryInventory()
	for _, key := range localRouteKeys() {
		if inv[key] {
			t.Errorf("clerk mode still registers %s", key)
		}
	}
	for key := range inv {
		if strings.HasPrefix(key.Path, "/v1/auth/local") || strings.HasPrefix(key.Path, "/v1/admin") {
			t.Errorf("clerk mode registered a local route: %s", key)
		}
	}
	// Mode discovery survives clerk mode, or a client cannot tell which
	// authenticator to use.
	if !inv[openapi.RouteKey{Method: "GET", Path: "/v1/auth/config"}] {
		t.Error("/v1/auth/config is not registered in clerk mode")
	}

	// The same call in local mode registers all of them, so the assertion above
	// is measuring the switch and not an empty registry.
	if err := InitAuthMode("local", "approval"); err != nil {
		t.Fatalf("switch to local: %v", err)
	}
	RegisterLocalAuthRoutes(api)
	inv = openapi.RegistryInventory()
	for _, key := range localRouteKeys() {
		if !inv[key] {
			t.Errorf("local mode did not register %s", key)
		}
	}
}

func TestClerkModeDoesNotAuthenticateSessionTokens(t *testing.T) {
	_, localApp := setupLocalAuth(t)
	token := signupToken(t, localApp, "clerk-switch@example.com")
	if token == "" {
		t.Fatal("no session token issued")
	}

	if err := InitAuthMode("clerk", "approval"); err != nil {
		t.Fatalf("switch to clerk: %v", err)
	}
	t.Cleanup(func() { _ = InitAuthMode("", "") })

	probe := fiber.New()
	probe.Get("/v1/probe", AuthRequired(), func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"user_id": c.Locals("user_id")})
	})
	if r := call(t, probe, "GET", "/v1/probe", token, ""); r.status != 401 {
		t.Fatalf("clerk mode accepted a local session token: %d (%s)", r.status, r.raw)
	}
}

// TestLoginRateLimitIsEnforced needs a live Redis, because the limiter is the
// repo's Redis sliding window and it fails OPEN by design. It skips (rather
// than fails) when REDIS_URL is not set, so the standard unit run stays
// hermetic; the PR body shows it run against a real redis-server.
func TestLoginRateLimitIsEnforced(t *testing.T) {
	if os.Getenv("REDIS_URL") == "" {
		t.Skip("REDIS_URL not set — the login limiter needs a live Redis to enforce anything")
	}
	if err := projredis.InitRedis(); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	t.Cleanup(func() { _ = projredis.CloseRedis() })

	_, a := setupLocalAuth(t)
	email := fmt.Sprintf("ratelimit-%d@example.com", time.Now().UnixNano())

	var got429 bool
	for i := 0; i < int(loginRateMaxPerEmail)+2; i++ {
		r := login(t, a, email, "wrong-password-here")
		if r.status == 429 {
			if code := errorCode(t, r); code != "rate_limited" {
				t.Fatalf("429 code = %q, want rate_limited", code)
			}
			got429 = true
			break
		}
		if r.status != 401 {
			t.Fatalf("attempt %d = %d (%s), want 401 before the limit", i+1, r.status, r.raw)
		}
	}
	if !got429 {
		t.Fatalf("no 429 after %d attempts — the login limiter is not counting", loginRateMaxPerEmail+2)
	}
}

// Idempotence guard for the token derivation: the same raw token always maps to
// the same blind index, and two different tokens never collide.
func TestLocalTokenHashIsDeterministic(t *testing.T) {
	t.Setenv("HAAKU_SECRET_KEY", testRootKey)
	a, err := localTokenHash("hs_one")
	if err != nil {
		t.Fatal(err)
	}
	b, err := localTokenHash("hs_one")
	if err != nil {
		t.Fatal(err)
	}
	c, err := localTokenHash("hs_two")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("the same token produced two different hashes")
	}
	if a == c || len(a) != 64 {
		t.Fatalf("hash %q / %q — want distinct 64-char hex", a, c)
	}
}
