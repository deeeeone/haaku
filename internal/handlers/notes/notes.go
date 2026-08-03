package notes

import (
	"errors"
	"strconv"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "POST", Path: "/v1/notes", Summary: "Create or update a personal note on a media item", Tags: []string{"Notes"}, Auth: "required", StatusCodes: map[int]string{200: "Note created/updated", 400: "Invalid request", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/notes/{id}", Summary: "Get a single note by ID (owner only)", Tags: []string{"Notes"}, Auth: "required", StatusCodes: map[int]string{200: "Note found", 404: "Not found", 401: "Unauthorized"}},
		{Method: "PUT", Path: "/v1/notes/{id}", Summary: "Update a note body (owner only)", Tags: []string{"Notes"}, Auth: "required", StatusCodes: map[int]string{200: "Note updated", 403: "Not your note", 404: "Not found", 401: "Unauthorized"}},
		{Method: "DELETE", Path: "/v1/notes/{id}", Summary: "Delete a note (owner only)", Tags: []string{"Notes"}, Auth: "required", StatusCodes: map[int]string{204: "Note deleted", 404: "Not found", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/users/me/notes", Summary: "List own notes, paginated, optional media_type filter", Tags: []string{"Notes"}, Auth: "required", StatusCodes: map[int]string{200: "Notes list", 401: "Unauthorized"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires note routes.
func RegisterRoutes(api fiber.Router) {
	api.Post("/notes", requireAuth, handleCreateNote)
	api.Get("/notes/:id", requireAuth, handleGetNote)
	api.Put("/notes/:id", requireAuth, handleUpdateNote)
	api.Delete("/notes/:id", requireAuth, handleDeleteNote)
	api.Get("/users/me/notes", requireAuth, handleListNotes)
}

// ---------------------------------------------------------------------------
// Auth middleware
// ---------------------------------------------------------------------------

func requireAuth(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return writeError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	return c.Next()
}

// ---------------------------------------------------------------------------
// Request/Response types
// ---------------------------------------------------------------------------

type noteCreateRequest struct {
	MediaType string `json:"media_type"`
	MediaID   int64  `json:"media_id"`
	Note      string `json:"note"`
}

type noteUpdateRequest struct {
	Note string `json:"note"`
}

type noteResponse struct {
	ID        int64  `json:"id"`
	UserID    string `json:"user_id"`
	MediaType string `json:"media_type"`
	MediaID   int64  `json:"media_id"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type notesListResponse struct {
	Data       []noteResponse `json:"data"`
	Page       int            `json:"page"`
	Limit      int            `json:"limit"`
	TotalCount int64          `json:"total_count"`
}

func toNoteResponse(n *database.UserNote) noteResponse {
	return noteResponse{
		ID:        n.ID,
		UserID:    n.UserID,
		MediaType: n.MediaType,
		MediaID:   n.MediaID,
		Note:      n.Note,
		CreatedAt: n.CreatedAt.Format("2006-01-02T15:04:05.000Z"),
		UpdatedAt: n.UpdatedAt.Format("2006-01-02T15:04:05.000Z"),
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// POST /v1/notes — create or update a note (upsert)
func handleCreateNote(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req noteCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.MediaType == "" || req.MediaID == 0 || req.Note == "" {
		return badRequest(c, "media_type, media_id, and note are required")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	now := time.Now()

	// Try to find existing note for upsert.
	var existing database.UserNote
	result := db.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, req.MediaType, req.MediaID).First(&existing)

	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		// Create new note.
		note := database.UserNote{
			UserID:    userID,
			MediaType: req.MediaType,
			MediaID:   req.MediaID,
			Note:      req.Note,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := db.Create(&note).Error; err != nil {
			return writeError(c, fiber.StatusInternalServerError, "internal", "failed to create note")
		}
		return c.Status(fiber.StatusOK).JSON(toNoteResponse(&note))
	}

	// Update existing note.
	existing.Note = req.Note
	existing.UpdatedAt = now
	if err := db.Save(&existing).Error; err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to update note")
	}

	return c.Status(fiber.StatusOK).JSON(toNoteResponse(&existing))
}

// GET /v1/notes/:id — get single note (owner only)
func handleGetNote(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return badRequest(c, "invalid note id")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var note database.UserNote
	if err := db.First(&note, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "note not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	if note.UserID != userID {
		return notFound(c, "note not found")
	}

	return c.JSON(toNoteResponse(&note))
}

// PUT /v1/notes/:id — update note body (owner only)
func handleUpdateNote(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return badRequest(c, "invalid note id")
	}

	var req noteUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if req.Note == "" {
		return badRequest(c, "note is required")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var note database.UserNote
	if err := db.First(&note, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "note not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	if note.UserID != userID {
		return notFound(c, "note not found")
	}

	note.Note = req.Note
	note.UpdatedAt = time.Now()
	if err := db.Save(&note).Error; err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to update note")
	}

	return c.JSON(toNoteResponse(&note))
}

// DELETE /v1/notes/:id — delete note (owner only)
func handleDeleteNote(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return badRequest(c, "invalid note id")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	result := db.Where("id = ? AND user_id = ?", id, userID).Delete(&database.UserNote{})
	if result.Error != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to delete note")
	}
	if result.RowsAffected == 0 {
		return notFound(c, "note not found")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// GET /v1/users/me/notes — list own notes, paginated
func handleListNotes(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	page := queryInt(c, "page", 1)
	limit := clampLimit(queryInt(c, "limit", 20), 1, 100)
	mediaType := c.Query("media_type", "")

	query := db.Model(&database.UserNote{}).Where("user_id = ?", userID)
	if mediaType != "" {
		query = query.Where("media_type = ?", mediaType)
	}

	var totalCount int64
	query.Count(&totalCount)

	var notes []database.UserNote
	query.Order("created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&notes)

	resp := make([]noteResponse, len(notes))
	for i, n := range notes {
		resp[i] = toNoteResponse(&n)
	}

	return c.JSON(notesListResponse{
		Data:       resp,
		Page:       page,
		Limit:      limit,
		TotalCount: totalCount,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func queryInt(c *fiber.Ctx, key string, defaultVal int) int {
	val := c.Query(key, "")
	if val == "" {
		return defaultVal
	}
	iv, err := strconv.Atoi(val)
	if err != nil || iv < 1 {
		return defaultVal
	}
	return iv
}

func clampLimit(limit, min, max int) int {
	if limit < min {
		return min
	}
	if limit > max {
		return max
	}
	return limit
}

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
