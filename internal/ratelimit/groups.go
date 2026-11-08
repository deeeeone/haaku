// Package ratelimit is haaku's anti-abuse limiter: a per-group burst ceiling
// (Redis sorted set, sliding window) plus a per-group daily request budget
// (Redis counter, reset at the instance's local midnight).
//
// The shape is TheIntroDB's (the owner's other service): every limited surface
// gets its OWN group with its OWN counters, signed-in traffic gets its own
// ceiling and its own keyspace, the operator's account is exempt, and the 429 it
// writes names which ceiling was hit. The mechanism differs where haaku's
// conventions demand it — see ratelimit.md ("Differences from TheIntroDB").
//
// Nothing here caps DATA: history, lists, watchlist and collection are unbounded
// by charter (AGENTS.md §7). These limits cap REQUEST VOLUME, which is what
// abuse looks like from the outside.
package ratelimit

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

// Groups. A group is a counter namespace, not a route group: two surfaces in
// the same group share one allowance, and two surfaces in different groups can
// never spend each other's. That separation is the point — before this package
// one bucket served every path, so a media page load (dozens of requests) could
// exhaust the allowance for the activity feed, and a client hitting one
// endpoint could throttle an unrelated one.
const (
	// GroupRead is the catch-all for reads: profiles, stats, activity,
	// notifications, lists, comments. The steady state of the web UI.
	GroupRead = "read"
	// GroupMedia is the metadata surface: movies, shows, seasons, episodes,
	// people, search, calendars, updates, watch providers. Search is the most
	// expensive query in the API (issue #10 §5), so it must not be able to spend
	// the profile-read allowance.
	GroupMedia = "media"
	// GroupSync is the client-driven write/read firehose: /v1/sync/* and
	// /v1/scrobble/*. A library scan is the one workload that legitimately
	// spends thousands of calls back to back, and throttling it at browsing pace
	// would make the Jellyfin plugin unusable.
	GroupSync = "sync"
	// GroupWrite is every other mutating request: comments, lists, ratings
	// pushed outside /sync, follows, webhooks, reports. Comment and follow spam
	// are the abuse cases issue #10 §5 names.
	GroupWrite = "write"
	// GroupAuth is the identity surface: login, signup, logout, password reset,
	// oauth, received webhooks. Login has its own, stricter limiter
	// (internal/auth, keyed on IP and on a hash of the address); this group only
	// stops a loop.
	GroupAuth = "auth"
	// GroupAdmin is the operator surface (the local-mode admin panel).
	GroupAdmin = "admin"
)

// tier is one group's ceilings.
//
// Burst is per window (config.RateLimitWindow, default 60s). Auth is the
// signed-in tier, Public the anonymous one, which is keyed on the client IP —
// see identity.go for how that IP is resolved behind a proxy.
//
// Daily is the 24h request budget. 0 means "no budget for this tier", which is
// how a surface that requires authentication says so: the burst ceiling still
// applies, and the route answers 401 anyway.
//
// The numbers are chosen so a human never meets them and a runaway client meets
// them the same day. They are deliberately NOT env-tunable one-by-one: they are
// a table with one shared window knob (RATE_LIMIT_WINDOW), because sixteen
// independent numbers are sixteen ways to misconfigure an instance. Changing a
// number here is a reviewed change with a reason in the diff.
type tier struct {
	BurstPublic int
	BurstAuth   int
	DailyPublic int
	DailyAuth   int
}

var groupTiers = map[string]tier{
	// 60/min anonymous ≈ one page load per second; 300/min signed in ≈ five
	// requests a second sustained, which no interactive UI reaches.
	// 50k/day signed in: a human never approaches it, a loop hits it by lunchtime.
	GroupRead:  {BurstPublic: 60, BurstAuth: 300, DailyPublic: 2_000, DailyAuth: 50_000},
	GroupMedia: {BurstPublic: 60, BurstAuth: 300, DailyPublic: 2_000, DailyAuth: 50_000},
	// 1200/min = 20 requests a second for a scan; 250k/day covers a full rescan
	// of a six-figure library plus normal scrobbling.
	GroupSync: {BurstPublic: 30, BurstAuth: 1_200, DailyAuth: 250_000},
	// 120/min signed in is far above hand-driven interaction (one request every
	// couple of seconds at worst) and far below what spam looks like.
	GroupWrite: {BurstPublic: 30, BurstAuth: 120, DailyAuth: 10_000},
	// A person logs in a handful of times a day; the auth group's job is only to
	// stop a loop, not to be the login limiter.
	GroupAuth:  {BurstPublic: 30, BurstAuth: 60, DailyAuth: 1_000},
	GroupAdmin: {BurstPublic: 30, BurstAuth: 300, DailyAuth: 20_000},
}

// TierFor returns the ceilings for a group. An unknown group gets the strictest
// public ceilings and no budget rather than panicking: a new route added
// without a group should be over-limited, never unthrottled.
func TierFor(group string) tier {
	if t, ok := groupTiers[group]; ok {
		return t
	}
	return tier{BurstPublic: 30, BurstAuth: 300}
}

// ceilingFor is TierFor over the table a middleware instance was built with, so
// a test can install tiny ceilings without touching the real ones.
func ceilingFor(tiers map[string]tier, group string) tier {
	if t, ok := tiers[group]; ok {
		return t
	}
	return TierFor(group)
}

// groupPrefixes maps a first path segment (with the /v1 prefix already removed)
// to its group. Order matters only in that the first match wins, and no two
// entries overlap.
var groupPrefixes = []struct {
	group    string
	segments []string
}{
	{GroupSync, []string{"sync", "syncs", "scrobble"}},
	{GroupAdmin, []string{"admin"}},
	{GroupAuth, []string{"auth", "oauth", "webhooks"}},
	{GroupMedia, []string{
		"movies", "shows", "seasons", "episodes", "people",
		"search", "calendars", "updates", "reference", "watch",
		"media", "networks", "languages", "genres", "countries",
		"certifications", "recommendations",
	}},
}

// GroupFor classifies a request. Reads that no prefix claims land in GroupRead;
// anything else (a mutating method) lands in GroupWrite. Unclassified writes are
// therefore limited as writes, which is the safe direction.
func GroupFor(method, path string) string {
	first := firstSegment(path)
	for _, entry := range groupPrefixes {
		for _, segment := range entry.segments {
			if first == segment {
				return entry.group
			}
		}
	}
	if method == fiber.MethodGet || method == fiber.MethodHead {
		return GroupRead
	}
	return GroupWrite
}

// firstSegment returns the first path segment of a request path, ignoring the
// API version prefix. "/v1/sync/collection" -> "sync"; "/healthz" -> "healthz".
func firstSegment(path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	trimmed = strings.TrimPrefix(trimmed, "v1/")
	if trimmed == "v1" {
		return ""
	}
	if i := strings.IndexByte(trimmed, '/'); i >= 0 {
		return trimmed[:i]
	}
	return trimmed
}

// exemptPaths are never limited. Health and readiness are polled by the
// container's supervisor and by whoever watches the box; refusing them makes an
// instance look down precisely when it is busy. /v1/openapi.json and /v1/config
// are cheap, static, and read by tools on every start.
//
// Note /v1/auth/config (mode discovery) is NOT here: it lives in GroupAuth,
// which is loose, and a client that reads it in a loop is misbehaving.
var exemptPaths = map[string]bool{
	"/healthz":         true,
	"/readyz":          true,
	"/metrics":         true,
	"/v1/openapi.json": true,
}

// exemptPath reports whether path skips the limiter entirely.
func exemptPath(path string) bool {
	return exemptPaths[path]
}
