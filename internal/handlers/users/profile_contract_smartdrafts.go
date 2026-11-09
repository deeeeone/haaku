package users

import (
	"errors"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Smart-list drafts (issue #103).
//
// /users/{id}/smart-list-drafts is a summary of a user's stale personal lists
// that Trakt can convert into smart lists, carrying a per-list `confidence`.
// haaku stores smart-list filters (smart_lists.filters) and personal lists, but
// it stores neither a list's `stale_origin` nor a computed draft `confidence`,
// so there is nothing real to summarise as convertible. The contract defines the
// empty state explicitly: convertible_count 0, best_confidence null, lists [].
// That is what this endpoint serves — an honest zero state, not a fabricated
// non-empty draft list.
//
// The contract says only the owner may read it, so the handler authenticates and
// requires the caller to be the target. The row stays `divergent` in the parity
// manifest with a rationale naming the subset (empty state only).

func init() {
	openapi.Register(openapi.RouteDef{
		Method:      "GET",
		Path:        "/v1/users/{id}/smart-list-drafts",
		Summary:     "Lists ready to become smart lists (contract path; empty state)",
		Tags:        []string{"Users"},
		Auth:        "required",
		StatusCodes: map[int]string{200: "Smart list drafts", 401: "Unauthorized", 403: "Not the owner", 404: "Not found"},
	})
}

// registerContractSmartListDraftRoutes mounts the owner-only drafts read. It is
// called from registerContractCollectionReads, before the generic two-segment
// /:id/<section> routes, so the static "smart-list-drafts" segment is not
// shadowed by /:id/history and friends.
func registerContractSmartListDraftRoutes(users fiber.Router) {
	users.Get("/:id/smart-list-drafts", auth.AuthRequired(), handleContractSmartListDrafts)
}

// handleContractSmartListDrafts serves GET /v1/users/{id}/smart-list-drafts.
func handleContractSmartListDrafts(c *fiber.Ctx) error {
	caller, _ := c.Locals("user_id").(string)
	if caller == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	targetID := auth.ResolveTargetID(c, "id")
	if targetID == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	if targetID != caller {
		return response.WriteError(c, fiber.StatusForbidden, "forbidden", "only the owner can read smart list drafts")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}
	var user database.User
	if err := db.First(&user, "id = ?", targetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "user not found")
		}
		return response.InternalError(c)
	}

	return c.JSON(map[string]any{
		"convertible_count": 0,
		"best_confidence":   nil,
		"lists":             []map[string]any{},
	})
}
