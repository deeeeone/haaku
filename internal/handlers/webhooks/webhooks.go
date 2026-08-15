package webhooks

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	"trakt-api/internal/auth"
	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
	"trakt-api/internal/outbox"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"trakt-api/internal/response"
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
	api.Get("/webhooks", auth.AuthRequired(), handleListEndpoints)
	api.Post("/webhooks", auth.AuthRequired(), handleCreateEndpoint)
	api.Put("/webhooks/:id", auth.AuthRequired(), handleUpdateEndpoint)
	api.Delete("/webhooks/:id", auth.AuthRequired(), handleDeleteEndpoint)
	api.Post("/webhooks/:id/rotate-secret", auth.AuthRequired(), handleRotateSecret)
	api.Post("/webhooks/:id/test", auth.AuthRequired(), handleTestPing)
	api.Get("/webhooks/:id/deliveries", auth.AuthRequired(), handleDeliveries)
}

// ---------------------------------------------------------------------------
// Local error helpers (avoid import cycle with httpapi package)
// ---------------------------------------------------------------------------

// requireUserID returns the authenticated user ID or writes a 401 when the
// auth middleware did not set one (defense in depth — routes also run
// auth.AuthRequired).
func requireUserID(c *fiber.Ctx) (string, error) {
	uid, ok := c.Locals("user_id").(string)
	if !ok || uid == "" {
		return "", response.Unauthorized(c)
	}
	return uid, nil
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

func getDB() (*gorm.DB, error) {
	db, err := database.GetDB()
	if err != nil {
		return nil, err
	}
	return db, nil
}

// SecretKeyFromEnv returns the AES-256 key used to encrypt webhook secrets at
// rest. Required: WEBHOOK_SECRET_KEY (32 bytes, hex-encoded).
func SecretKeyFromEnv() ([]byte, error) {
	raw := os.Getenv("WEBHOOK_SECRET_KEY")
	if raw == "" {
		return nil, errors.New("WEBHOOK_SECRET_KEY is not set")
	}
	key, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("WEBHOOK_SECRET_KEY must be hex-encoded: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("WEBHOOK_SECRET_KEY must be 32 bytes (64 hex chars)")
	}
	return key, nil
}

// EncryptSecret encrypts a webhook secret at rest with AES-256-GCM. Output is
// base64(nonce || ciphertext). The stored value must be recoverable so the
// delivery worker can sign requests with the plaintext consumers verify.
func EncryptSecret(secret string) (string, error) {
	key, err := SecretKeyFromEnv()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(secret), nil)), nil
}

// DecryptSecret reverses EncryptSecret.
func DecryptSecret(stored string) (string, error) {
	key, err := SecretKeyFromEnv()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", fmt.Errorf("webhook secret is not a valid encrypted blob: %w", err)
	}
	if len(raw) < aead.NonceSize() {
		return "", errors.New("webhook secret blob is too short")
	}
	nonce, ct := raw[:aead.NonceSize()], raw[aead.NonceSize():]
	pt, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("webhook secret decryption failed (key changed or legacy value): %w", err)
	}
	return string(pt), nil
}

func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// isBlockedIP reports whether ip must never be dialed (internal/private/
// special-use ranges). Used by both endpoint validation and the delivery worker.
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// ResolvePublicIP verifies every resolved address for host is public and
// returns the first one. Callers must dial this exact IP (pinned) rather than
// re-resolving, otherwise a DNS-rebinding attacker could swap in an internal
// address after the check.
func ResolvePublicIP(host string) (net.IP, error) {
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses found for host %q", host)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("host %q resolves to a private/internal IP address — not allowed", host)
		}
	}
	return ips[0], nil
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
	if parsed.User != nil {
		return fmt.Errorf("URL must not include userinfo")
	}
	if parsed.Opaque != "" {
		return fmt.Errorf("invalid URL form")
	}
	if _, err := ResolvePublicIP(parsed.Hostname()); err != nil {
		return err
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
	uid, err := requireUserID(c)
	if err != nil {
		return err
	}

	db, err := getDB()
	if err != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	var endpoints []database.WebhookEndpoint
	if err := db.Where("user_id = ?", uid).Order("created_at DESC").Find(&endpoints).Error; err != nil {
		return response.InternalError(c, "failed to list webhooks")
	}

	results := make([]endpointResponse, len(endpoints))
	for i, ep := range endpoints {
		results[i] = toEndpointResponse(&ep)
	}
	return c.JSON(results)
}

