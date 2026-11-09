package notes

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/gorm"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "POST", Path: "/v1/notes", Summary: "Create or update a personal note on a media item", Tags: []string{"Notes"}, Auth: "required", StatusCodes: map[int]string{200: "Note created/updated", 400: "Invalid request", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/notes/{id}", Summary: "Get a single note by ID (owner only)", Tags: []string{"Notes"}, Auth: "required", StatusCodes: map[int]string{200: "Note found", 403: "Not your note", 404: "Not found", 401: "Unauthorized"}},
		{Method: "PUT", Path: "/v1/notes/{id}", Summary: "Update a note body (owner only)", Tags: []string{"Notes"}, Auth: "required", StatusCodes: map[int]string{200: "Note updated", 403: "Not your note", 404: "Not found", 401: "Unauthorized"}},
		{Method: "DELETE", Path: "/v1/notes/{id}", Summary: "Delete a note (owner only)", Tags: []string{"Notes"}, Auth: "required", StatusCodes: map[int]string{204: "Note deleted", 403: "Not your note", 404: "Not found", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/users/me/notes", Summary: "List own notes, paginated, optional media_type filter", Tags: []string{"Notes"}, Auth: "required", StatusCodes: map[int]string{200: "Notes list", 401: "Unauthorized"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires note routes.
func RegisterRoutes(api fiber.Router) {
	api.Post("/notes", auth.AuthRequired(), handleCreateNote)
	api.Get("/notes/:id", auth.AuthRequired(), handleGetNote)
	api.Put("/notes/:id", auth.AuthRequired(), handleUpdateNote)
	api.Delete("/notes/:id", auth.AuthRequired(), handleDeleteNote)
	api.Get("/users/me/notes", auth.AuthRequired(), handleListNotes)
}

// ---------------------------------------------------------------------------
// Auth middleware
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Request/Response types
// ---------------------------------------------------------------------------

type noteCreateRequest struct {
	MediaType string `json:"media_type"`
	MediaID   int64  `json:"media_id"`
	Note      string `json:"note"`
}

// maxNoteLength caps note bodies at 500 characters per the API spec
// ("500-char cap" for parity).
const maxNoteLength = 500

// validMediaTypes is the allowlist of media_type values notes may attach to,
// mirroring the API's movie/show/season/episode polymorphic id model.
var validMediaTypes = map[string]bool{
	"movie":   true,
	"show":    true,
	"season":  true,
	"episode": true,
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
		CreatedAt: response.FormatTime(n.CreatedAt),
		UpdatedAt: response.FormatTime(n.UpdatedAt),
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// POST /v1/notes — create or update a note (upsert)
func handleCreateNote(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req noteCreateRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}
	if req.MediaType == "" || req.MediaID == 0 || req.Note == "" {
		return response.BadRequest(c, "media_type, media_id, and note are required")
	}
	if !validMediaTypes[req.MediaType] {
		return response.BadRequest(c, "invalid media_type: "+req.MediaType)
	}
	if len(req.Note) > maxNoteLength {
		return response.BadRequest(c, fmt.Sprintf("note exceeds maximum length of %d characters", maxNoteLength))
	}

	now := time.Now()

	var resp noteResponse
	err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		// Serialize concurrent upserts for the same (user, media) so the
		// check-then-write below is race-free: two simultaneous POSTs can't both
		// see "not found" and create duplicate rows (no DB unique constraint
		// today) or lose an update. Advisory lock mirrors checkin/sync.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))",
			fmt.Sprintf("note:%s:%s:%d", userID, req.MediaType, req.MediaID)).Error; err != nil {
			return nil, fmt.Errorf("advisory lock: %w", err)
		}

		// Try to find existing note for upsert.
		var existing database.UserNote
		result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, req.MediaType, req.MediaID).First(&existing)
		if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("lookup note: %w", result.Error)
		}

		var note database.UserNote
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			// Create new note.
			note = database.UserNote{
				UserID:    userID,
				MediaType: req.MediaType,
				MediaID:   req.MediaID,
				Note:      req.Note,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := tx.Create(&note).Error; err != nil {
				return nil, fmt.Errorf("create note: %w", err)
			}
		} else {
			// Update existing note.
			existing.Note = req.Note
			existing.UpdatedAt = now
			if err := tx.Save(&existing).Error; err != nil {
				return nil, fmt.Errorf("update note: %w", err)
			}
			note = existing
		}

		queueNoteInvalidation(cacheTx, &note, userID)
		resp = toNoteResponse(&note)
		return cacheTx, nil
	})
	if err != nil {
		fmt.Printf("notes: upsert note failed: %v\n", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to save note")
	}

	return c.Status(fiber.StatusOK).JSON(resp)
}

