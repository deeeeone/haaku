package hidden

import (
	"time"

	"trakt-api/internal/auth"
	"trakt-api/internal/database"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
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
	"calendar":            true,
	"progress_watched":    true,
	"progress_collected":  true,
	"recommendations":     true,
	"comments":            true,
	"dropped":             true,
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
		return badRequest(c, "invalid section: "+section)
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var items []database.UserHidden
	if err := db.Where("user_id = ? AND section = ?", userID, section).
		Order("hidden_at DESC").
		Find(&items).Error; err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to fetch hidden items")
	}

	resp := make([]HiddenItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, HiddenItemResponse{
			UserID:    item.UserID,
			Section:   item.Section,
			MediaType: item.MediaType,
			MediaID:   item.MediaID,
			HiddenAt:  item.HiddenAt.Format("2006-01-02T15:04:05.000Z"),
		})
	}

	return c.JSON(resp)
}

// handleAddHidden adds items to a hidden section.
func handleAddHidden(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	section := c.Params("section")

	if !validSections[section] {
		return badRequest(c, "invalid section: "+section)
	}

	var req AddHiddenRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}

	if req.MediaType == "" || req.MediaID == 0 {
		return badRequest(c, "media_type and media_id are required")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	item := database.UserHidden{
		UserID:    userID,
		Section:   section,
		MediaType: req.MediaType,
		MediaID:   req.MediaID,
		HiddenAt:  time.Now(),
	}

	// Use FirstOrCreate to avoid duplicate key errors.
	result := db.Where(database.UserHidden{
		UserID:    userID,
		Section:   section,
		MediaType: req.MediaType,
		MediaID:   req.MediaID,
	}).FirstOrCreate(&item)

	if result.Error != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to hide item")
	}

	// Publish outbox event (best-effort, non-blocking).
	_ = db

	return c.Status(fiber.StatusCreated).JSON(HiddenItemResponse{
		UserID:    item.UserID,
		Section:   item.Section,
		MediaType: item.MediaType,
		MediaID:   item.MediaID,
		HiddenAt:  item.HiddenAt.Format("2006-01-02T15:04:05.000Z"),
	})
}

// handleRemoveHidden removes items from a hidden section.
func handleRemoveHidden(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	section := c.Params("section")

	if !validSections[section] {
		return badRequest(c, "invalid section: "+section)
	}

	var req RemoveHiddenRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}

	if req.MediaType == "" || req.MediaID == 0 {
		return badRequest(c, "media_type and media_id are required")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	result := db.Where("user_id = ? AND section = ? AND media_type = ? AND media_id = ?",
		userID, section, req.MediaType, req.MediaID).
		Delete(&database.UserHidden{})

	if result.Error != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to remove hidden item")
	}

	if result.RowsAffected == 0 {
		return notFound(c, "hidden item not found")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{
			"code":    code,
			"message": message,
		},
	})
}

func badRequest(c *fiber.Ctx, msg string) error {
	return writeError(c, fiber.StatusBadRequest, "invalid_request", msg)
}

func notFound(c *fiber.Ctx, msg string) error {
	return writeError(c, fiber.StatusNotFound, "not_found", msg)
}