// POST /v1/webhooks
func handleCreateEndpoint(c *fiber.Ctx) error {
	uid, err := requireUserID(c)
	if err != nil {
		return err
	}

	var req createEndpointRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	if req.URL == "" {
		return response.BadRequest(c, "url is required")
	}

	if err := isValidHTTPS(req.URL); err != nil {
		return response.BadRequest(c, err.Error())
	}

	if len(req.Events) == 0 {
		return response.BadRequest(c, "at least one event type is required")
	}

	validEvents := ValidEventTypes()
	for _, ev := range req.Events {
		if !validEvents[ev] {
			return response.BadRequest(c, fmt.Sprintf("unknown event type: %s", ev))
		}
	}

	secret := req.Secret
	if secret == "" {
		var err error
		secret, err = generateSecret()
		if err != nil {
			return response.InternalError(c, "failed to generate secret")
		}
	}

	db, err := getDB()
	if err != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	encSecret, err := EncryptSecret(secret)
	if err != nil {
		return response.InternalError(c, "failed to store webhook secret securely")
	}

	ep := database.WebhookEndpoint{
		UserID:     uid,
		URL:        req.URL,
		Events:     database.StringSlice(req.Events),
		SecretHash: encSecret,
		Active:     true,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	if err := db.Create(&ep).Error; err != nil {
		return response.InternalError(c, "failed to create webhook endpoint")
	}

	resp := createEndpointResponse{
		endpointResponse: toEndpointResponse(&ep),
		Secret:           secret,
	}
	return c.Status(fiber.StatusCreated).JSON(resp)
}

// PUT /v1/webhooks/:id
func handleUpdateEndpoint(c *fiber.Ctx) error {
	uid, err := requireUserID(c)
	if err != nil {
		return err
	}

	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return response.BadRequest(c, "invalid endpoint id")
	}

	db, err := getDB()
	if err != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	var ep database.WebhookEndpoint
	if err := db.Where("id = ? AND user_id = ?", id, uid).First(&ep).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "webhook endpoint not found")
		}
		return response.InternalError(c, "database query error")
	}

	var req updateEndpointRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	if req.URL != nil {
		if err := isValidHTTPS(*req.URL); err != nil {
			return response.BadRequest(c, err.Error())
		}
		ep.URL = *req.URL
	}

	if req.Events != nil {
		if len(req.Events) == 0 {
			return response.BadRequest(c, "at least one event type is required")
		}
		validEvents := ValidEventTypes()
		for _, ev := range req.Events {
			if !validEvents[ev] {
				return response.BadRequest(c, fmt.Sprintf("unknown event type: %s", ev))
			}
		}
		ep.Events = database.StringSlice(req.Events)
	}

	if req.Active != nil {
		ep.Active = *req.Active
	}

	ep.UpdatedAt = time.Now().UTC()

	if err := db.Save(&ep).Error; err != nil {
		return response.InternalError(c, "failed to update webhook endpoint")
	}

	return c.JSON(toEndpointResponse(&ep))
}