// GET /v1/notes/:id — get single note (owner only)
func handleGetNote(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return response.BadRequest(c, "invalid note id")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var note database.UserNote
	if err := db.First(&note, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "note not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	if note.UserID != userID {
		return response.Forbidden(c, "note belongs to another user")
	}

	return c.JSON(toNoteResponse(&note))
}

// PUT /v1/notes/:id — update note body (owner only)
func handleUpdateNote(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return response.BadRequest(c, "invalid note id")
	}

	var req noteUpdateRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}
	if req.Note == "" {
		return response.BadRequest(c, "note is required")
	}
	if len(req.Note) > maxNoteLength {
		return response.BadRequest(c, fmt.Sprintf("note exceeds maximum length of %d characters", maxNoteLength))
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var note database.UserNote
	if err := db.First(&note, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "note not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	if note.UserID != userID {
		return response.Forbidden(c, "note belongs to another user")
	}

	var resp noteResponse
	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		now := time.Now()
		note.Note = req.Note
		note.UpdatedAt = now
		if err := tx.Save(&note).Error; err != nil {
			return nil, fmt.Errorf("update note: %w", err)
		}

		queueNoteInvalidation(cacheTx, &note, userID)
		resp = toNoteResponse(&note)
		return cacheTx, nil
	})
	if err != nil {
		fmt.Printf("notes: update note %d failed: %v\n", id, err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to update note")
	}

	return c.JSON(resp)
}

// DELETE /v1/notes/:id — delete note (owner only)
func handleDeleteNote(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return response.BadRequest(c, "invalid note id")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var note database.UserNote
	result := db.Where("id = ?", id).First(&note)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "note not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	if note.UserID != userID {
		return response.Forbidden(c, "note belongs to another user")
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := tx.Delete(&note).Error; err != nil {
			return nil, fmt.Errorf("delete note: %w", err)
		}
		queueNoteInvalidation(cacheTx, &note, userID)
		return cacheTx, nil
	})
	if err != nil {
		fmt.Printf("notes: delete note %d failed: %v\n", id, err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to delete note")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// GET /v1/users/me/notes — list own notes, paginated
func handleListNotes(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	page := response.QueryInt(c, "page", 1)
	limit := response.ClampLimit(response.QueryInt(c, "limit", 20), 1, 100)
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

// queueNoteInvalidation bumps the user's version counter (their notes list)
// and the target media's version counter (media responses can surface a note
// presence/count) after any note write, invalidating caches in O(1). Notes are
// private per-user; the webhook catalog defines no note event, so no outbox
// event is emitted.
func queueNoteInvalidation(cacheTx *cache.CacheTransaction, note *database.UserNote, userID string) {
	uvk := redis.BuildUserVersionKey(userID)
	cacheTx.AddInvalidation(uvk, map[string]interface{}{
		"user_id":      userID,
		"version_key":  uvk,
		"version_only": true,
	})
	if note != nil && note.MediaType != "" && note.MediaID != 0 {
		mvk := redis.BuildMediaVersionKey(note.MediaType, note.MediaID, nil, nil)
		cacheTx.AddInvalidation(mvk, map[string]interface{}{
			"media_id":     note.MediaID,
			"media_type":   note.MediaType,
			"version_key":  mvk,
			"version_only": true,
		})
	}
}
