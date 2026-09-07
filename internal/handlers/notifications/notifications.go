package notifications

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	"trakt-api/internal/cache"
	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
	"trakt-api/redis"

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

	// Fetch notifications: unread first, then by created_at desc. A stable
	// id tiebreaker keeps pagination deterministic within each read-status
	// group (offset pagination stays consistent with the rest of the API).
	var notifications []database.Notification
	db.Where("user_id = ?", userID).
		Order("read ASC, created_at DESC, id DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&notifications)

	data := make([]notificationItem, len(notifications))
	for i, n := range notifications {
		data[i] = toNotificationItem(&n)
	}

	setPaginationHeaders(c, page, limit, totalCount, len(data))

	return c.JSON(paginatedNotifResponse{
		Data:        data,
		Page:        page,
		Limit:       limit,
		TotalCount:  totalCount,
		UnreadCount: unreadCount,
	})
}

// setPaginationHeaders emits the §7 pagination headers (api-conventions.md)
// plus a Link: rel="next" header when more pages exist.
func setPaginationHeaders(c *fiber.Ctx, page, limit int, total int64, itemsCount int) {
	totalPages := int64(0)
	if total > 0 {
		totalPages = (total + int64(limit) - 1) / int64(limit)
	}
	c.Set("X-Pagination-Page", strconv.Itoa(page))
	c.Set("X-Pagination-Limit", strconv.Itoa(limit))
	c.Set("X-Pagination-Total-Pages", strconv.FormatInt(totalPages, 10))
	c.Set("X-Pagination-Total-Items", strconv.FormatInt(total, 10))
	c.Set("X-Pagination-Items-Count", strconv.Itoa(itemsCount))
	if page < int(totalPages) {
		c.Set("Link", `<`+c.Path()+`?page=`+strconv.Itoa(page+1)+`&limit=`+strconv.Itoa(limit)+`>; rel="next"`)
	}
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

// Notification type constants — the canonical inbox Type values used by
// CreateNotification and the inbox UI/settings (follows/replies/list_invite).
// The types mirror the spec's inbox categories (social-api.md: replies, follow
// requests, list invites, import done, mentions).
const (
	NotifTypeFollow        = "follow"
	NotifTypeReply         = "reply"
	NotifTypeListInvite    = "list_invite"
	NotifTypeImportDone    = "import_complete"
)

// prefKeyForType maps a notification type to the recipient-settings toggle that
// governs it (see parseNotifSettings). Types with no dedicated toggle (e.g.
// mentions) return "" so they are always allowed.
func prefKeyForType(notifType string) string {
	switch notifType {
	case NotifTypeFollow:
		return "follows"
	case NotifTypeReply:
		return "replies"
	case NotifTypeListInvite:
		return "list_invite"
	case NotifTypeImportDone:
		return "import_complete"
	}
	return ""
}

// recipientEnabled reports whether the recipient's notification settings allow
// a notification of the given type. Reads the user row's settings JSON (which
// lives on `users.settings`). Best-effort: on a read error it defaults to
// enabled (true) so a settings hiccup never silently swallows a notification.
func recipientEnabled(db *gorm.DB, userID, notifType string) bool {
	key := prefKeyForType(notifType)
	if key == "" {
		return true // no toggle governs this type — always allowed
	}

	var user database.User
	if err := db.Select("settings").First(&user, "id = ?", userID).Error; err != nil {
		return true
	}
	settings := parseNotifSettings(user.Settings)

	switch key {
	case "follows":
		return settings.Follows
	case "replies":
		return settings.Replies
	case "list_invite":
		return settings.ListInvite
	case "import_complete":
		return settings.ImportComplete
	}
	return true
}

// CreateNotification creates a notification for a user. It must be called from
// within a `cache.WithCacheTransaction` (DB-commit-first) flow: pass the active
// transaction `db` and the caller's `cacheTx` so the insert commits atomically
// with the surrounding write and the recipient's user version counter is queued
// for a bump (invalidates the cached inbox in O(1)). Notification writes never
// bypass version counters or open their own transaction.
//
// The recipient's notification preferences are respected: if the toggle for
// this notification's type (follows/replies/list_invite/import_complete) is
// disabled, the notification is silently skipped (returns nil) — the underlying
// social action still happens, the user just gets no inbox entry.
func CreateNotification(db *gorm.DB, cacheTx *cache.CacheTransaction, userID, notifType, actorID string, payload map[string]interface{}) error {
	if !recipientEnabled(db, userID, notifType) {
		return nil
	}

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

	if err := db.Create(&notif).Error; err != nil {
		return err
	}

	if cacheTx != nil {
		uvk := redis.BuildUserVersionKey(userID)
		cacheTx.AddInvalidation(uvk, map[string]interface{}{
			"user_id":      userID,
			"version_key":  uvk,
			"version_only": true,
		})
	}
	return nil
}
