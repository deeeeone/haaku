package httpapi

import (
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
)

// readinessBody is the JSON /readyz answers with.
//
// The shape is additive: existing clients read "status" (and "redis"), and the
// migration block is new, so nothing already polling /readyz has to change. The
// fields are a struct rather than a fiber.Map so the response cannot silently
// lose a key and the encoding is pinned by tests.
type readinessBody struct {
	Status    string                  `json:"status"`
	Reason    string                  `json:"reason,omitempty"`
	Redis     string                  `json:"redis,omitempty"`
	Migration database.MigrationState `json:"migration"`
}

// handleReady is the /readyz handler: readiness, not liveness. /healthz answers
// "the process is up"; /readyz answers "this process can serve against a schema
// it understands".
//
// It is deliberately hard to get a 200 out of it that lies:
//   - no database handle, or an unreachable one, is 503 "degraded";
//   - a reachable database whose Goose ledger does not match the migrations this
//     binary ships is 503 — "behind", "ahead" or "migrating" — never a 200;
//   - only a ledger that matches exactly (newest applied == newest shipped, one
//     applied row per file) is 200 "ready".
//
// Redis stays soft: it is reported as "unavailable" on an otherwise-ready
// service rather than failing readiness, because the API degrades gracefully
// without it.
func handleReady(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil || db == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status": "degraded", "reason": "database unavailable",
		})
	}

	state, err := database.ReadMigrationState(c.UserContext(), db)
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status": "degraded", "reason": "database unreachable",
		})
	}

	code, status, reason := readinessResult(state)
	if code != fiber.StatusOK {
		return c.Status(code).JSON(readinessBody{
			Status: status, Reason: reason, Migration: state,
		})
	}

	body := readinessBody{Status: status, Migration: state}
	if redis.GetClient() == nil {
		body.Redis = "unavailable"
	}
	return c.Status(code).JSON(body)
}

// readinessResult maps a migration state to the /readyz status code, status
// word and (optional) reason. Extracted so the mapping — including the
// "migrating" branch, which needs a real advisory lock to reach end to end — is
// unit-testable without a database.
func readinessResult(state database.MigrationState) (code int, status, reason string) {
	switch {
	case state.Migrating:
		return fiber.StatusServiceUnavailable, "migrating", ""
	case state.UpToDate:
		return fiber.StatusOK, "ready", ""
	case state.Version > state.Newest:
		return fiber.StatusServiceUnavailable, "degraded", "database schema is ahead of this binary"
	default:
		return fiber.StatusServiceUnavailable, "degraded", "database schema is behind"
	}
}
