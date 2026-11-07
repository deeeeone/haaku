package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/haaku-tv/haaku/redis"

	goredis "github.com/redis/go-redis/v9"

	"github.com/gofiber/fiber/v2"
)

// rateLimitSkipPaths are paths excluded from rate limiting.
var rateLimitSkipPaths = map[string]bool{
	"/healthz": true,
	"/readyz":  true,
	"/metrics": true,
}

// RateLimit returns a Fiber middleware for sliding-window rate limiting using Redis sorted sets.
// It auto-detects authentication from c.Locals("user_id"): authenticated users get
// the auth tier limits; unauthenticated get the public tier.
//
// Config via env vars:
//
//	RATE_LIMIT_ENABLED=true           # enable rate limiting (default: on)
//	RATE_LIMIT_AUTH_WINDOW=60         # window in seconds for authenticated users
//	RATE_LIMIT_AUTH_MAX=100           # max requests per window for authenticated users
//	RATE_LIMIT_PUBLIC_WINDOW=60       # window in seconds for public users
//	RATE_LIMIT_PUBLIC_MAX=30          # max requests per window for public users
//
// Algorithm: sliding window via Redis ZSET (ZADD + ZREMRANGEBYSCORE + ZCARD).
// Fails OPEN: if Redis is unavailable, requests pass through unthrottled.
func RateLimit() fiber.Handler {
	enabled := rlGetEnvBool("RATE_LIMIT_ENABLED", true)
	if !enabled {
		return func(c *fiber.Ctx) error { return c.Next() }
	}

	authWindow := time.Duration(rlGetEnvInt("RATE_LIMIT_AUTH_WINDOW", 60)) * time.Second
	authMax := int64(rlGetEnvInt("RATE_LIMIT_AUTH_MAX", 100))
	publicWindow := time.Duration(rlGetEnvInt("RATE_LIMIT_PUBLIC_WINDOW", 60)) * time.Second
	publicMax := int64(rlGetEnvInt("RATE_LIMIT_PUBLIC_MAX", 30))

	slog.Info("rate limiter initialised",
		"auth_window", authWindow,
		"auth_max", authMax,
		"public_window", publicWindow,
		"public_max", publicMax,
	)

	return func(c *fiber.Ctx) error {
		// Skip rate limiting on health, readiness, and metrics endpoints.
		if rateLimitSkipPaths[c.Path()] {
			return c.Next()
		}

		rdb := redis.GetClient()
		if rdb == nil {
			// Fail OPEN: Redis unavailable, let traffic through.
			return c.Next()
		}

		// Determine tier based on authentication.
		var window time.Duration
		var maxReq int64
		var identifier string

		userID, ok := c.Locals("user_id").(string)
		if ok && userID != "" {
			// Authenticated tier.
			window = authWindow
			maxReq = authMax
			identifier = userID
		} else {
			// Public tier — use IP address.
			window = publicWindow
			maxReq = publicMax
			identifier = c.IP()
		}

		// Build the rate limit key: ratelimit:{identifier}:{group}
		group := "public"
		if ok && userID != "" {
			group = "auth"
		}
		key := fmt.Sprintf("ratelimit:%s:%s", identifier, group)

		now := time.Now()
		nowMs := float64(now.UnixMilli())
		windowStart := float64(now.Add(-window).UnixMilli())

		// Unique member: nanosecond timestamp + counter to prevent collisions.
		member := fmt.Sprintf("%d:%d", now.UnixNano(), now.UnixNano()%100000)

		ctx, cancel := context.WithTimeout(c.Context(), 100*time.Millisecond)
		defer cancel()

		pipe := rdb.Pipeline()

		// Add current request to sorted set.
		pipe.ZAdd(ctx, key, goredis.Z{Score: nowMs, Member: member})

		// Remove entries outside the window.
		pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%.0f", windowStart))

		// Count remaining entries in the window.
		countCmd := pipe.ZCard(ctx, key)

		// Set TTL on the key so zombie keys expire.
		pipe.Expire(ctx, key, window)

		if _, err := pipe.Exec(ctx); err != nil {
			slog.Warn("rate limiter redis pipeline failed — failing open",
				"path", c.Path(),
				"error", err,
			)
			// Fail OPEN: Redis error, allow the request.
			return c.Next()
		}

		count, err := countCmd.Result()
		if err != nil {
			slog.Warn("rate limiter ZCARD failed — failing open",
				"path", c.Path(),
				"error", err,
			)
			return c.Next()
		}

		if count > maxReq {
			retryAfter := int(window.Seconds())
			c.Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			IncRateLimited()
			slog.Warn("rate limited",
				"identifier", identifier,
				"group", group,
				"count", count,
				"max", maxReq,
				"path", c.Path(),
			)
			return writeError(c, fiber.StatusTooManyRequests, "rate_limited",
				"too many requests — slow down")
		}

		return c.Next()
	}
}

// ---------------------------------------------------------------------------
// Private helpers — avoid import cycle with config package
// ---------------------------------------------------------------------------

func rlGetEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		switch v {
		case "true", "1", "yes", "TRUE", "YES":
			return true
		case "false", "0", "no", "FALSE", "NO":
			return false
		}
	}
	return fallback
}

func rlGetEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return fallback
}
