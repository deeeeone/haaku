package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/haaku-tv/haaku/internal/config"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	projredis "github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Local account module (issue #53): signup, login, logout and password reset,
// self-hosted identity as the default.
//
// The module is one switch. RegisterLocalAuthRoutes registers the handlers AND
// their OpenAPI definitions only when the instance is in local mode; the
// always-on /v1/auth/config route is the one public way a client learns which
// mode it is talking to. Nothing here changes the middleware contract: it
// issues an identity, and auth.AuthRequired keeps reading c.Locals("user_id").

// Local user ids are namespaced. Clerk ids are always "user_...", so the
// prefix makes the two id spaces disjoint by construction: a local account can
// never collide with, or be mistaken for, a Clerk user.
const LocalIDPrefix = "local_"

// localUserID returns a fresh namespaced id.
func localUserID() (string, error) { return newRandomToken(LocalIDPrefix, 16) }

// signupPolicyError and friends are returned from the transaction so the
// handler can map them to a status code.
var (
	errEmailTaken   = errors.New("auth: email already registered")
	errSignupClosed = errors.New("auth: signup closed")
	errUsernameGone = errors.New("auth: could not allocate a username")
)

// Login attempt rate limits (issue #53 §5). Redis sliding window, the same
// convention as the request limiter and the report limiter: fail OPEN when
// Redis is unavailable, 429 + Retry-After when exceeded. Two keys are counted:
// the client IP (one host hammering) and a blind hash of the email (one
// account probed from many hosts). Neither ever stores the address itself.
const (
	loginRateWindow      = 5 * time.Minute
	loginRateMaxPerIP    = 20
	loginRateMaxPerEmail = 10
)

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

// RegisterLocalAuthConfig registers the mode-discovery endpoint. It is
// registered in BOTH modes on purpose: it is how a client finds out it is in
// clerk mode and must not call the local routes. It never touches the database
// in clerk mode.
func RegisterLocalAuthConfig(api fiber.Router) {
	api.Get("/auth/config", handleLocalAuthConfig)
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/auth/config",
		Summary: "Identity mode discovery — local vs clerk, signup policy, bootstrap state",
		Tags:    []string{"Auth"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "The identity configuration of this instance",
		},
	})
}

// RegisterLocalAuthRoutes registers the local account module and its OpenAPI
// definitions, and only in local mode. In clerk mode it registers nothing at
// all: no handler, no RouteDef, so the route registry and the generated spec
// both prove the module is absent rather than merely disabled at runtime.
func RegisterLocalAuthRoutes(api fiber.Router) {
	if !LocalAuthEnabled() {
		return
	}

	api.Post("/auth/local/signup", handleLocalSignup)
	api.Post("/auth/local/login", handleLocalLogin)
	api.Post("/auth/local/logout", AuthRequired(), handleLocalLogout)
	api.Post("/auth/local/password-reset", handleLocalPasswordReset)
	admin := api.Group("/admin", AuthRequired(), RequireAdmin())
	registerLocalAdminRoutes(admin)

	for _, r := range localAuthRouteDefs() {
		openapi.Register(r)
	}
}

