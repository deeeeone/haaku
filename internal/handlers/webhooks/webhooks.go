package webhooks

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
	"trakt-api/internal/outbox"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// init — register OpenAPI route definitions
// ---------------------------------------------------------------------------

func init() {
	routes := []openapi.RouteDef{
		{
			Method:  "GET",
			Path:    "/v1/webhooks",
			Summary: "List own webhook endpoints",
			Tags:    []string{"Webhooks"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "List of webhook endpoints",
				401: "Unauthorized",
			},
		},
		{
			Method:  "POST",
			Path:    "/v1/webhooks",
			Summary: "Create a webhook endpoint",
			Tags:    []string{"Webhooks"},
			Auth:    "required",
			StatusCodes: map[int]string{
				201: "Created — secret returned once",
				400: "Invalid request (URL validation failed)",
				401: "Unauthorized",
			},
		},
		{
			Method:  "PUT",
			Path:    "/v1/webhooks/{id}",
			Summary: "Update webhook endpoint",
			Tags:    []string{"Webhooks"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Updated",
				400: "Invalid request",
				401: "Unauthorized",
				404: "Endpoint not found",
			},
		},
		{
			Method:  "DELETE",
			Path:    "/v1/webhooks/{id}",
			Summary: "Delete webhook endpoint",
			Tags:    []string{"Webhooks"},
			Auth:    "required",
			StatusCodes: map[int]string{
				204: "Deleted",
				401: "Unauthorized",
				404: "Endpoint not found",
			},
		},
		{
			Method:  "POST",
			Path:    "/v1/webhooks/{id}/rotate-secret",
			Summary: "Rotate webhook secret",
			Tags:    []string{"Webhooks"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Secret rotated — new secret returned once",
				401: "Unauthorized",
				404: "Endpoint not found",
			},
		},
		{
			Method:  "POST",
			Path:    "/v1/webhooks/{id}/test",
			Summary: "Send test ping event to webhook",
			Tags:    []string{"Webhooks"},
			Auth:    "required",
			StatusCodes: map[int]string{
				202: "Ping queued",
				401: "Unauthorized",
				404: "Endpoint not found",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/webhooks/{id}/deliveries",
			Summary: "List delivery log for webhook endpoint",
			Tags:    []string{"Webhooks"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Delivery log",
				401: "Unauthorized",
				404: "Endpoint not found",
			},
		},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// ---------------------------------------------------------------------------
// RegisterRoutes — wire webhook routes
// ---------------------------------------------------------------------------

func RegisterRoutes(api fiber.Router) {
	api.Get("/webhooks", handleListEndpoints)
	api.Post("/webhooks", handleCreateEndpoint)
	api.Put("/webhooks/:id", handleUpdateEndpoint)
	api.Delete("/webhooks/:id", handleDeleteEndpoint)
	api.Post("/webhooks/:id/rotate-secret", handleRotateSecret)
	api.Post("/webhooks/:id/test", handleTestPing)
	api.Get("/webhooks/:id/deliveries", handleDeliveries)
}

// ---------------------------------------------------------------------------
// Local error helpers (avoid import cycle with httpapi package)
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

// ---------------------------------------------------------------------------
// Request types
// ---------------------------------------------------------------------------

type createEndpointRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
	Secret string   `json:"secret,omitempty"`
}

type updateEndpointRequest struct {
	URL    *string  `json:"url,omitempty"`
	Events []string `json:"events,omitempty"`
	Active *bool    `json:"active,omitempty"`
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type endpointResponse struct {
	ID        int64     `json:"id"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type createEndpointResponse struct {
	endpointResponse
	Secret string `json:"secret"`
}

type deliveryResponse struct {
	ID           int64     `json:"id"`
	EventID      string    `json:"event_id"`
	EventType    string    `json:"event_type"`
	Status       string    `json:"status"`
	Attempts     int       `json:"attempts"`
	ResponseCode *int      `json:"response_code,omitempty"`
	DurationMs   *int      `json:"duration_ms,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func userID(c *fiber.Ctx) string {
	id, _ := c.Locals("user_id").(string)
	return id
}

func getDB() (*gorm.DB, error) {
	db, err := database.GetDB()
	if err != nil {
		return nil, err
	}
	return db, nil
}

func hashSecret(secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte("webhook-secret"))
	return hex.EncodeToString(h.Sum(nil))
}

func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func isValidHTTPS(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("URL must use HTTPS")
	}
	if parsed.Host == "" {
		return fmt.Errorf("URL must include a host")
	}
	host := parsed.Hostname()
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("cannot resolve host %q: %w", host, err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("URL resolves to a private/internal IP address — not allowed")
		}
	}
	return nil
}

func toEndpointResponse(ep *database.WebhookEndpoint) endpointResponse {
	events := make([]string, len(ep.Events))
	copy(events, ep.Events)
	return endpointResponse{
		ID:        ep.ID,
		URL:       ep.URL,
		Events:    events,
		Active:    ep.Active,
		CreatedAt: ep.CreatedAt,
		UpdatedAt: ep.UpdatedAt,
	}
}

