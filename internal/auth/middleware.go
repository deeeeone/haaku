package auth

import (
	"log/slog"
	"strings"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
)

// Context key constants — these are the contract for other packages reading auth state.
const (
	ctxUserID     = "user_id"
	ctxAuthMethod = "auth_method"
)

// AuthRequired is a Fiber middleware that requires a valid auth token.
// Sets c.Locals("user_id", string) and c.Locals("auth_method", "jwt" | "apikey" | "session").
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
		// API key path — resolve via Redis-cached Clerk metadata lookup
		userID, err := LookupUserByAPIKey(token)
		if err != nil {
			slog.Debug("api key verification failed", "path", c.Path(), "error", err)
			return "", "", errInvalidToken
		}
		return userID, "apikey", nil
	case strings.HasPrefix(token, SessionTokenPrefix):
		// Local session path — an opaque server-side session row (issue #53).
		// Gated on the mode: in clerk mode a local session token is not a
		// credential at all, so the two systems can never both authenticate an
		// instance.
		if !LocalAuthEnabled() {
			return "", "", errUnsupportedPrefix
		}
		return verifyLocalSession(token)
	default:
		return "", "", errUnsupportedPrefix
	}
}

// verifyLocalSession resolves an opaque local session token to its user id.
// Every failure (unknown, revoked, expired, non-active account) collapses to
// one error, so the response never says why.
func verifyLocalSession(token string) (string, string, error) {
	db, err := database.GetDB()
	if err != nil {
		return "", "", errInvalidToken
	}
	userID, err := ResolveSessionToken(db, token)
	if err != nil {
		slog.Debug("local session verification failed", "error", err)
		return "", "", errInvalidToken
	}
	return userID, "session", nil
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
	errNoAuthHeader       = errStr("no authorization header")
	errUnsupportedPrefix  = errStr("unsupported token prefix")
	errClerkNotConfigured = errStr("clerk not configured")
	errInvalidToken       = errStr("invalid or expired token")
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
