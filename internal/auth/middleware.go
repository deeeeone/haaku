package auth

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// Context key constants — these are the contract for other packages reading auth state.
const (
	ctxUserID     = "user_id"
	ctxAuthMethod = "auth_method"
)

// AuthRequired is a Fiber middleware that requires a valid auth token.
// Sets c.Locals("user_id", string) and c.Locals("auth_method", "jwt" | "apikey").
func AuthRequired() fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, authMethod, err := extractAuth(c)
		if err != nil {
			slog.Debug("auth required: rejected", "path", c.Path(), "error", err)
			return writeUnauthorized(c)
		}
		c.Locals(ctxUserID, userID)
		c.Locals(ctxAuthMethod, authMethod)
		return c.Next()
	}
}

// OptionalAuth is a Fiber middleware that attempts auth but continues regardless.
// Sets c.Locals("user_id") and c.Locals("auth_method") only if token is present and valid.
func OptionalAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, authMethod, err := extractAuth(c)
		if err != nil {
			// Token present but invalid — still continue, just don't set locals.
			// Don't log garbage/missing tokens at debug; only log parse failures.
			if err.Error() != "no authorization header" && err.Error() != "unsupported token prefix" {
				slog.Debug("optional auth: token invalid, continuing", "path", c.Path(), "error", err)
			}
			return c.Next()
		}
		c.Locals(ctxUserID, userID)
		c.Locals(ctxAuthMethod, authMethod)
		return c.Next()
	}
}

// extractAuth reads the Authorization header, sniffs the token type, and verifies it.
// Returns (userID, authMethod, error).
func extractAuth(c *fiber.Ctx) (string, string, error) {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return "", "", errNoAuthHeader
	}

	// Must be Bearer token
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return "", "", errNoAuthHeader
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	token = strings.TrimSpace(token)
	if token == "" {
		return "", "", errNoAuthHeader
	}

	// Token sniffing
	switch {
	case strings.HasPrefix(token, "eyJ"):
		// JWT path — verify through Clerk
		return verifyJWT(token)
	case strings.HasPrefix(token, "tk_"):
		// API key path — defer to apikey package (not yet implemented)
		slog.Debug("api key path not yet implemented", "path", c.Path())
		return "", "", errUnsupportedPrefix
	default:
		return "", "", errUnsupportedPrefix
	}
}

// verifyJWT verifies a Clerk session JWT and returns the user ID.
func verifyJWT(token string) (string, string, error) {
	clerk := GetClerk()
	if clerk == nil {
		return "", "", errClerkNotConfigured
	}

	userID, err := clerk.VerifySessionToken(token)
	if err != nil {
		slog.Debug("jwt verification failed", "error", err)
		return "", "", errInvalidToken
	}

	return userID, "jwt", nil
}

// ---------------------------------------------------------------------------
// Error helpers for the auth middleware
// ---------------------------------------------------------------------------

var (
	errNoAuthHeader        = errStr("no authorization header")
	errUnsupportedPrefix   = errStr("unsupported token prefix")
	errClerkNotConfigured  = errStr("clerk not configured")
	errInvalidToken        = errStr("invalid or expired token")
)

type errStr string

func (e errStr) Error() string { return string(e) }

// writeUnauthorized sends the generic unauthorized error envelope.
func writeUnauthorized(c *fiber.Ctx) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"error": fiber.Map{
			"code":    "unauthorized",
			"message": "invalid or expired token",
		},
	})
}
