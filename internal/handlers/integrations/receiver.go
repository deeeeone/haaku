package integrations

import (
	"encoding/json"
	"errors"
	"fmt"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
	"trakt-api/internal/outbox"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"trakt-api/internal/response"
)

// ---------------------------------------------------------------------------
// init — register OpenAPI route definition for the receiver
// ---------------------------------------------------------------------------

func init() {
	openapi.Register(openapi.RouteDef{
		Method:  "POST",
		Path:    "/v1/integrations/{token}",
		Summary: "Receive inbound integration event",
		Tags:    []string{"Integrations"},
		Auth:    "none",
		StatusCodes: map[int]string{
			202: "Event accepted for processing",
			400: "Invalid payload shape",
			401: "Invalid token",
		},
	})
}

// RegisterReceiverRoutes wires the inbound receiver route.
func RegisterReceiverRoutes(api fiber.Router) {
	api.Post("/integrations/:token", handleReceiveEvent)
}

// POST /v1/integrations/:token
func handleReceiveEvent(c *fiber.Ctx) error {
	tokenStr := c.Params("token")
	if tokenStr == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or expired token")
	}

	db, err := database.GetDB()
	if err != nil {
		fmt.Printf("integrations: receiver database unavailable: %v\n", err)
		return response.ServiceUnavailable(c, "database unavailable")
	}

	tok, err := LookupToken(db, tokenStr)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Token genuinely unknown/inactive → invalid credentials.
			return response.Unauthorized(c)
		}
		// A DB failure during lookup is a server-side fault, not bad credentials.
		fmt.Printf("integrations: receiver token lookup failed: %v\n", err)
		return response.InternalError(c)
	}
	if tok == nil || !tok.Active {
		return response.Unauthorized(c)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return response.BadRequest(c, "invalid JSON payload")
	}

	if payload["event"] == nil && payload["type"] == nil {
		return response.BadRequest(c, "payload must contain an 'event' or 'type' field")
	}

	if err := outbox.PublishEvent(db, "integration.received", tok.UserID, payload); err != nil {
		fmt.Printf("integrations: receiver publish event failed: %v\n", err)
		return response.InternalError(c)
	}

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "accepted"})
}
