package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Webhook types
// ---------------------------------------------------------------------------

// ClerkWebhookEvent is the Clerk webhook event payload.
type ClerkWebhookEvent struct {
	Data   ClerkUser `json:"data"`
	Object string    `json:"object"`
	Type   string    `json:"type"`
}

// ---------------------------------------------------------------------------
// Route registration (init-time for OpenAPI)
// ---------------------------------------------------------------------------

func init() {
	openapi.Register(openapi.RouteDef{
		Method:  "POST",
		Path:    "/v1/webhooks/clerk",
		Summary: "Receive Clerk webhook events (user.created, user.updated, user.deleted)",
		Tags:    []string{"Webhooks"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "Webhook event processed",
			400: "Invalid payload or signature",
			401: "Invalid webhook signature",
		},
	})
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// HandleClerkWebhook is the Fiber handler for POST /v1/webhooks/clerk.
// It verifies the Svix signature and processes Clerk user events.
func HandleClerkWebhook(c *fiber.Ctx) error {
	clerk := GetClerk()
	if clerk == nil {
		slog.Warn("webhook: Clerk not initialized")
		return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "service_unavailable",
				"message": "Clerk not configured",
			},
		})
	}

	// Read raw body for signature verification
	rawBody := c.Body()

	// Verify Svix webhook signature
	if err := verifySvixSignature(clerk.SecretKey, c, rawBody); err != nil {
		slog.Warn("webhook: invalid signature", "error", err)
		return c.Status(http.StatusUnauthorized).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "unauthorized",
				"message": "invalid webhook signature",
			},
		})
	}

	// Parse webhook payload
	var event ClerkWebhookEvent
	if err := json.Unmarshal(rawBody, &event); err != nil {
		slog.Warn("webhook: failed to parse body", "error", err)
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "invalid_request",
				"message": "invalid webhook payload",
			},
		})
	}

	// Process event
	if err := handleWebhookEvent(&event); err != nil {
		slog.Error("webhook: event processing failed", "type", event.Type, "user_id", event.Data.ID, "error", err)
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{
			"error": fiber.Map{
				"code":    "internal",
				"message": "failed to process webhook event",
			},
		})
	}

	return c.JSON(fiber.Map{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Event processing
// ---------------------------------------------------------------------------

func handleWebhookEvent(event *ClerkWebhookEvent) error {
	db, err := database.GetDB()
	if err != nil {
		return fmt.Errorf("database unavailable: %w", err)
	}

	switch event.Type {
	case "user.created":
		return handleUserCreated(db, &event.Data)
	case "user.updated":
		return handleUserUpdated(db, &event.Data)
	case "user.deleted":
		return handleUserDeleted(db, &event.Data)
	default:
		slog.Debug("webhook: unhandled event type", "type", event.Type)
		return nil // Not an error — just not handled
	}
}

func handleUserCreated(db *gorm.DB, cu *ClerkUser) error {
	slog.Info("webhook: user.created", "user_id", cu.ID)
	return upsertUserFromClerk(db, cu)
}

func handleUserUpdated(db *gorm.DB, cu *ClerkUser) error {
	slog.Info("webhook: user.updated", "user_id", cu.ID)
	return upsertUserFromClerk(db, cu)
}

func handleUserDeleted(db *gorm.DB, cu *ClerkUser) error {
	slog.Info("webhook: user.deleted", "user_id", cu.ID)
	return db.Model(&database.User{}).
		Where("id = ?", cu.ID).
		Update("status", "deleted").Error
}

// ---------------------------------------------------------------------------
// Svix signature verification
// ---------------------------------------------------------------------------

// verifySvixSignature verifies the Svix-compatible webhook signature headers.
// Headers used: svix-id, svix-timestamp, svix-signature.
// The signing secret is the Clerk secret key's raw bytes.
func verifySvixSignature(secret string, c *fiber.Ctx, body []byte) error {
	svixID := c.Get("svix-id")
	svixTimestamp := c.Get("svix-timestamp")
	svixSignature := c.Get("svix-signature")

	if svixID == "" || svixTimestamp == "" || svixSignature == "" {
		return fmt.Errorf("missing svix headers")
	}

	// Parse signatures (comma-separated: "v1,<base64>,v1,<base64>")
	parts := splitSvixSignatures(svixSignature)
	if len(parts) == 0 {
		return fmt.Errorf("no signatures in svix-signature header")
	}

	// Build the content to sign
	signedContent := fmt.Sprintf("%s.%s.%s", svixID, svixTimestamp, string(body))

	// Compute HMAC-SHA256 with the secret
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedContent))
	expectedSig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	// Check each signature
	for _, sig := range parts {
		if len(sig) < 3 {
			continue // Skip malformed
		}
		// Svix signature format: "v1,<base64_hmac>"
		sigParts := strings.SplitN(sig, ",", 2)
		if len(sigParts) != 2 {
			continue
		}
		if sigParts[1] == expectedSig {
			return nil // Valid
		}
	}

	return fmt.Errorf("signature mismatch")
}

// splitSvixSignatures splits the Svix signature header into individual signatures.
// Svix sends signatures as space-separated: "v1,sig1 v1,sig2"
func splitSvixSignatures(header string) []string {
	// The header value is space-separated signature groups
	parts := strings.Split(header, " ")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