func toDeliveryResponse(d *database.WebhookDelivery) deliveryResponse {
	return deliveryResponse{
		ID:           d.ID,
		EventID:      d.EventID,
		EventType:    d.EventType,
		Status:       d.Status,
		Attempts:     d.Attempts,
		ResponseCode: d.ResponseCode,
		DurationMs:   d.DurationMs,
		CreatedAt:    d.CreatedAt,
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// GET /v1/webhooks
func handleListEndpoints(c *fiber.Ctx) error {
	db, err := getDB()
	if err != nil {
		return badRequest(c, "database unavailable")
	}

	var endpoints []database.WebhookEndpoint
	if err := db.Where("user_id = ?", userID(c)).Order("created_at DESC").Find(&endpoints).Error; err != nil {
		return badRequest(c, "failed to list webhooks")
	}

	results := make([]endpointResponse, len(endpoints))
	for i, ep := range endpoints {
		results[i] = toEndpointResponse(&ep)
	}
	return c.JSON(results)
}

// POST /v1/webhooks
func handleCreateEndpoint(c *fiber.Ctx) error {
	var req createEndpointRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return badRequest(c, "invalid JSON body")
	}

	if req.URL == "" {
		return badRequest(c, "url is required")
	}

	if err := isValidHTTPS(req.URL); err != nil {
		return badRequest(c, err.Error())
	}

	if len(req.Events) == 0 {
		return badRequest(c, "at least one event type is required")
	}

	validEvents := ValidEventTypes()
	for _, ev := range req.Events {
		if !validEvents[ev] {
			return badRequest(c, fmt.Sprintf("unknown event type: %s", ev))
		}
	}

	secret := req.Secret
	if secret == "" {
		var err error
		secret, err = generateSecret()
		if err != nil {
			return badRequest(c, "failed to generate secret")
		}
	}

	db, err := getDB()
	if err != nil {
		return badRequest(c, "database unavailable")
	}

	ep := database.WebhookEndpoint{
		UserID:     userID(c),
		URL:        req.URL,
		Events:     database.StringSlice(req.Events),
		SecretHash: hashSecret(secret),
		Active:     true,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	if err := db.Create(&ep).Error; err != nil {
		return badRequest(c, "failed to create webhook endpoint")
	}

	resp := createEndpointResponse{
		endpointResponse: toEndpointResponse(&ep),
		Secret:           secret,
	}
	return c.Status(fiber.StatusCreated).JSON(resp)
}

// PUT /v1/webhooks/:id
func handleUpdateEndpoint(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return badRequest(c, "invalid endpoint id")
	}

	db, err := getDB()
	if err != nil {
		return badRequest(c, "database unavailable")
	}

	var ep database.WebhookEndpoint
	if err := db.Where("id = ? AND user_id = ?", id, userID(c)).First(&ep).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "webhook endpoint not found")
		}
		return badRequest(c, "database query error")
	}

	var req updateEndpointRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return badRequest(c, "invalid JSON body")
	}

	if req.URL != nil {
		if err := isValidHTTPS(*req.URL); err != nil {
			return badRequest(c, err.Error())
		}
		ep.URL = *req.URL
	}

	if req.Events != nil {
		if len(req.Events) == 0 {
			return badRequest(c, "at least one event type is required")
		}
		validEvents := ValidEventTypes()
		for _, ev := range req.Events {
			if !validEvents[ev] {
				return badRequest(c, fmt.Sprintf("unknown event type: %s", ev))
			}
		}
		ep.Events = database.StringSlice(req.Events)
	}

	if req.Active != nil {
		ep.Active = *req.Active
	}

	ep.UpdatedAt = time.Now().UTC()

	if err := db.Save(&ep).Error; err != nil {
		return badRequest(c, "failed to update webhook endpoint")
	}

	return c.JSON(toEndpointResponse(&ep))
}

// DELETE /v1/webhooks/:id
func handleDeleteEndpoint(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return badRequest(c, "invalid endpoint id")
	}

	db, err := getDB()
	if err != nil {
		return badRequest(c, "database unavailable")
	}

	result := db.Where("id = ? AND user_id = ?", id, userID(c)).Delete(&database.WebhookEndpoint{})
	if result.Error != nil {
		return badRequest(c, "failed to delete webhook endpoint")
	}
	if result.RowsAffected == 0 {
		return notFound(c, "webhook endpoint not found")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// POST /v1/webhooks/:id/rotate-secret
func handleRotateSecret(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return badRequest(c, "invalid endpoint id")
	}

	db, err := getDB()
	if err != nil {
		return badRequest(c, "database unavailable")
	}

	var ep database.WebhookEndpoint
	if err := db.Where("id = ? AND user_id = ?", id, userID(c)).First(&ep).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "webhook endpoint not found")
		}
		return badRequest(c, "database query error")
	}

	secret, err := generateSecret()
	if err != nil {
		return badRequest(c, "failed to generate secret")
	}

	ep.SecretHash = hashSecret(secret)
	ep.UpdatedAt = time.Now().UTC()

	if err := db.Save(&ep).Error; err != nil {
		return badRequest(c, "failed to rotate secret")
	}

	return c.JSON(fiber.Map{"secret": secret})
}

