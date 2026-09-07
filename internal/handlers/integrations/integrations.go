package integrations

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"trakt-api/internal/auth"
	"trakt-api/internal/database"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"trakt-api/internal/response"
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

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type createTokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes,omitempty"`
}

// validScopes is the allowlist of scopes an integration token may grant. It
// mirrors the canonical OAuth scope set. "*" is deliberately excluded — a
// wildcard token is too broad, and unknown scopes are rejected rather than
// silently dropped (no token should receive permissions it was not asked for).
var validScopes = map[string]bool{
	"profile": true, "history:read": true, "history:write": true,
	"watchlist:read": true, "watchlist:write": true,
	"ratings:read": true, "ratings:write": true,
	"lists:read": true, "lists:write": true,
	"social:read": true, "social:write": true,
	"webhooks:manage": true, "offline_access": true,
}

// defaultScopes grants a minimal read-only scope to a token created without an
// explicit scopes field. It replaces the former wildcard "*" default, which
// would have granted unrestricted access.
var defaultScopes = []string{"profile"}

type tokenResponse struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	Active    bool     `json:"active"`
	CreatedAt string   `json:"created_at"`
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
		CreatedAt: response.FormatTime(t.CreatedAt),
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// POST /v1/integrations
func handleCreateToken(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return response.Unauthorized(c)
	}

	var req createTokenRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	if req.Name == "" {
		return response.BadRequest(c, "name is required")
	}

	tokenStr, err := generateToken()
	if err != nil {
		fmt.Printf("integrations: generate token failed: %v\n", err)
		return response.InternalError(c)
	}

	db, err := getDB()
	if err != nil {
		fmt.Printf("integrations: database unavailable: %v\n", err)
		return response.ServiceUnavailable(c, "database unavailable")
	}

	scopes := database.StringSlice(req.Scopes)
	if len(scopes) == 0 {
		scopes = database.StringSlice(defaultScopes)
	} else {
		// Validate every requested scope against the allowlist and dedupe.
		// Unknown (including the former wildcard "*") are rejected outright so a
		// token is never granted permission beyond what was explicitly listed.
		granted := make([]string, 0, len(scopes))
		seen := make(map[string]bool, len(scopes))
		for _, s := range scopes {
			s = strings.TrimSpace(s)
			if !validScopes[s] {
				return response.BadRequest(c, "invalid scope: "+s)
			}
			if !seen[s] {
				seen[s] = true
				granted = append(granted, s)
			}
		}
		scopes = database.StringSlice(granted)
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
		fmt.Printf("integrations: create token failed: %v\n", err)
		return response.InternalError(c)
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
		return response.Unauthorized(c)
	}

	db, err := getDB()
	if err != nil {
		fmt.Printf("integrations: database unavailable: %v\n", err)
		return response.ServiceUnavailable(c, "database unavailable")
	}

	var tokens []database.IntegrationToken
	if err := db.Where("user_id = ?", uid).Order("created_at DESC").Find(&tokens).Error; err != nil {
		fmt.Printf("integrations: list tokens failed: %v\n", err)
		return response.InternalError(c)
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
		return response.Unauthorized(c)
	}

	id, err := c.ParamsInt("id")
	if err != nil || id <= 0 {
		return response.BadRequest(c, "invalid token id")
	}

	db, err := getDB()
	if err != nil {
		fmt.Printf("integrations: database unavailable: %v\n", err)
		return response.ServiceUnavailable(c, "database unavailable")
	}

	result := db.Where("id = ? AND user_id = ?", id, uid).Delete(&database.IntegrationToken{})
	if result.Error != nil {
		fmt.Printf("integrations: revoke token %d failed: %v\n", id, result.Error)
		return response.InternalError(c)
	}
	if result.RowsAffected == 0 {
		return response.NotFound(c, "integration token not found")
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
			return nil, fmt.Errorf("integration token not found or inactive: %w", err)
		}
		return nil, err
	}
	return &tok, nil
}