// localAuthRouteDefs is the OpenAPI half of the module, kept beside the
// registration so the two cannot drift.
func localAuthRouteDefs() []openapi.RouteDef {
	return []openapi.RouteDef{
		{Method: "POST", Path: "/v1/auth/local/signup", Summary: "Create a local account (first account on an empty instance becomes the owner)", Tags: []string{"Local Auth"}, Auth: "none", StatusCodes: map[int]string{201: "Account created", 400: "Invalid request", 403: "Signup closed", 409: "Email already registered"}},
		{Method: "POST", Path: "/v1/auth/local/login", Summary: "Log in with email + password, returning an opaque session token", Tags: []string{"Local Auth"}, Auth: "none", StatusCodes: map[int]string{200: "Logged in", 400: "Invalid request", 401: "Invalid credentials", 403: "Account pending or banned", 429: "Too many attempts"}},
		{Method: "POST", Path: "/v1/auth/local/logout", Summary: "Revoke the session token used for this request", Tags: []string{"Local Auth"}, Auth: "required", StatusCodes: map[int]string{204: "Session revoked"}},
		{Method: "POST", Path: "/v1/auth/local/password-reset", Summary: "Redeem a one-time reset token issued by an admin and set a new password", Tags: []string{"Local Auth"}, Auth: "none", StatusCodes: map[int]string{200: "Password updated", 400: "Invalid or expired token"}},
		{Method: "GET", Path: "/v1/admin/users", Summary: "List local accounts (email, username, created_at, last_active_at, status)", Tags: []string{"Admin"}, Auth: "required", StatusCodes: map[int]string{200: "User list", 403: "Admin only"}},
		{Method: "POST", Path: "/v1/admin/users/{id}/ban", Summary: "Ban/lock an account and revoke its sessions", Tags: []string{"Admin"}, Auth: "required", StatusCodes: map[int]string{200: "Account banned", 403: "Admin only", 404: "User not found"}},
		{Method: "POST", Path: "/v1/admin/users/{id}/unlock", Summary: "Lift a ban/lock (sets the account back to active)", Tags: []string{"Admin"}, Auth: "required", StatusCodes: map[int]string{200: "Account unlocked", 403: "Admin only", 404: "User not found"}},
		{Method: "POST", Path: "/v1/admin/users/{id}/approve", Summary: "Approve a pending signup", Tags: []string{"Admin"}, Auth: "required", StatusCodes: map[int]string{200: "Account approved", 403: "Admin only", 404: "User not found"}},
		{Method: "POST", Path: "/v1/admin/users/{id}/password-reset", Summary: "Issue a one-time password-reset token for an admin to hand over", Tags: []string{"Admin"}, Auth: "required", StatusCodes: map[int]string{200: "Reset token issued", 403: "Admin only", 404: "User not found"}},
	}
}

// ---------------------------------------------------------------------------
// Bodies and responses
// ---------------------------------------------------------------------------

type localSignupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Username string `json:"username"`
}

type localLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type localPasswordResetRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type localUserResponse struct {
	ID           string     `json:"id"`
	Email        string     `json:"email,omitempty"`
	Username     string     `json:"username"`
	Status       string     `json:"status"`
	IsAdmin      bool       `json:"is_admin"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
}

type localSessionResponse struct {
	Token     string    `json:"token"`
	TokenType string    `json:"token_type"` // always "Bearer"
	ExpiresAt time.Time `json:"expires_at"`
}

type localSignupResponse struct {
	User    localUserResponse     `json:"user"`
	Status  string                `json:"status"`
	Session *localSessionResponse `json:"session"`
	Message string                `json:"message,omitempty"`
}

type localLoginResponse struct {
	User    localUserResponse    `json:"user"`
	Session localSessionResponse `json:"session"`
}

type localAuthConfigResponse struct {
	Mode              string `json:"mode"`
	LocalAuth         bool   `json:"local_auth"`
	SignupPolicy      string `json:"signup_policy,omitempty"`
	FirstRun          bool   `json:"first_run"`
	PasswordMinLength int    `json:"password_min_length"`
	PasswordKDF       string `json:"password_kdf"`
	// PasswordResetDelivery is "admin_handover": a reset token is generated
	// for an admin to pass on. There is no mail transport in this build, so
	// nothing here ever depends on email being configured.
	PasswordResetDelivery string `json:"password_reset_delivery"`
	Session               struct {
		TokenPrefix string `json:"token_prefix"`
		Transport   string `json:"transport"`
		TTLSeconds  int    `json:"ttl_seconds"`
	} `json:"session"`
}

func toLocalUser(u *database.User) localUserResponse {
	resp := localUserResponse{
		ID:        u.ID,
		Email:     u.Email,
		Username:  u.Username,
		Status:    u.Status,
		IsAdmin:   u.IsAdmin,
		CreatedAt: &u.CreatedAt,
	}
	if u.LastActiveAt != nil {
		resp.LastActiveAt = u.LastActiveAt
	}
	return resp
}

func newLocalSessionResponse(token string, expires time.Time) *localSessionResponse {
	return &localSessionResponse{Token: token, TokenType: "Bearer", ExpiresAt: expires}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// GET /v1/auth/config
func handleLocalAuthConfig(c *fiber.Ctx) error {
	cfg := localAuthConfigResponse{
		Mode:                  AuthMode(),
		LocalAuth:             LocalAuthEnabled(),
		PasswordMinLength:     MinPasswordLen,
		PasswordKDF:           "argon2id",
		PasswordResetDelivery: "admin_handover",
	}
	if LocalAuthEnabled() {
		cfg.SignupPolicy = SignupPolicy()
		cfg.Session.TokenPrefix = SessionTokenPrefix
		cfg.Session.Transport = "bearer"
		cfg.Session.TTLSeconds = int(SessionTTL.Seconds())
		if db, err := database.GetDB(); err == nil {
			var n int64
			if err := db.Model(&database.User{}).Count(&n).Error; err == nil {
				cfg.FirstRun = n == 0
			}
		}
	}
	return c.JSON(cfg)
}

// POST /v1/auth/local/signup
func handleLocalSignup(c *fiber.Ctx) error {
	if !LocalAuthEnabled() {
		return localNotFound(c)
	}
	var req localSignupRequest
	if err := c.BodyParser(&req); err != nil {
		return apiBadRequest(c, "invalid JSON body")
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		return apiBadRequest(c, err.Error())
	}
	if err := ValidatePassword(req.Password); err != nil {
		return apiBadRequest(c, err.Error())
	}
	db, err := database.GetDB()
	if err != nil {
		return apiInternalError(c, "database unavailable")
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		return apiInternalError(c, "failed to hash password")
	}
	user, sess, err := createLocalAccount(db, email, req.Username, hash)
	switch {
	case errors.Is(err, errEmailTaken):
		return writeError(c, fiber.StatusConflict, "conflict", "an account with that email already exists")
	case errors.Is(err, errSignupClosed):
		return writeError(c, fiber.StatusForbidden, "signup_closed", "signups are closed on this instance")
	case errors.Is(err, errUsernameGone):
		return apiInternalError(c, "could not allocate a username")
	case err != nil:
		return apiInternalError(c, "failed to create account")
	}

	resp := localSignupResponse{User: toLocalUser(user), Status: user.Status}
	if sess != nil {
		resp.Session = newLocalSessionResponse(sess.token, sess.expires)
	}
	if user.Status == database.UserStatusPending {
		resp.Message = "account created — an admin must approve it before you can log in"
	}
	return c.Status(fiber.StatusCreated).JSON(resp)
}

type issuedSession struct {
	token   string
	expires time.Time
}

// createLocalAccount creates the users row and its credential in one
// transaction. The first account on an empty instance becomes the owner/admin
// and is active; later accounts follow the signup policy.
func createLocalAccount(db *gorm.DB, email, requestedUsername, hash string) (*database.User, *issuedSession, error) {
	var user database.User
	var session *issuedSession

	err := db.Transaction(func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&database.User{}).
			Where("lower(email) = ? AND substr(id, 1, 6) = 'local_' AND deleted_at IS NULL", email).
			Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return errEmailTaken
		}

		var total int64
		if err := tx.Model(&database.User{}).Count(&total).Error; err != nil {
			return err
		}
		firstRun := total == 0

		status := database.UserStatusActive
		if !firstRun {
			switch SignupPolicy() {
			case config.SignupClosed:
				return errSignupClosed
			case config.SignupOpen:
				status = database.UserStatusActive
			default: // config.SignupApproval
				status = database.UserStatusPending
			}
		}

		username, err := allocateUsername(tx, requestedUsername, email)
		if err != nil {
			return err
		}
		id, err := localUserID()
		if err != nil {
			return err
		}
		user = database.User{
			ID:        id,
			Username:  username,
			Email:     email,
			Status:    status,
			IsAdmin:   firstRun,
			Plan:      "free",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		if err := tx.Create(&user).Error; err != nil {
			if isUniqueViolation(err) {
				return errEmailTaken
			}
			return err
		}
		if err := tx.Create(&database.LocalCredential{
			UserID:            id,
			PasswordHash:      hash,
			PasswordUpdatedAt: time.Now().UTC(),
		}).Error; err != nil {
			return err
		}

		// An active account (the bootstrap owner, or an open-policy signup) is
		// logged in immediately so first-run setup is one request. A pending
		// account gets no session: it has no identity to act with yet.
		if user.Status == database.UserStatusActive {
			now := time.Now().UTC()
			token, created, err := CreateSession(tx, user.ID, "", "")
			if err != nil {
				return err
			}
			session = &issuedSession{token: token, expires: created.ExpiresAt}
			user.LastActiveAt = &now
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &user, session, nil
}

// allocateUsername picks a free username, deriving one from the email's local
// part when the caller did not supply one.
func allocateUsername(tx *gorm.DB, requested, email string) (string, error) {
	base := sanitizeUsername(requested)
	if base == "" {
		base = sanitizeUsername(strings.SplitN(email, "@", 2)[0])
	}
	if base == "" {
		base = "user"
	}
	for i := 0; i < 50; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s%d", base, i+1)
		}
		var n int64
		if err := tx.Model(&database.User{}).Where("username = ?", candidate).Count(&n).Error; err != nil {
			return "", err
		}
		if n == 0 {
			return candidate, nil
		}
	}
	return "", errUsernameGone
}

// sanitizeUsername lower-cases and drops anything outside [a-z0-9_.-].
func sanitizeUsername(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), ".-_")
}

// POST /v1/auth/local/login
func handleLocalLogin(c *fiber.Ctx) error {
	if !LocalAuthEnabled() {
		return localNotFound(c)
	}
	var req localLoginRequest
	if err := c.BodyParser(&req); err != nil {
		return apiBadRequest(c, "invalid JSON body")
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		// A malformed address is still an authentication failure, not a
		// validation hint: answering "invalid email" would let a caller tell
		// input-shape errors from account-existence.
		return invalidCredentials(c)
	}
	if limited, err := loginRateLimited(c, email); err != nil {
		return err
	} else if limited {
		return nil
	}
	db, err := database.GetDB()
	if err != nil {
		return apiInternalError(c, "database unavailable")
	}

	var row localLoginRow
	err = db.Table("users").
		Select("users.id AS id, users.email AS email, users.username AS username, users.status AS status, users.is_admin AS is_admin, local_credentials.password_hash AS password_hash").
		Joins("JOIN local_credentials ON local_credentials.user_id = users.id").
		Where("lower(users.email) = ? AND users.deleted_at IS NULL", email).
		Take(&row).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return apiInternalError(c, "database query error")
		}
		// No account: pay the same KDF cost as a real verification so the
		// response time does not answer the question. Result is discarded.
		_ = VerifyPassword(dummyPasswordHash(), req.Password)
		return invalidCredentials(c)
	}

	if verr := VerifyPassword(row.PasswordHash, req.Password); verr != nil {
		if errors.Is(verr, ErrInvalidHash) {
			// A corrupt row is a server error, not a wrong password.
			return apiInternalError(c, "stored credential is unreadable")
		}
		return invalidCredentials(c)
	}
	switch row.Status {
	case database.UserStatusPending:
		return writeError(c, fiber.StatusForbidden, "account_pending", "this account is waiting for admin approval")
	case database.UserStatusActive:
		// continue
	default:
		return writeError(c, fiber.StatusForbidden, "account_banned", "this account is locked")
	}

	token, sess, err := CreateSession(db, row.ID, c.IP(), c.Get("User-Agent"))
	if err != nil {
		return apiInternalError(c, "failed to create session")
	}
	now := time.Now().UTC()
	_ = db.Model(&database.User{}).Where("id = ?", row.ID).Update("last_active_at", now).Error

	return c.JSON(localLoginResponse{
		User: localUserResponse{
			ID: row.ID, Email: row.Email, Username: row.Username,
			Status: row.Status, IsAdmin: row.IsAdmin,
		},
		Session: *newLocalSessionResponse(token, sess.ExpiresAt),
	})
}

// localLoginRow is the flat projection the login lookup scans into.
type localLoginRow struct {
	ID           string
	Email        string
	Username     string
	Status       string
	IsAdmin      bool
	PasswordHash string
}

// POST /v1/auth/local/logout
func handleLocalLogout(c *fiber.Ctx) error {
	token := bearerToken(c)
	if token == "" {
		return apiUnauthorized(c)
	}
	db, err := database.GetDB()
	if err != nil {
		return apiInternalError(c, "database unavailable")
	}
	if err := RevokeSessionByToken(db, token); err != nil {
		return apiInternalError(c, "failed to revoke session")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// POST /v1/auth/local/password-reset
func handleLocalPasswordReset(c *fiber.Ctx) error {
	if !LocalAuthEnabled() {
		return localNotFound(c)
	}
	var req localPasswordResetRequest
	if err := c.BodyParser(&req); err != nil {
		return apiBadRequest(c, "invalid JSON body")
	}
	if err := ValidatePassword(req.NewPassword); err != nil {
		return apiBadRequest(c, err.Error())
	}
	db, err := database.GetDB()
	if err != nil {
		return apiInternalError(c, "database unavailable")
	}
	hash, err := HashPassword(req.NewPassword)
	if err != nil {
		return apiInternalError(c, "failed to hash password")
	}
	if err := redeemPasswordResetToken(db, req.Token, hash); err != nil {
		if errors.Is(err, errInvalidResetToken) {
			// One message for unknown, expired and already-used tokens: the
			// caller must not learn whether the token or the account exists.
			return writeError(c, fiber.StatusBadRequest, "invalid_request", "invalid or expired reset token")
		}
		return apiInternalError(c, "failed to reset password")
	}
	return c.JSON(fiber.Map{"status": "ok"})
}

// redeemPasswordResetToken consumes the token and sets the new hash in one
// transaction. The consume is a conditional UPDATE, so of two concurrent
// redemptions exactly one sees RowsAffected == 1; the loser gets
// errInvalidResetToken.
func redeemPasswordResetToken(db *gorm.DB, token, hash string) error {
	if token == "" {
		return errInvalidResetToken
	}
	tokenHash, err := localTokenHash(token)
	if err != nil {
		return errInvalidResetToken
	}
	now := time.Now().UTC()
	return db.Transaction(func(tx *gorm.DB) error {
		var row database.LocalAuthToken
		if err := tx.Where("token_hash = ? AND purpose = ?", tokenHash, database.LocalAuthTokenPurposePasswordReset).
			First(&row).Error; err != nil {
			return errInvalidResetToken
		}
		if row.ConsumedAt != nil || !now.Before(row.ExpiresAt) {
			return errInvalidResetToken
		}
		res := tx.Model(&database.LocalAuthToken{}).
			Where("id = ? AND consumed_at IS NULL", row.ID).
			Update("consumed_at", now)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errInvalidResetToken
		}
		res = tx.Model(&database.LocalCredential{}).
			Where("user_id = ?", row.UserID).
			Updates(map[string]any{"password_hash": hash, "password_updated_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errInvalidResetToken
		}
		// A reset ends every session: whoever held the old password is out.
		return RevokeSessionsForUser(tx, row.UserID)
	})
}

var errInvalidResetToken = errors.New("auth: invalid reset token")

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func invalidCredentials(c *fiber.Ctx) error {
	return writeError(c, fiber.StatusUnauthorized, "unauthorized", "invalid email or password")
}

func localNotFound(c *fiber.Ctx) error {
	return writeError(c, fiber.StatusNotFound, "not_found", "not found")
}

// bearerToken returns the token from an Authorization: Bearer header.
func bearerToken(c *fiber.Ctx) string {
	h := c.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

// normalizeEmail trims, lower-cases and validates an address. Local accounts
// are keyed on the normalized form so case cannot create a second account.
func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return "", errors.New("email is required")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || !strings.Contains(email, "@") {
		return "", errors.New("a valid email address is required")
	}
	return email, nil
}

// isUniqueViolation reports whether err is a unique-index violation on either
// backend (Postgres 23505 or SQLite's UNIQUE constraint).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "unique failed")
}

// loginRateLimited counts this attempt against the IP and the email, and
// answers 429 when either window is over. It reports whether it wrote the
// response. Redis failures fail OPEN, like every other limiter here.
func loginRateLimited(c *fiber.Ctx, email string) (bool, error) {
	if over, err := slidingWindowExceeded(c, "ratelimit:login:ip:"+c.IP(), loginRateMaxPerIP); err != nil {
		return false, err
	} else if over {
		return true, nil
	}
	sum := sha256.Sum256([]byte(email))
	return slidingWindowExceeded(c, "ratelimit:login:email:"+hex.EncodeToString(sum[:8]), loginRateMaxPerEmail)
}

// slidingWindowExceeded applies the repo's Redis ZSET sliding-window limit
// under key. It writes the 429 itself and returns true when the caller must
// stop. A missing or broken Redis fails open.
func slidingWindowExceeded(c *fiber.Ctx, key string, max int64) (bool, error) {
	rdb := projredis.GetClient()
	if rdb == nil {
		return false, nil
	}
	now := time.Now()
	member := fmt.Sprintf("%d:%d", now.UnixNano(), now.UnixNano()%100000)

	ctx, cancel := context.WithTimeout(c.Context(), 100*time.Millisecond)
	defer cancel()

	pipe := rdb.Pipeline()
	pipe.ZAdd(ctx, key, goredis.Z{Score: float64(now.UnixMilli()), Member: member})
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%.0f", float64(now.Add(-loginRateWindow).UnixMilli())))
	countCmd := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, loginRateWindow)

	if _, err := pipe.Exec(ctx); err != nil {
		return false, nil
	}
	count, err := countCmd.Result()
	if err != nil {
		return false, nil
	}
	if count > max {
		c.Set("Retry-After", fmt.Sprintf("%d", int(loginRateWindow.Seconds())))
		return true, writeError(c, fiber.StatusTooManyRequests, "rate_limited", "too many login attempts — try again later")
	}
	return false, nil
}
