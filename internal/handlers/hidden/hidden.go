package hidden

import (
	"errors"
	"fmt"
	"time"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/gorm"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{
			Method:  "GET",
			Path:    "/v1/users/hidden/{section}",
			Summary: "List hidden items for a section",
			Tags:    []string{"Users"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Hidden items list",
				401: "Unauthorized",
			},
		},
		{
			Method:  "POST",
			Path:    "/v1/users/hidden/{section}",
			Summary: "Add items to hidden section",
			Tags:    []string{"Users"},
			Auth:    "required",
			StatusCodes: map[int]string{
				201: "Items hidden",
				400: "Invalid request",
				401: "Unauthorized",
			},
		},
		{
			Method:  "DELETE",
			Path:    "/v1/users/hidden/{section}",
			Summary: "Remove items from hidden section",
			Tags:    []string{"Users"},
			Auth:    "required",
			StatusCodes: map[int]string{
				204: "Items removed from hidden",
				400: "Invalid request",
				401: "Unauthorized",
			},
		},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires hidden item routes to the Fiber router.
func RegisterRoutes(api fiber.Router) {
	hidden := api.Group("/users/hidden")
	hidden.Get("/:section", auth.AuthRequired(), handleListHidden)
	hidden.Post("/:section", auth.AuthRequired(), handleAddHidden)
	hidden.Delete("/:section", auth.AuthRequired(), handleRemoveHidden)
}

// ---------------------------------------------------------------------------
// Valid sections
// ---------------------------------------------------------------------------

var validSections = map[string]bool{
	"calendar":           true,
	"progress_watched":   true,
	"progress_collected": true,
	"recommendations":    true,
	"comments":           true,
	"dropped":            true,
}

// validMediaTypes is the allowlist of media_type values the hidden subsystem
// supports, mirroring the API's movie/show/season/episode polymorphic id model.
var validMediaTypes = map[string]bool{
	"movie":   true,
	"show":    true,
	"season":  true,
	"episode": true,
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// HiddenItemResponse is a single hidden item in API responses.
type HiddenItemResponse struct {
	UserID    string `json:"user_id"`
	Section   string `json:"section"`
	MediaType string `json:"media_type"`
	MediaID   int64  `json:"media_id"`
	HiddenAt  string `json:"hidden_at"`
}

// AddHiddenRequest is the body for POST /v1/users/hidden/:section.
type AddHiddenRequest struct {
	MediaType string `json:"media_type"`
	MediaID   int64  `json:"media_id"`
}

// RemoveHiddenRequest is the body for DELETE /v1/users/hidden/:section.
type RemoveHiddenRequest struct {
	MediaType string `json:"media_type"`
	MediaID   int64  `json:"media_id"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleListHidden returns all hidden items in a section for the current user.
func handleListHidden(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	section := c.Params("section")

	if !validSections[section] {
		return response.BadRequest(c, "invalid section: "+section)
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var items []database.UserHidden
	if err := db.Where("user_id = ? AND section = ?", userID, section).
		Order("hidden_at DESC").
		Find(&items).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to fetch hidden items")
	}

	resp := make([]HiddenItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, HiddenItemResponse{
			UserID:    item.UserID,
			Section:   item.Section,
			MediaType: item.MediaType,
			MediaID:   item.MediaID,
			HiddenAt:  response.FormatTime(item.HiddenAt),
		})
	}

	return c.JSON(resp)
}

// handleAddHidden adds items to a hidden section.
func handleAddHidden(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	section := c.Params("section")

	if !validSections[section] {
		return response.BadRequest(c, "invalid section: "+section)
	}

	var req AddHiddenRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}

	if req.MediaType == "" || req.MediaID == 0 {
		return response.BadRequest(c, "media_type and media_id are required")
	}
	if !validMediaTypes[req.MediaType] {
		return response.BadRequest(c, "invalid media_type: "+req.MediaType)
	}

	var respItem *database.UserHidden
	err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		item := database.UserHidden{
			UserID:    userID,
			Section:   section,
			MediaType: req.MediaType,
			MediaID:   req.MediaID,
			HiddenAt:  time.Now(),
		}
		// Use FirstOrCreate to avoid duplicate key errors (idempotent).
		result := tx.Where(database.UserHidden{
			UserID:    userID,
			Section:   section,
			MediaType: req.MediaType,
			MediaID:   req.MediaID,
		}).FirstOrCreate(&item)
		if result.Error != nil {
			return nil, fmt.Errorf("hide item: %w", result.Error)
		}
		respItem = &item

		// Hiding affects the calendar/progress/recommendations feeds (which read
		// user_hidden to exclude items) and the media item's own state, so bump
		// both the user and media version counters. No outbox event: the webhook
		// catalog defines no hide/unhide event type.
		queueHiddenInvalidation(cacheTx, userID, req.MediaType, req.MediaID)
		return cacheTx, nil
	})
	if err != nil {
		fmt.Printf("hidden: hide item failed: %v\n", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to hide item")
	}

	return c.Status(fiber.StatusCreated).JSON(HiddenItemResponse{
		UserID:    respItem.UserID,
		Section:   respItem.Section,
		MediaType: respItem.MediaType,
		MediaID:   respItem.MediaID,
		HiddenAt:  response.FormatTime(respItem.HiddenAt),
	})
}

// handleRemoveHidden removes items from a hidden section.
func handleRemoveHidden(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	section := c.Params("section")

	if !validSections[section] {
		return response.BadRequest(c, "invalid section: "+section)
	}

	var req RemoveHiddenRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}

	if req.MediaType == "" || req.MediaID == 0 {
		return response.BadRequest(c, "media_type and media_id are required")
	}
	if !validMediaTypes[req.MediaType] {
		return response.BadRequest(c, "invalid media_type: "+req.MediaType)
	}

	err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		result := tx.Where("user_id = ? AND section = ? AND media_type = ? AND media_id = ?",
			userID, section, req.MediaType, req.MediaID).
			Delete(&database.UserHidden{})
		if result.Error != nil {
			return nil, fmt.Errorf("unhide item: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return nil, errHiddenItemNotFound
		}

		queueHiddenInvalidation(cacheTx, userID, req.MediaType, req.MediaID)
		return cacheTx, nil
	})
	if err != nil {
		if errors.Is(err, errHiddenItemNotFound) {
			return response.NotFound(c, "hidden item not found")
		}
		fmt.Printf("hidden: unhide item failed: %v\n", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to remove hidden item")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var errHiddenItemNotFound = errors.New("hidden item not found")

// queueHiddenInvalidation bumps the user's version counter (calendar/progress/
// recommendations feeds read user_hidden to exclude items) and the target
// media's version counter after a hide/unhide, invalidating caches in O(1).
func queueHiddenInvalidation(cacheTx *cache.CacheTransaction, userID, mediaType string, mediaID int64) {
	uvk := redis.BuildUserVersionKey(userID)
	cacheTx.AddInvalidation(uvk, map[string]interface{}{
		"user_id":      userID,
		"version_key":  uvk,
		"version_only": true,
	})
	if mediaType != "" && mediaID != 0 {
		mvk := redis.BuildMediaVersionKey(mediaType, mediaID, nil, nil)
		cacheTx.AddInvalidation(mvk, map[string]interface{}{
			"media_id":     mediaID,
			"media_type":   mediaType,
			"version_key":  mvk,
			"version_only": true,
		})
	}
}
