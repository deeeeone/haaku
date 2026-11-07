package ratelimit

import (
	"testing"

	"github.com/gofiber/fiber/v2"
)

// TestGroupForClassifiesBySurface pins the classification of the paths that
// matter. The two properties being defended: a read never lands in the same
// counter as a write, and the metadata surface (search especially) never shares
// an allowance with the browsing surface — before this package one bucket
// served every path, so a media page load could exhaust the activity feed.
func TestGroupForClassifiesBySurface(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		{"profile read", fiber.MethodGet, "/v1/users/me/activity", GroupRead},
		{"stats read", fiber.MethodGet, "/v1/users/1/stats", GroupRead},
		{"media page", fiber.MethodGet, "/v1/movies/550", GroupMedia},
		{"show page", fiber.MethodGet, "/v1/shows/1399/seasons", GroupMedia},
		{"search", fiber.MethodGet, "/v1/search", GroupMedia},
		{"reference data", fiber.MethodGet, "/v1/genres", GroupMedia},
		{"calendar", fiber.MethodGet, "/v1/calendars/my/shows", GroupMedia},
		{"sync push", fiber.MethodPost, "/v1/sync/history", GroupSync},
		{"sync session", fiber.MethodPost, "/v1/syncs", GroupSync},
		{"scrobble", fiber.MethodPost, "/v1/scrobble/start", GroupSync},
		{"comment write", fiber.MethodPost, "/v1/comments", GroupWrite},
		{"list write", fiber.MethodPut, "/v1/lists/1", GroupWrite},
		{"follow write", fiber.MethodPost, "/v1/users/1/follow", GroupWrite},
		{"login", fiber.MethodPost, "/v1/auth/login", GroupAuth},
		{"oauth token", fiber.MethodPost, "/v1/oauth/token", GroupAuth},
		{"clerk webhook", fiber.MethodPost, "/v1/webhooks/clerk", GroupAuth},
		{"admin panel", fiber.MethodGet, "/v1/admin/users", GroupAdmin},
		{"health is unclassified, not a group", fiber.MethodGet, "/healthz", GroupRead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GroupFor(tt.method, tt.path); got != tt.want {
				t.Errorf("GroupFor(%s %s) = %q, want %q", tt.method, tt.path, got, tt.want)
			}
		})
	}
}

// TestGroupForUnknownPathIsSafeDirection pins the fallback: a route added
// without a group is limited as a read (GET/HEAD) or as a write, never skipped.
func TestGroupForUnknownPathIsSafeDirection(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   string
	}{
		{fiber.MethodGet, "/v1/brand-new-thing", GroupRead},
		{fiber.MethodHead, "/v1/brand-new-thing", GroupRead},
		{fiber.MethodPost, "/v1/brand-new-thing", GroupWrite},
		{fiber.MethodDelete, "/v1/brand-new-thing", GroupWrite},
		{fiber.MethodPatch, "/", GroupWrite},
	}
	for _, tt := range tests {
		if got := GroupFor(tt.method, tt.path); got != tt.want {
			t.Errorf("GroupFor(%s %s) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestFirstSegment(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/v1/sync/collection", "sync"},
		{"/v1/movies", "movies"},
		{"/v1", ""},
		{"/healthz", "healthz"},
		{"/", ""},
	}
	for _, tt := range tests {
		if got := firstSegment(tt.path); got != tt.want {
			t.Errorf("firstSegment(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// TestTierForUnknownGroupIsStrictestAndBudgetless: a group with no row must be
// over-limited, not unlimited, and must carry no daily budget (0 means "no
// budget for this tier").
func TestTierForUnknownGroupIsStrictestAndBudgetless(t *testing.T) {
	got := TierFor("no-such-group")
	if got.BurstPublic <= 0 || got.BurstAuth <= 0 {
		t.Fatalf("unknown group burst = %d/%d, want both positive", got.BurstPublic, got.BurstAuth)
	}
	if got.DailyPublic != 0 || got.DailyAuth != 0 {
		t.Errorf("unknown group daily = %d/%d, want 0/0", got.DailyPublic, got.DailyAuth)
	}
}

// TestCeilingForFallsBackToTheRealTable keeps the test seam honest: a group
// absent from the table under test still gets the production ceilings.
func TestCeilingForFallsBackToTheRealTable(t *testing.T) {
	tiers := map[string]tier{GroupRead: {BurstPublic: 3}}
	if got := ceilingFor(tiers, GroupRead); got.BurstPublic != 3 {
		t.Errorf("tabled group = %+v, want the table's row", got)
	}
	if got, want := ceilingFor(tiers, GroupMedia), TierFor(GroupMedia); got != want {
		t.Errorf("untabled group = %+v, want the production row %+v", got, want)
	}
}

// TestEveryGroupIsTieredAndSignedInTrafficNeverGetsLess closes the table's own
// hole: a group reachable by a prefix but missing from groupTiers would silently
// fall back to the strictest ceilings for signed-in callers too, and a tier
// where the anonymous ceiling is the higher one would meter logged-out traffic
// more generously than an account.
func TestEveryGroupIsTieredAndSignedInTrafficNeverGetsLess(t *testing.T) {
	for _, entry := range groupPrefixes {
		if _, ok := groupTiers[entry.group]; !ok {
			t.Errorf("group %q is reachable by prefix but has no row in groupTiers", entry.group)
		}
	}
	for name, tiers := range groupTiers {
		if tiers.BurstAuth < tiers.BurstPublic {
			t.Errorf("group %q: BurstAuth %d < BurstPublic %d — a signed-in caller is throttled harder than an anonymous one",
				name, tiers.BurstAuth, tiers.BurstPublic)
		}
		if tiers.DailyPublic > 0 && tiers.DailyAuth > 0 && tiers.DailyAuth < tiers.DailyPublic {
			t.Errorf("group %q: DailyAuth %d < DailyPublic %d", name, tiers.DailyAuth, tiers.DailyPublic)
		}
	}
}

func TestExemptPath(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz", "/metrics", "/v1/openapi.json"} {
		if !exemptPath(path) {
			t.Errorf("exemptPath(%q) = false, want true — a probe must never look down because the box is busy", path)
		}
	}
	// /v1/auth/config is deliberately NOT exempt: mode discovery is cheap, but a
	// client reading it in a loop is misbehaving.
	for _, path := range []string{"/v1/auth/config", "/v1/users/me/activity"} {
		if exemptPath(path) {
			t.Errorf("exemptPath(%q) = true, want false", path)
		}
	}
}
