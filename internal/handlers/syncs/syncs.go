package syncs

import (
	"errors"

	"trakt-api/internal/auth"
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
		{
			Method:  "POST",
			Path:    "/v1/syncs",
			Summary: "Create a batch sync session for history or ratings import",
			Tags:    []string{"Syncs"},
			Auth:    "required",
			StatusCodes: map[int]string{
				201: "Sync session created",
				400: "Invalid request",
				401: "Unauthorized",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/syncs/{id}",
			Summary: "Get sync session progress",
			Tags:    []string{"Syncs"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Sync session progress",
				401: "Unauthorized",
				404: "Sync session not found",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/syncs/{id}/paused",
			Summary: "Get items that couldn't be matched during sync",
			Tags:    []string{"Syncs"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Paused/unmatched items",
				401: "Unauthorized",
				404: "Sync session not found",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/syncs/{id}/skipped",
			Summary: "Get skipped items from sync session",
			Tags:    []string{"Syncs"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Skipped items",
				401: "Unauthorized",
				404: "Sync session not found",
			},
		},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires sync routes to the Fiber router.
func RegisterRoutes(api fiber.Router) {
	syncs := api.Group("/syncs")
	syncs.Post("/", auth.AuthRequired(), handleCreateSync)
	syncs.Get("/:id<int>", auth.AuthRequired(), handleGetSync)
	syncs.Get("/:id<int>/paused", auth.AuthRequired(), handleGetSyncPaused)
	syncs.Get("/:id<int>/skipped", auth.AuthRequired(), handleGetSyncSkipped)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// SyncCreateRequest is the body for POST /v1/syncs.
type SyncCreateRequest struct {
	Kind  string `json:"kind"`  // history | ratings
	Items []any  `json:"items"` // items to sync
}

// SyncResponse is the response for sync session endpoints.
type SyncResponse struct {
	ID             int64  `json:"id"`
	UserID         string `json:"user_id"`
	Kind           string `json:"kind"`
	Status         string `json:"status"`
	TotalItems     int    `json:"total_items"`
	ProcessedItems int    `json:"processed_items"`
	Errors         any    `json:"errors,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// handleCreateSync creates a new batch sync session.
func handleCreateSync(c *fiber.Ctx) error {
	userID, err := requireUserID(c)
	if err != nil {
		return err
	}

	var req SyncCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	if req.Kind != "history" && req.Kind != "ratings" {
		return writeError(c, fiber.StatusBadRequest, "invalid_request", "kind must be 'history' or 'ratings'")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	session := database.SyncSession{
		UserID:     userID,
		Kind:       req.Kind,
		Status:     "pending",
		TotalItems: len(req.Items),
	}

	if err := db.Create(&session).Error; err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to create sync session")
	}

	// TODO: enqueue async processing (Phase 3+).

	return c.Status(fiber.StatusCreated).JSON(buildSyncResponse(&session))
}

// handleGetSync returns the progress of a sync session.
func handleGetSync(c *fiber.Ctx) error {
	userID, err := requireUserID(c)
	if err != nil {
		return err
	}
	syncID, err := parseSyncID(c)
	if err != nil {
		return err
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var session database.SyncSession
	if err := db.Where("id = ? AND user_id = ?", syncID, userID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "sync session not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	return c.JSON(buildSyncResponse(&session))
}

// handleGetSyncPaused returns items that couldn't be matched.
func handleGetSyncPaused(c *fiber.Ctx) error {
	userID, err := requireUserID(c)
	if err != nil {
		return err
	}
	syncID, err := parseSyncID(c)
	if err != nil {
		return err
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var session database.SyncSession
	if err := db.Where("id = ? AND user_id = ?", syncID, userID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "sync session not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	return c.JSON(fiber.Map{
		"sync_id":      session.ID,
		"paused_items": session.Errors,
	})
}

// handleGetSyncSkipped returns skipped items.
func handleGetSyncSkipped(c *fiber.Ctx) error {
	userID, err := requireUserID(c)
	if err != nil {
		return err
	}
	syncID, err := parseSyncID(c)
	if err != nil {
		return err
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var session database.SyncSession
	if err := db.Where("id = ? AND user_id = ?", syncID, userID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "sync session not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	// Minimal: return empty for now; skipped items require full sync engine (Phase 3+).
	return c.JSON(fiber.Map{
		"sync_id":       session.ID,
		"skipped_items": []any{},
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func requireUserID(c *fiber.Ctx) (string, error) {
	uid, ok := c.Locals("user_id").(string)
	if !ok || uid == "" {
		return "", writeError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	return uid, nil
}

func parseSyncID(c *fiber.Ctx) (int64, error) {
	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return 0, writeError(c, fiber.StatusBadRequest, "invalid_request", "invalid sync id")
	}
	return int64(id), nil
}

func buildSyncResponse(s *database.SyncSession) SyncResponse {
	return SyncResponse{
		ID:             s.ID,
		UserID:         s.UserID,
		Kind:           s.Kind,
		Status:         s.Status,
		TotalItems:     s.TotalItems,
		ProcessedItems: s.ProcessedItems,
		Errors:         s.Errors,
		CreatedAt:      s.CreatedAt.Format("2006-01-02T15:04:05.000Z"),
		UpdatedAt:      s.UpdatedAt.Format("2006-01-02T15:04:05.000Z"),
	}
}

func writeError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{
			"code":    code,
			"message": message,
		},
	})
}