// POST /v1/webhooks/:id/test
func handleTestPing(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return badRequest(c, "invalid endpoint id")
	}

	db, err := getDB()
	if err != nil {
		return badRequest(c, "database unavailable")
	}

	var ep database.WebhookEndpoint
	if err := db.Where("id = ? AND user_id = ?", id, userID(c)).First(&ep).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "webhook endpoint not found")
		}
		return badRequest(c, "database query error")
	}

	payload := map[string]interface{}{
		"ping":    true,
		"sent_to": ep.URL,
	}
	if err := outbox.PublishEvent(db, "ping", userID(c), payload); err != nil {
		return badRequest(c, "failed to queue ping event")
	}

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "queued"})
}

// GET /v1/webhooks/:id/deliveries
func handleDeliveries(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return badRequest(c, "invalid endpoint id")
	}

	db, err := getDB()
	if err != nil {
		return badRequest(c, "database unavailable")
	}

	var ep database.WebhookEndpoint
	if err := db.Where("id = ? AND user_id = ?", id, userID(c)).First(&ep).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "webhook endpoint not found")
		}
		return badRequest(c, "database query error")
	}

	var deliveries []database.WebhookDelivery
	if err := db.Where("endpoint_id = ?", id).Order("created_at DESC").Limit(100).Find(&deliveries).Error; err != nil {
		return badRequest(c, "failed to list deliveries")
	}

	results := make([]deliveryResponse, len(deliveries))
	for i, d := range deliveries {
		results[i] = toDeliveryResponse(&d)
	}
	return c.JSON(results)
}

// ---------------------------------------------------------------------------
// Valid event types (matching outbox constants)
// ---------------------------------------------------------------------------

func ValidEventTypes() map[string]bool {
	return map[string]bool{
		outbox.EventHistoryAdded:     true,
		outbox.EventHistoryRemoved:   true,
		outbox.EventCollectionAdded:  true,
		outbox.EventWatchlistAdded:   true,
		outbox.EventWatchlistRemoved: true,
		outbox.EventRatingAdded:      true,
		outbox.EventFavoriteAdded:    true,
		outbox.EventScrobbleStart:    true,
		outbox.EventScrobblePause:    true,
		outbox.EventScrobbleStop:     true,
		outbox.EventCheckinCreated:   true,
		outbox.EventCheckinDeleted:   true,
		outbox.EventUserUpdated:      true,
		outbox.EventListCreated:      true,
		outbox.EventListUpdated:      true,
		outbox.EventListDeleted:      true,
		outbox.EventListItemsAdded:   true,
		outbox.EventListItemsRemoved: true,
		"ping":                       true,
	}
}

func GenerateHMAC(secret string, timestamp int64, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.%s", timestamp, body)))
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifyHMAC(secret string, timestamp int64, body, signature string) bool {
	expected := GenerateHMAC(secret, timestamp, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// ---------------------------------------------------------------------------
// Shared helpers for webhook worker
// ---------------------------------------------------------------------------

var EventCatalogV1 = map[string]string{
	outbox.EventHistoryAdded:     "history.added",
	outbox.EventHistoryRemoved:   "history.removed",
	outbox.EventCollectionAdded:  "collection.updated",
	outbox.EventWatchlistAdded:   "watchlist.added",
	outbox.EventWatchlistRemoved: "watchlist.removed",
	outbox.EventRatingAdded:      "rating.added",
	outbox.EventFavoriteAdded:    "favorite.added",
	outbox.EventScrobbleStart:    "scrobble.start",
	outbox.EventScrobblePause:    "scrobble.pause",
	outbox.EventScrobbleStop:     "scrobble.stop",
	outbox.EventCheckinCreated:   "checkin.created",
	outbox.EventCheckinDeleted:   "checkin.deleted",
	outbox.EventUserUpdated:      "user.updated",
	outbox.EventListCreated:      "list.created",
	outbox.EventListUpdated:      "list.updated",
	outbox.EventListDeleted:      "list.deleted",
	outbox.EventListItemsAdded:   "list.items.added",
	outbox.EventListItemsRemoved: "list.items.removed",
	"ping":                       "ping",
}

type WebhookPayload struct {
	EventID    string                 `json:"event_id"`
	Type       string                 `json:"type"`
	CreatedAt  string                 `json:"created_at"`
	Data       map[string]interface{} `json:"data"`
	APIVersion int                    `json:"api_version"`
}

func NewWebhookPayload(eventID, eventType string, data map[string]interface{}) WebhookPayload {
	return WebhookPayload{
		EventID:    uuid.New().String(),
		Type:       eventType,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Data:       data,
		APIVersion: 1,
	}
}
