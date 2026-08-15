package filters

import (
	"errors"
	"strconv"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"trakt-api/internal/auth"
	"trakt-api/internal/response"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/users/me/saved_filters/{section}", Summary: "List saved filters for a section", Tags: []string{"Filters"}, Auth: "required", StatusCodes: map[int]string{200: "Filters list", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/users/me/saved_filters", Summary: "Save a filter for a section (max 20 per section)", Tags: []string{"Filters"}, Auth: "required", StatusCodes: map[int]string{201: "Filter saved", 400: "Invalid request or limit reached", 401: "Unauthorized"}},
		{Method: "DELETE", Path: "/v1/users/me/saved_filters/{id}", Summary: "Delete a saved filter (owner only)", Tags: []string{"Filters"}, Auth: "required", StatusCodes: map[int]string{204: "Filter deleted", 404: "Not found", 401: "Unauthorized"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires saved filter routes.
func RegisterRoutes(api fiber.Router) {
	api.Get("/users/me/saved_filters/:section", auth.AuthRequired(), handleListFilters)
	api.Post("/users/me/saved_filters", auth.AuthRequired(), handleSaveFilter)
	api.Delete("/users/me/saved_filters/:id", auth.AuthRequired(), handleDeleteFilter)
}

// ---------------------------------------------------------------------------
// Auth middleware
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Request/Response types
// ---------------------------------------------------------------------------

type filterSaveRequest struct {
	Section string           `json:"section"`
	Name    string           `json:"name"`
	Filter  database.JSONMap `json:"filter"`
}

type filterResponse struct {
	ID        int64            `json:"id"`
	UserID    string           `json:"user_id"`
	Section   string           `json:"section"`
	Name      string           `json:"name"`
	Filter    database.JSONMap `json:"filter"`
	CreatedAt string           `json:"created_at"`
}

func toFilterResponse(f *database.SavedFilter) filterResponse {
	return filterResponse{
		ID:        f.ID,
		UserID:    f.UserID,
		Section:   f.Section,
		Name:      f.Name,
		Filter:    f.Filter,
		CreatedAt: response.FormatTime(f.CreatedAt),
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// GET /v1/users/me/saved_filters/:section — list saved filters for a section
func handleListFilters(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	section := c.Params("section")
	if section == "" {
		return response.BadRequest(c, "section is required")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var filters []database.SavedFilter
	if err := db.Where("user_id = ? AND section = ?", userID, section).
		Order("created_at ASC").
		Find(&filters).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to fetch filters")
	}

	resp := make([]filterResponse, len(filters))
	for i, f := range filters {
		resp[i] = toFilterResponse(&f)
	}

	return c.JSON(resp)
}

// POST /v1/users/me/saved_filters — save a filter
func handleSaveFilter(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req filterSaveRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid request body")
	}
	if req.Section == "" || req.Name == "" {
		return response.BadRequest(c, "section and name are required")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Check count — max 20 per user per section.
	var count int64
	db.Model(&database.SavedFilter{}).
		Where("user_id = ? AND section = ?", userID, req.Section).
		Count(&count)
	if count >= 20 {
		return response.BadRequest(c, "maximum of 20 saved filters per section reached")
	}

	filter := req.Filter
	if filter == nil {
		filter = database.JSONMap{}
	}

	saved := database.SavedFilter{
		UserID:  userID,
		Section: req.Section,
		Name:    req.Name,
		Filter:  filter,
	}

	if err := db.Create(&saved).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to save filter")
	}

	return c.Status(fiber.StatusCreated).JSON(toFilterResponse(&saved))
}

// DELETE /v1/users/me/saved_filters/:id — delete a saved filter
func handleDeleteFilter(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return response.BadRequest(c, "invalid filter id")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify owner before deleting.
	var f database.SavedFilter
	if err := db.First(&f, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "filter not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}
	if f.UserID != userID {
		return response.NotFound(c, "filter not found")
	}

	if err := db.Delete(&f).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to delete filter")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
