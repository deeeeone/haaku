package notifications

import (
	"bytes"
	"encoding/json"
	"time"

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
		{Method: "GET", Path: "/v1/notifications", Summary: "Get notification inbox (unread-first)", Tags: []string{"Notifications"}, Auth: "required", StatusCodes: map[int]string{200: "Notifications", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/notifications/read", Summary: "Mark notifications as read", Tags: []string{"Notifications"}, Auth: "required", StatusCodes: map[int]string{200: "Marked read", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/notifications/settings", Summary: "Get notification settings", Tags: []string{"Notifications"}, Auth: "required", StatusCodes: map[int]string{200: "Settings", 401: "Unauthorized"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires all notification route handlers.
func RegisterRoutes(api fiber.Router) {
	ng := api.Group("/notifications")
	ng.Get("/", auth.AuthRequired(), handleGetInbox)
	ng.Post("/read", auth.AuthRequired(), handleMarkRead)
	ng.Get("/settings", auth.AuthRequired(), handleGetSettings)
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type notificationItem struct {
	ID        int64                  `json:"id"`
	UserID    string                 `json:"user_id"`
	Type      string                 `json:"type"`
	ActorID   string                 `json:"actor_id,omitempty"`
	Payload   map[string]interface{} `json:"payload"`
	Read      bool                   `json:"read"`
	CreatedAt string                 `json:"created_at"`
}

type paginatedNotifResponse struct {
	Data        []notificationItem `json:"data"`
	Page        int                `json:"page"`
	Limit       int                `json:"limit"`
	TotalCount  int64              `json:"total_count"`
	UnreadCount int64              `json:"unread_count"`
}

type markReadRequest struct {
	IDs []int64 `json:"ids"`
}

type notifSettingsResponse struct {
	Follows        bool `json:"follows"`
	Replies        bool `json:"replies"`
	ListInvite     bool `json:"list_invite"`
	ImportComplete bool `json:"import_complete"`
}

func toNotificationItem(n *database.Notification) notificationItem {
	payload := make(map[string]interface{})
	if n.Payload != nil {
		for k, v := range n.Payload {
			payload[k] = v
		}
	}
	return notificationItem{
		ID:        n.ID,
		UserID:    n.UserID,
		Type:      n.Type,
		ActorID:   n.ActorID,
		Payload:   payload,
		Read:      n.Read,
		CreatedAt: response.FormatTime(n.CreatedAt),
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// GET /v1/notifications
func handleGetInbox(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	page := response.QueryInt(c, "page", 1)
	limit := response.ClampLimit(response.QueryInt(c, "limit", 20), 1, 100)

	// Unread count
	var unreadCount int64
	db.Model(&database.Notification{}).
		Where("user_id = ? AND read = false", userID).
		Count(&unreadCount)

	// Total count
	var totalCount int64
	db.Model(&database.Notification{}).
		Where("user_id = ?", userID).
		Count(&totalCount)

	// Fetch notifications: unread first, then by created_at desc
	var notifications []database.Notification
	db.Where("user_id = ?", userID).
		Order("read ASC, created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&notifications)

	data := make([]notificationItem, len(notifications))
	for i, n := range notifications {
		data[i] = toNotificationItem(&n)
	}

	return c.JSON(paginatedNotifResponse{
		Data:        data,
		Page:        page,
		Limit:       limit,
		TotalCount:  totalCount,
		UnreadCount: unreadCount,
	})
}

// POST /v1/notifications/read
func handleMarkRead(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var req markReadRequest
	body := c.Body()
	if len(bytes.TrimSpace(body)) > 0 {
		// A non-empty body must be valid JSON; a client typo should not
		// silently escalate into "mark everything read".
		if err := json.Unmarshal(body, &req); err != nil {
			return response.BadRequest(c, "invalid JSON body")
		}
	}
	// An empty/whitespace-only body means "mark ALL as read".

	if len(req.IDs) > 0 {
		if dbErr := db.Model(&database.Notification{}).
			Where("id IN ? AND user_id = ?", req.IDs, userID).
			Update("read", true).Error; dbErr != nil {
			return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to mark as read")
		}
	} else {
		if dbErr := db.Model(&database.Notification{}).
			Where("user_id = ? AND read = false", userID).
			Update("read", true).Error; dbErr != nil {
			return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to mark as read")
		}
	}

	return c.JSON(fiber.Map{"status": "ok"})
}

// GET /v1/notifications/settings
func handleGetSettings(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var user database.User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
		return response.WriteError(c, fiber.StatusNotFound, "not_found", "user not found")
	}

	settings := parseNotifSettings(user.Settings)
	return c.JSON(settings)
}

// ---------------------------------------------------------------------------
// Settings and helpers
// ---------------------------------------------------------------------------

func parseNotifSettings(rawSettings string) notifSettingsResponse {
	settings := notifSettingsResponse{
		Follows:        true,
		Replies:        true,
		ListInvite:     true,
		ImportComplete: true,
	}

	if rawSettings == "" || rawSettings == "{}" {
		return settings
	}

	type parsedSettings struct {
		Notifications *struct {
			Follows        *bool `json:"follows"`
			Replies        *bool `json:"replies"`
			ListInvite     *bool `json:"list_invite"`
			ImportComplete *bool `json:"import_complete"`
		} `json:"notifications"`
	}

	var parsed parsedSettings
	if err := json.Unmarshal([]byte(rawSettings), &parsed); err != nil {
		return settings
	}

	if parsed.Notifications != nil {
		if parsed.Notifications.Follows != nil {
			settings.Follows = *parsed.Notifications.Follows
		}
		if parsed.Notifications.Replies != nil {
			settings.Replies = *parsed.Notifications.Replies
		}
		if parsed.Notifications.ListInvite != nil {
			settings.ListInvite = *parsed.Notifications.ListInvite
		}
		if parsed.Notifications.ImportComplete != nil {
			settings.ImportComplete = *parsed.Notifications.ImportComplete
		}
	}

	return settings
}

// CreateNotification creates a notification for a user. Called inline from
// other handlers when an event occurs (e.g., follow, reply, list invite).
func CreateNotification(db *gorm.DB, userID, notifType, actorID string, payload map[string]interface{}) error {
	payloadMap := database.JSONMap{}
	if payload != nil {
		for k, v := range payload {
			payloadMap[k] = v
		}
	}

	notif := database.Notification{
		UserID:    userID,
		Type:      notifType,
		ActorID:   actorID,
		Payload:   payloadMap,
		Read:      false,
		CreatedAt: time.Now(),
	}

	return db.Create(&notif).Error
}
