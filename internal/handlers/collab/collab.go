package collab

import (
	"errors"
	"time"

	"trakt-api/internal/auth"
	"trakt-api/internal/database"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"trakt-api/internal/response"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{
			Method:  "GET",
			Path:    "/v1/users/me/lists/collaborations",
			Summary: "Get all lists the current user can edit (owned + collaborated)",
			Tags:    []string{"Lists"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Collaborated lists",
				401: "Unauthorized",
			},
		},
		{
			Method:  "POST",
			Path:    "/v1/lists/{id}/collaborators",
			Summary: "Invite a user as collaborator (owner only)",
			Tags:    []string{"Lists"},
			Auth:    "required",
			StatusCodes: map[int]string{
				201: "Collaborator invited",
				400: "Invalid request",
				403: "Forbidden (not owner)",
				404: "List not found",
				401: "Unauthorized",
			},
		},
		{
			Method:  "DELETE",
			Path:    "/v1/lists/{id}/collaborators/{user_id}",
			Summary: "Remove a collaborator (owner only)",
			Tags:    []string{"Lists"},
			Auth:    "required",
			StatusCodes: map[int]string{
				204: "Collaborator removed",
				403: "Forbidden (not owner)",
				404: "Not found",
				401: "Unauthorized",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/lists/{id}/collaborators",
			Summary: "List collaborators for a list",
			Tags:    []string{"Lists"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Collaborators list",
				404: "List not found",
				401: "Unauthorized",
			},
		},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires collaboration routes.
func RegisterRoutes(api fiber.Router) {
	// GET /v1/users/me/lists/collaborations
	api.Get("/users/me/lists/collaborations", auth.AuthRequired(), handleMyCollaborations)

	// List-scoped collaborators
	lists := api.Group("/lists")
	lists.Post("/:id/collaborators", auth.AuthRequired(), handleInviteCollaborator)
	lists.Delete("/:id/collaborators/:user_id", auth.AuthRequired(), handleRemoveCollaborator)
	lists.Get("/:id/collaborators", auth.AuthRequired(), handleListCollaborators)
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// CollaboratorResponse is the API shape for a collaborator.
type CollaboratorResponse struct {
	UserID     string  `json:"user_id"`
	Role       string  `json:"role"`
	InvitedAt  string  `json:"invited_at"`
	AcceptedAt *string `json:"accepted_at,omitempty"`
}

// InviteCollaboratorRequest is the body for POST /v1/lists/:id/collaborators.
type InviteCollaboratorRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"` // editor | viewer
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleMyCollaborations returns all lists the current user can edit.
func handleMyCollaborations(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Find lists where user is a collaborator.
	var collabs []database.ListCollaborator
	if err := db.Where("user_id = ?", userID).Find(&collabs).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to fetch collaborations")
	}

	// Build response.
	resp := make([]map[string]interface{}, 0, len(collabs))
	for _, col := range collabs {
		entry := map[string]interface{}{
			"list_id":    col.ListID,
			"user_id":    col.UserID,
			"role":       col.Role,
			"invited_at": response.FormatTime(col.InvitedAt),
		}
		if col.AcceptedAt != nil {
			ts := response.FormatTime(*col.AcceptedAt)
			entry["accepted_at"] = ts
		}
		resp = append(resp, entry)
	}

	return c.JSON(resp)
}

// handleInviteCollaborator invites a user to collaborate on a list.
func handleInviteCollaborator(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	listIDStr := c.Params("id")
	listID := parseID(listIDStr)
	if listID == 0 {
		return response.BadRequest(c, "invalid list id")
	}

	var req InviteCollaboratorRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid request body")
	}
	if req.Username == "" {
		return response.BadRequest(c, "username is required")
	}
	if req.Role != "editor" && req.Role != "viewer" {
		return response.BadRequest(c, "role must be 'editor' or 'viewer'")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify list exists and current user is the owner.
	var listCount int64
	if err := db.Table("lists").Where("id = ? AND user_id = ?", listID, userID).Count(&listCount).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}
	if listCount == 0 {
		return response.NotFound(c, "list not found")
	}

	// Resolve username to user ID.
	var targetUser database.User
	if err := db.Where("username = ?", req.Username).First(&targetUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "user not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	// Don't allow inviting yourself.
	if targetUser.ID == userID {
		return response.BadRequest(c, "cannot invite yourself")
	}

	// Check if already a collaborator.
	var existing database.ListCollaborator
	if err := db.Where("list_id = ? AND user_id = ?", listID, targetUser.ID).First(&existing).Error; err == nil {
		return response.BadRequest(c, "user is already a collaborator")
	}

	now := time.Now()
	collab := database.ListCollaborator{
		ListID:    listID,
		UserID:    targetUser.ID,
		Role:      req.Role,
		InvitedAt: now,
	}

	if err := db.Create(&collab).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to add collaborator")
	}

	resp := CollaboratorResponse{
		UserID:    collab.UserID,
		Role:      collab.Role,
		InvitedAt: response.FormatTime(collab.InvitedAt),
	}

	return c.Status(fiber.StatusCreated).JSON(resp)
}

// handleRemoveCollaborator removes a collaborator from a list.
func handleRemoveCollaborator(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	listIDStr := c.Params("id")
	targetUserID := c.Params("user_id")

	listID := parseID(listIDStr)
	if listID == 0 {
		return response.BadRequest(c, "invalid list id")
	}
	if targetUserID == "" {
		return response.BadRequest(c, "user_id is required")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify list exists and current user is the owner.
	var listCount int64
	if err := db.Table("lists").Where("id = ? AND user_id = ?", listID, userID).Count(&listCount).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}
	if listCount == 0 {
		return response.NotFound(c, "list not found")
	}

	result := db.Where("list_id = ? AND user_id = ?", listID, targetUserID).Delete(&database.ListCollaborator{})
	if result.Error != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to remove collaborator")
	}
	if result.RowsAffected == 0 {
		return response.NotFound(c, "collaborator not found")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// handleListCollaborators lists all collaborators for a list.
func handleListCollaborators(c *fiber.Ctx) error {
	listIDStr := c.Params("id")
	listID := parseID(listIDStr)
	if listID == 0 {
		return response.BadRequest(c, "invalid list id")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var collabs []database.ListCollaborator
	if err := db.Where("list_id = ?", listID).Find(&collabs).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to fetch collaborators")
	}

	resp := make([]CollaboratorResponse, 0, len(collabs))
	for _, col := range collabs {
		entry := CollaboratorResponse{
			UserID:    col.UserID,
			Role:      col.Role,
			InvitedAt: response.FormatTime(col.InvitedAt),
		}
		if col.AcceptedAt != nil {
			ts := response.FormatTime(*col.AcceptedAt)
			entry.AcceptedAt = &ts
		}
		resp = append(resp, entry)
	}

	return c.JSON(resp)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func parseID(s string) int64 {
	var id int64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		id = id*10 + int64(ch-'0')
	}
	return id
}
