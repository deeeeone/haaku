package syncs

import (
	"encoding/json"
	"errors"
	"strconv"

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
			Summary: "Get paused items (unmatched or errored) from a sync session",
			Tags:    []string{"Syncs"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Paused items",
				401: "Unauthorized",
				404: "Sync session not found",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/syncs/{id}/skipped",
			Summary: "Get skipped (duplicate) items from a sync session",
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
	ID             int64               `json:"id"`
	UserID         string              `json:"user_id"`
	Kind           string              `json:"kind"`
	Status         string              `json:"status"`
	TotalItems     int                 `json:"total_items"`
	ProcessedItems int                 `json:"processed_items"`
	Errors         database.SyncErrors `json:"errors"`
	CreatedAt      string              `json:"created_at"`
	UpdatedAt      string              `json:"updated_at"`
}

// SyncItemResponse is one item in the /paused and /skipped listings.
type SyncItemResponse struct {
	ID          int64            `json:"id"`
	Seq         int              `json:"seq"`
	MediaType   *string          `json:"media_type,omitempty"`
	MediaID     *int64           `json:"media_id,omitempty"`
	Outcome     string           `json:"outcome"`
	ErrorCode   *string          `json:"error_code,omitempty"`
	Error       *string          `json:"error,omitempty"`
	WatchedAt   *string          `json:"watched_at,omitempty"`
	Rating      *int             `json:"rating,omitempty"`
	Payload     database.JSONMap `json:"payload,omitempty"`
	ProcessedAt *string          `json:"processed_at,omitempty"`
}

// handleCreateSync creates a new batch sync session and persists the submitted
// items so the session is resumable and auditable. It deliberately does NOT
// process them: resolution and the collection writes are the async engine's job
// (see internal/syncengine), so the request path stays a fast insert.
func handleCreateSync(c *fiber.Ctx) error {
	userID, err := requireUserID(c)
	if err != nil {
		return err
	}

	var req SyncCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return response.WriteError(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	if req.Kind != "history" && req.Kind != "ratings" {
		return response.WriteError(c, fiber.StatusBadRequest, "invalid_request", "kind must be 'history' or 'ratings'")
	}

	// Each item must be a JSON object: the engine resolves items by their ids
	// and attributes, so a scalar or array entry can never be processed. Reject
	// it up front rather than storing something the engine can only poison.
	payloads := make([]database.JSONMap, 0, len(req.Items))
	for i, raw := range req.Items {
		payload, ok := itemObject(raw)
		if !ok {
			return response.WriteError(c, fiber.StatusBadRequest, "invalid_request",
				"items["+strconv.Itoa(i)+"] must be an object")
		}
		payloads = append(payloads, payload)
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	session := database.SyncSession{
		UserID:     userID,
		Kind:       req.Kind,
		Status:     database.SyncStatusPending,
		TotalItems: len(req.Items),
	}

	// Session + items in one transaction: a session with no items, or items
	// with no session, is never observable. The engine (a separate process)
	// only ever sees a session that is fully enqueued.
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		if len(payloads) == 0 {
			return nil
		}
		items := make([]database.SyncSessionItem, 0, len(payloads))
		for i, payload := range payloads {
			items = append(items, database.SyncSessionItem{
				SessionID: session.ID,
				Seq:       i + 1, // 1-based: the order the engine walks
				Payload:   payload,
				State:     "pending",
			})
		}
		return tx.Create(&items).Error
	})
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to create sync session")
	}

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
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var session database.SyncSession
	if err := db.Where("id = ? AND user_id = ?", syncID, userID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "sync session not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	return c.JSON(buildSyncResponse(&session))
}

// handleGetSyncPaused returns the items the engine paused — ones it could not
// add, either because they did not resolve to a media row (not_found) or
// because the item itself was rejected (error). This is real engine output now
// (issue #6): the engine writes sync_session_items.outcome, and this reads it
// back owner-scoped. Serving a 501 here was correct only while no engine
// existed.
func handleGetSyncPaused(c *fiber.Ctx) error {
	return serveSessionItems(c, []string{database.SyncOutcomeNotFound, database.SyncOutcomeError})
}

// handleGetSyncSkipped returns the items the engine deliberately did not write
// because the collection already had them (duplicate). Real engine output, same
// as /paused.
func handleGetSyncSkipped(c *fiber.Ctx) error {
	return serveSessionItems(c, []string{database.SyncOutcomeDuplicate})
}

// serveSessionItems pages the items of a session whose outcome is one of
// outcomes. The session must belong to the caller (404 otherwise, so another
// user's session id is indistinguishable from a missing one).
func serveSessionItems(c *fiber.Ctx, outcomes []string) error {
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
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var session database.SyncSession
	if err := db.Where("id = ? AND user_id = ?", syncID, userID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "sync session not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	page, limit := response.ParsePagination(c, 25)
	query := db.Model(&database.SyncSessionItem{}).
		Where("session_id = ? AND outcome IN ?", syncID, outcomes)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	var items []database.SyncSessionItem
	if err := query.Order("seq ASC").Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	results := make([]SyncItemResponse, 0, len(items))
	for i := range items {
		results = append(results, buildSyncItemResponse(&items[i]))
	}

	response.SetPaginationHeaders(c, page, limit, total, len(results))

	return c.JSON(fiber.Map{
		"session_id": session.ID,
		"status":     session.Status,
		"items":      results,
		"page":       page,
		"limit":      limit,
		"row_count":  total,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func requireUserID(c *fiber.Ctx) (string, error) {
	uid, ok := c.Locals("user_id").(string)
	if !ok || uid == "" {
		return "", response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	return uid, nil
}

func parseSyncID(c *fiber.Ctx) (int64, error) {
	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return 0, response.WriteError(c, fiber.StatusBadRequest, "invalid_request", "invalid sync id")
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
		CreatedAt:      response.FormatTime(s.CreatedAt),
		UpdatedAt:      response.FormatTime(s.UpdatedAt),
	}
}

// itemObject normalises one submitted item (decoded as `any`) to a JSON object.
// Anything that is not an object cannot be resolved by the engine, so it is
// rejected rather than stored as a payload that can only fail.
func itemObject(raw any) (database.JSONMap, bool) {
	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var obj database.JSONMap
	if err := json.Unmarshal(bytes, &obj); err != nil || obj == nil {
		return nil, false
	}
	return obj, true
}

func buildSyncItemResponse(it *database.SyncSessionItem) SyncItemResponse {
	resp := SyncItemResponse{
		ID:        it.ID,
		Seq:       it.Seq,
		MediaType: it.MediaType,
		MediaID:   it.MediaID,
		ErrorCode: it.ErrorCode,
		Error:     it.Error,
		Rating:    it.Rating,
		Payload:   it.Payload,
	}
	if it.Outcome != nil {
		resp.Outcome = *it.Outcome
	}
	if it.WatchedAt != nil {
		formatted := response.FormatTime(*it.WatchedAt)
		resp.WatchedAt = &formatted
	}
	if it.ProcessedAt != nil {
		formatted := response.FormatTime(*it.ProcessedAt)
		resp.ProcessedAt = &formatted
	}
	return resp
}
