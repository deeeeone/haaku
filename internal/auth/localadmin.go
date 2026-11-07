package auth

import (
	"errors"
	"strconv"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Admin actions on local accounts (issue #53 §4): list, ban/lock, unlock,
// approve, and issue a one-time password-reset token. Everything here is
// admin-only — the caller has already passed AuthRequired + RequireAdmin, and a
// non-admin gets 403 before reaching a handler.
//
// Deliberately *not* a general account console: the list plus those four
// actions, nothing else.

// ResetTokenPrefix marks a one-time admin-issued reset token. It is a distinct
// prefix from a session token so the two can never be confused.
const ResetTokenPrefix = "hr_"

// resetTokenTTL is how long an admin-issued reset token stays valid. Long
// enough to hand over out of band (in person, over a chat), short enough that a
// leaked note goes stale.
const resetTokenTTL = 24 * time.Hour

// adminUserResponse is the row the admin panel renders.
type adminUserResponse struct {
	ID           string     `json:"id"`
	Email        string     `json:"email,omitempty"`
	Username     string     `json:"username"`
	Status       string     `json:"status"`
	IsAdmin      bool       `json:"is_admin"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
}

type adminUserListResponse struct {
	Users  []adminUserResponse `json:"users"`
	Total  int64               `json:"total"`
	Limit  int                 `json:"limit"`
	Offset int                 `json:"offset"`
}

type adminResetResponse struct {
	UserID string `json:"user_id"`
	// Token is returned exactly once, to the admin who asked for it. It is
	// stored hashed and cannot be recovered afterwards.
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	URLPath   string    `json:"url_path"`
	// Delivery says how the admin is expected to get this to the user. This
	// build has no mail transport, so it is always "admin_handover" and the
	// endpoint never pretends mail was sent.
	Delivery string `json:"delivery"`
}

// registerLocalAdminRoutes wires the admin handlers onto an already-guarded
// group (/v1/admin, AuthRequired + RequireAdmin).
func registerLocalAdminRoutes(admin fiber.Router) {
	admin.Get("/users", handleAdminListUsers)
	admin.Post("/users/:id/ban", handleAdminBanUser)
	admin.Post("/users/:id/unlock", handleAdminUnlockUser)
	admin.Post("/users/:id/approve", handleAdminApproveUser)
	admin.Post("/users/:id/password-reset", handleAdminPasswordReset)
}

// GET /v1/admin/users
func handleAdminListUsers(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return apiInternalError(c, "database unavailable")
	}
	limit := clampInt(c.Query("limit"), 50, 1, 200)
	offset := clampInt(c.Query("offset"), 0, 0, 1_000_000)

	q := db.Model(&database.User{})
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return apiInternalError(c, "database query error")
	}
	var users []database.User
	if err := q.Order("created_at ASC, id ASC").Limit(limit).Offset(offset).Find(&users).Error; err != nil {
		return apiInternalError(c, "database query error")
	}
	out := make([]adminUserResponse, 0, len(users))
	for i := range users {
		out = append(out, toAdminUser(&users[i]))
	}
	return c.JSON(adminUserListResponse{Users: out, Total: total, Limit: limit, Offset: offset})
}

// POST /v1/admin/users/{id}/ban
func handleAdminBanUser(c *fiber.Ctx) error {
	return setUserStatus(c, database.UserStatusBanned, "ban")
}

// POST /v1/admin/users/{id}/unlock
func handleAdminUnlockUser(c *fiber.Ctx) error {
	return setUserStatus(c, database.UserStatusActive, "unlock")
}

// POST /v1/admin/users/{id}/approve
func handleAdminApproveUser(c *fiber.Ctx) error {
	return setUserStatus(c, database.UserStatusActive, "approve")
}

// setUserStatus is the shared body of ban/unlock/approve: load the target, move
// its status, and revoke its sessions when the move locks the door. action
// only shapes the error text.
func setUserStatus(c *fiber.Ctx, status, action string) error {
	db, err := database.GetDB()
	if err != nil {
		return apiInternalError(c, "database unavailable")
	}
	id := c.Params("id")
	if id == "" {
		return apiBadRequest(c, "user id is required")
	}
	// Locking yourself out of the panel is a mistake with no recovery path
	// short of database surgery, so refuse it.
	if callerID, _ := c.Locals("user_id").(string); callerID == id && status == database.UserStatusBanned {
		return apiBadRequest(c, "an admin cannot lock their own account")
	}

	var user database.User
	if err := db.First(&user, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "user not found")
		}
		return apiInternalError(c, "database query error")
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&database.User{}).Where("id = ?", id).
			Updates(map[string]any{"status": status, "updated_at": time.Now().UTC()}).Error; err != nil {
			return err
		}
		if status != database.UserStatusActive {
			return RevokeSessionsForUser(tx, id)
		}
		return nil
	})
	if err != nil {
		return apiInternalError(c, "failed to "+action+" user")
	}
	user.Status = status
	return c.JSON(toAdminUser(&user))
}

// POST /v1/admin/users/{id}/password-reset
func handleAdminPasswordReset(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return apiInternalError(c, "database unavailable")
	}
	id := c.Params("id")
	var user database.User
	if err := db.Select("id", "email", "username").First(&user, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "user not found")
		}
		return apiInternalError(c, "database query error")
	}

	token, err := newRandomToken(ResetTokenPrefix, sessionTokenBytes)
	if err != nil {
		return apiInternalError(c, "failed to generate reset token")
	}
	hash, err := localTokenHash(token)
	if err != nil {
		return apiInternalError(c, "failed to generate reset token")
	}
	rowID, err := newSessionID()
	if err != nil {
		return apiInternalError(c, "failed to generate reset token")
	}
	now := time.Now().UTC()
	expires := now.Add(resetTokenTTL)
	callerID, _ := c.Locals("user_id").(string)
	row := database.LocalAuthToken{
		ID:        rowID,
		UserID:    user.ID,
		TokenHash: hash,
		Purpose:   database.LocalAuthTokenPurposePasswordReset,
		CreatedBy: callerID,
		ExpiresAt: expires,
		CreatedAt: now,
	}
	if err := db.Create(&row).Error; err != nil {
		return apiInternalError(c, "failed to store reset token")
	}
	return c.JSON(adminResetResponse{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: expires,
		URLPath:   "/reset-password?token=" + token,
		Delivery:  "admin_handover",
	})
}

func toAdminUser(u *database.User) adminUserResponse {
	resp := adminUserResponse{
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

// clampInt parses a query parameter into [min,max], falling back to def.
func clampInt(raw string, def, min, max int) int {
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return maxInt(min, minInt(v, max))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
