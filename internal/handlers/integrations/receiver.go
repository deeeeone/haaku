package integrations

import (
	"encoding/json"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
	"trakt-api/internal/outbox"

	"github.com/gofiber/fiber/v2"
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
		return response.BadRequest(c, "database unavailable")
	}

	tok, err := LookupToken(db, tokenStr)
	if err != nil {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or expired token")
	}
	if tok == nil || !tok.Active {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or expired token")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return response.BadRequest(c, "invalid JSON payload")
	}

	if payload["event"] == nil && payload["type"] == nil {
		return response.BadRequest(c, "payload must contain an 'event' or 'type' field")
	}

	if err := outbox.PublishEvent(db, "integration.received", tok.UserID, payload); err != nil {
		return response.BadRequest(c, "failed to queue event")
	}

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "accepted"})
}