// DELETE /v1/webhooks/:id
func handleDeleteEndpoint(c *fiber.Ctx) error {
	uid, err := requireUserID(c)
	if err != nil {
		return err
	}

	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return response.BadRequest(c, "invalid endpoint id")
	}

	db, err := getDB()
	if err != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	result := db.Where("id = ? AND user_id = ?", id, uid).Delete(&database.WebhookEndpoint{})
	if result.Error != nil {
		return response.InternalError(c, "failed to delete webhook endpoint")
	}
	if result.RowsAffected == 0 {
		return response.NotFound(c, "webhook endpoint not found")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// POST /v1/webhooks/:id/rotate-secret
func handleRotateSecret(c *fiber.Ctx) error {
	uid, err := requireUserID(c)
	if err != nil {
		return err
	}

	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return response.BadRequest(c, "invalid endpoint id")
	}

	db, err := getDB()
	if err != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	var ep database.WebhookEndpoint
	if err := db.Where("id = ? AND user_id = ?", id, uid).First(&ep).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "webhook endpoint not found")
		}
		return response.InternalError(c, "database query error")
	}

	secret, err := generateSecret()
	if err != nil {
		return response.InternalError(c, "failed to generate secret")
	}

	encSecret, err := EncryptSecret(secret)
	if err != nil {
		return response.InternalError(c, "failed to store webhook secret securely")
	}

	ep.SecretHash = encSecret
	ep.UpdatedAt = time.Now().UTC()

	if err := db.Save(&ep).Error; err != nil {
		return response.InternalError(c, "failed to rotate secret")
	}

	return c.JSON(fiber.Map{"secret": secret})
}

// POST /v1/webhooks/:id/test
func handleTestPing(c *fiber.Ctx) error {
	uid, err := requireUserID(c)
	if err != nil {
		return err
	}

	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return response.BadRequest(c, "invalid endpoint id")
	}

	db, err := getDB()
	if err != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	var ep database.WebhookEndpoint
	if err := db.Where("id = ? AND user_id = ?", id, uid).First(&ep).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "webhook endpoint not found")
		}
		return response.InternalError(c, "database query error")
	}

	payload := map[string]interface{}{
		"ping":    true,
		"sent_to": ep.URL,
	}
	if err := outbox.PublishEvent(db, "ping", uid, payload); err != nil {
		return response.InternalError(c, "failed to queue ping event")
	}

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "queued"})
}

// GET /v1/webhooks/:id/deliveries
func handleDeliveries(c *fiber.Ctx) error {
	uid, err := requireUserID(c)
	if err != nil {
		return err
	}

	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return response.BadRequest(c, "invalid endpoint id")
	}

	db, err := getDB()
	if err != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	var ep database.WebhookEndpoint
	if err := db.Where("id = ? AND user_id = ?", id, uid).First(&ep).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "webhook endpoint not found")
		}
		return response.InternalError(c, "database query error")
	}

	// Pagination per api-conventions.md §7 (default 25, max 100).
	page := response.QueryInt(c, "page", 1)
	if page < 1 {
		page = 1
	}
	limit := response.ClampLimit(response.QueryInt(c, "limit", 25), 1, 100)

	var total int64
	if err := db.Model(&database.WebhookDelivery{}).Where("endpoint_id = ?", id).Count(&total).Error; err != nil {
		return response.InternalError(c, "failed to list deliveries")
	}

	var deliveries []database.WebhookDelivery
	if err := db.Where("endpoint_id = ?", id).
		Order("created_at DESC, id DESC"). // stable order for pagination
		Offset((page - 1) * limit).Limit(limit).
		Find(&deliveries).Error; err != nil {
		return response.InternalError(c, "failed to list deliveries")
	}

	results := make([]deliveryResponse, len(deliveries))
	for i, d := range deliveries {
		results[i] = toDeliveryResponse(&d)
	}

	totalPages := int64(1)
	if total > 0 {
		totalPages = (total + int64(limit) - 1) / int64(limit)
	}
	c.Set("X-Pagination-Page", strconv.Itoa(page))
	c.Set("X-Pagination-Limit", strconv.Itoa(limit))
	c.Set("X-Pagination-Total-Pages", strconv.FormatInt(totalPages, 10))
	c.Set("X-Pagination-Total-Items", strconv.FormatInt(total, 10))
	c.Set("X-Pagination-Items-Count", strconv.Itoa(len(results)))
	if page < int(totalPages) {
		c.Set("Link", fmt.Sprintf(`<%s?page=%d>; rel="next"`, c.Path(), page+1))
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

func NewWebhookPayload(eventType string, data map[string]interface{}) WebhookPayload {
	return WebhookPayload{
		EventID:    uuid.New().String(),
		Type:       eventType,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Data:       data,
		APIVersion: 1,
	}
}
