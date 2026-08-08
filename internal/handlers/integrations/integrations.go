package integrations

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"trakt-api/internal/auth"
	"trakt-api/internal/database"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// init — register OpenAPI route definitions
// ---------------------------------------------------------------------------

func init() {
	routes := []openapi.RouteDef{
		{
			Method:  "POST",
			Path:    "/v1/integrations",
			Summary: "Create integration token",
			Tags:    []string{"Integrations"},
			Auth:    "required",
			StatusCodes: map[int]string{
				201: "Token created — token returned once",
				400: "Invalid request",
				401: "Unauthorized",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/integrations",
			Summary: "List own integration tokens",
			Tags:    []string{"Integrations"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "List of tokens (token values redacted)",
				401: "Unauthorized",
			},
		},
		{
			Method:  "DELETE",
			Path:    "/v1/integrations/{id}",
			Summary: "Revoke integration token",
			Tags:    []string{"Integrations"},
			Auth:    "required",
			StatusCodes: map[int]string{
				204: "Token revoked",
				401: "Unauthorized",
				404: "Token not found",
			},
		},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// ---------------------------------------------------------------------------
// RegisterRoutes — wire integration routes
// ---------------------------------------------------------------------------

func RegisterRoutes(api fiber.Router) {
	api.Post("/integrations", auth.AuthRequired(), handleCreateToken)
	api.Get("/integrations", auth.AuthRequired(), handleListTokens)
	api.Delete("/integrations/:id", auth.AuthRequired(), handleRevokeToken)
}

// ---------------------------------------------------------------------------
// Local error helpers (avoid import cycle with httpapi)
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

func unauthorized(c *fiber.Ctx) error {
	return writeError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type createTokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes,omitempty"`
}

type tokenResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Scopes    []string  `json:"scopes"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

type createTokenResponse struct {
	tokenResponse
	Token string `json:"token"`
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

func generateToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "tk_" + hex.EncodeToString(b), nil
}

func toTokenResponse(t *database.IntegrationToken) tokenResponse {
	scopes := make([]string, len(t.Scopes))
	copy(scopes, t.Scopes)
	return tokenResponse{
		ID:        t.ID,
		Name:      t.Name,
		Scopes:    scopes,
		Active:    t.Active,
		CreatedAt: t.CreatedAt,
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// POST /v1/integrations
func handleCreateToken(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return unauthorized(c)
	}

	var req createTokenRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return badRequest(c, "invalid JSON body")
	}

	if req.Name == "" {
		return badRequest(c, "name is required")
	}

	tokenStr, err := generateToken()
	if err != nil {
		return badRequest(c, "failed to generate token")
	}

	db, err := getDB()
	if err != nil {
		return badRequest(c, "database unavailable")
	}

	scopes := database.StringSlice(req.Scopes)
	if len(scopes) == 0 {
		scopes = database.StringSlice{"*"}
	}

	tok := database.IntegrationToken{
		UserID:    uid,
		Token:     tokenStr,
		Name:      req.Name,
		Scopes:    scopes,
		Active:    true,
		CreatedAt: time.Now().UTC(),
	}

	if err := db.Create(&tok).Error; err != nil {
		return badRequest(c, "failed to create integration token")
	}

	resp := createTokenResponse{
		tokenResponse: toTokenResponse(&tok),
		Token:         tokenStr,
	}
	return c.Status(fiber.StatusCreated).JSON(resp)
}

// GET /v1/integrations
func handleListTokens(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return unauthorized(c)
	}

	db, err := getDB()
	if err != nil {
		return badRequest(c, "database unavailable")
	}

	var tokens []database.IntegrationToken
	if err := db.Where("user_id = ?", uid).Order("created_at DESC").Find(&tokens).Error; err != nil {
		return badRequest(c, "failed to list tokens")
	}

	results := make([]tokenResponse, len(tokens))
	for i, tok := range tokens {
		results[i] = toTokenResponse(&tok)
	}
	return c.JSON(results)
}

// DELETE /v1/integrations/:id
func handleRevokeToken(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return unauthorized(c)
	}

	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return badRequest(c, "invalid token id")
	}

	db, err := getDB()
	if err != nil {
		return badRequest(c, "database unavailable")
	}

	result := db.Where("id = ? AND user_id = ?", id, uid).Delete(&database.IntegrationToken{})
	if result.Error != nil {
		return badRequest(c, "failed to revoke token")
	}
	if result.RowsAffected == 0 {
		return notFound(c, "integration token not found")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// LookupToken is used by the receiver to authenticate inbound requests.
// ---------------------------------------------------------------------------

func LookupToken(db *gorm.DB, tokenStr string) (*database.IntegrationToken, error) {
	var tok database.IntegrationToken
	if err := db.Where("token = ? AND active = ?", tokenStr, true).First(&tok).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("integration token not found or inactive")
		}
		return nil, err
	}
	return &tok, nil
}
