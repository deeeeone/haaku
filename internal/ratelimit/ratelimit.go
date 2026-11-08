package ratelimit

import (
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/config"
	"github.com/haaku-tv/haaku/internal/obs"
	"github.com/haaku-tv/haaku/internal/response"
	projredis "github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
)

// DefaultWindow is the burst window when configuration does not set one.
const DefaultWindow = time.Minute

// Response headers. One set per layer, both present on every limited response,
// so a client can see how much of each allowance it has left before it is
// refused rather than after.
const (
	headerRateLimitLimit      = "X-RateLimit-Limit"
	headerRateLimitRemaining  = "X-RateLimit-Remaining"
	headerRateLimitReset      = "X-RateLimit-Reset"
	headerUsageLimitLimit     = "X-UsageLimit-Limit"
	headerUsageLimitRemaining = "X-UsageLimit-Remaining"
	headerUsageLimitReset     = "X-UsageLimit-Reset"
)

// Error codes. Distinct on purpose: "slow down for a few seconds" and "you are
// out of budget until tomorrow" need different client behaviour, and the status
// is 429 for both.
const (
	codeRateLimitExceeded  = "rate_limit_exceeded"
	codeUsageLimitExceeded = "usage_limit_exceeded"
)

// Middleware limits every request by group (see groups.go): a burst ceiling over
// a sliding window, then a daily budget. Both counters are per identity —
// account when the request carries a valid credential, client IP otherwise.
//
// It must be installed AFTER the request logger (so a refusal is logged) and it
// resolves identity itself (auth.AttemptAuth), so it does not depend on running
// after any particular route's auth middleware. In haaku the limiter is one
// global middleware, not per-route: the group is derived from method and path.
//
// Failure mode: Redis being unavailable makes both layers FAIL OPEN, and the
// request is logged at WARN. A limiter that fails closed turns a Redis blip into
// an outage of the whole API — on a self-hosted box that is a worse failure than
// the abuse it prevents. TheIntroDB fails closed instead; see ratelimit.md.
func Middleware(cfg config.RateLimitConfig) fiber.Handler {
	return limiter(cfg, groupTiers)
}

// limiter is Middleware over an explicit ceiling table. Tests build one with
// tiny numbers so the budget layer can be exercised without spending 50,000
// requests; production always passes groupTiers.
func limiter(cfg config.RateLimitConfig, tiers map[string]tier) fiber.Handler {
	if !cfg.Enabled {
		return func(c *fiber.Ctx) error { return c.Next() }
	}

	window := cfg.Window
	if window <= 0 {
		window = DefaultWindow
	}
	slog.Info("rate limiter initialised",
		"window", window.String(),
		"groups", len(groupTiers),
		"trusted_proxies", len(cfg.TrustedProxyNets),
		"exempt_local", cfg.ExemptLocal,
	)

	return func(c *fiber.Ctx) error {
		if c.Method() == fiber.MethodOptions || exemptPath(c.Path()) {
			return c.Next()
		}

		// Resolve identity first: the tier (and therefore which ceiling and
		// which counters apply) depends on it. AttemptAuth verifies the
		// Authorization header only when it has to, and records the identity, so
		// the route's own AuthRequired does not verify a second time.
		userID, authenticated := auth.AttemptAuth(c)

		// The operator is never throttled on their own instance — the same rule
		// TheIntroDB applies to its admin/mod env allowlist. In-memory, so it
		// costs nothing on the hot path.
		if authenticated && auth.IsModerationAllowlisted(userID) {
			return c.Next()
		}

		identity := ClientIP(c, cfg.TrustedProxyNets)
		if identity.Local && cfg.ExemptLocal {
			return c.Next()
		}

		group := GroupFor(c.Method(), c.Path())
		ceilings := ceilingFor(tiers, group)

		identifier, burstMax, dailyMax := identity.IP, ceilings.BurstPublic, ceilings.DailyPublic
		if authenticated {
			identifier, burstMax, dailyMax = userID, ceilings.BurstAuth, ceilings.DailyAuth
		}
		if identifier == "" {
			// Nothing to count against. Bucketing every such request together
			// would be worse than not counting them.
			return c.Next()
		}
		if burstMax <= 0 && dailyMax <= 0 {
			return c.Next()
		}

		now := time.Now()

		if burstMax > 0 {
			burst, err := projredis.SlidingWindow(projredis.RateLimitKey(group, identifier), window, int64(burstMax), now)
			if err != nil {
				slog.Warn("rate limiter unavailable — failing open",
					"layer", "burst", "group", group, "error", err)
				return c.Next()
			}
			c.Set(headerRateLimitLimit, strconv.Itoa(burstMax))
			c.Set(headerRateLimitRemaining, strconv.FormatInt(burst.Remaining, 10))
			c.Set(headerRateLimitReset, seconds(burst.Reset))

			if burst.Count > int64(burstMax) {
				obs.IncRateLimited()
				slog.Warn("rate limited",
					"layer", "burst",
					"identifier", identifier,
					"group", group,
					"count", burst.Count,
					"max", burstMax,
					"path", c.Path(),
				)
				c.Set(fiber.HeaderRetryAfter, seconds(burst.Reset))
				return response.WriteError(c, fiber.StatusTooManyRequests, codeRateLimitExceeded,
					"too many requests — slow down")
			}
		}

		if dailyMax <= 0 {
			return c.Next()
		}

		results, err := projredis.UsageCheckMany([]projredis.UsageCheckRequest{{
			Key:   projredis.UsageKey(group, identifier, now),
			Limit: int64(dailyMax),
		}}, now)
		if err != nil || len(results) == 0 {
			slog.Warn("usage limiter unavailable — failing open",
				"layer", "budget", "group", group, "error", err)
			return c.Next()
		}

		budget := results[0]
		c.Set(headerUsageLimitLimit, strconv.Itoa(dailyMax))
		c.Set(headerUsageLimitRemaining, strconv.FormatInt(budget.Remaining, 10))
		c.Set(headerUsageLimitReset, seconds(budget.Reset))

		if !budget.Allowed {
			obs.IncRateLimited()
			slog.Warn("rate limited",
				"layer", "budget",
				"identifier", identifier,
				"group", group,
				"count", budget.Count,
				"max", dailyMax,
				"path", c.Path(),
			)
			c.Set(fiber.HeaderRetryAfter, seconds(budget.Reset))
			return response.WriteError(c, fiber.StatusTooManyRequests, codeUsageLimitExceeded,
				"daily request budget for this endpoint group is exhausted — try again tomorrow")
		}

		return c.Next()
	}
}

// seconds renders a duration for Retry-After and the reset headers, which are
// delta-seconds (RFC 9110). Never zero: a client that reads 0 retries
// immediately, which is the behaviour the header exists to prevent.
func seconds(d time.Duration) string {
	s := int64(math.Ceil(d.Seconds()))
	if s < 1 {
		s = 1
	}
	return strconv.FormatInt(s, 10)
}
