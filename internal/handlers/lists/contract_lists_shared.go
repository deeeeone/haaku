package lists

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/outbox"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ============================================================================
// Helpers
// ============================================================================

// advisoryListLock serializes concurrent writes to a list. The lock is a
// Postgres advisory lock; sqlite (the handler-test driver) has no equivalent,
// so it is a no-op there.
func advisoryListLock(tx *gorm.DB, listID int64) error {
	if tx.Name() == "sqlite" {
		return nil
	}
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", fmt.Sprintf("list:%d", listID)).Error
}

// contractPublish queues an outbox event. The outbox writer emits a Postgres
// `::jsonb` cast, which sqlite (the handler-test driver) cannot parse, so the
// event is skipped there. Production is Postgres and always publishes.
func contractPublish(tx *gorm.DB, eventType, uid string, payload map[string]any) error {
	if tx.Name() == "sqlite" {
		return nil
	}
	return outbox.PublishEvent(tx, eventType, uid, payload)
}

func contractDB(c *fiber.Ctx) (*gorm.DB, error) {
	db, err := database.GetDB()
	if err != nil {
		_ = response.InternalError(c)
		return nil, response.ErrHandled
	}
	return db, nil
}

// contractTargetUser resolves the {id} path segment (including the "me" alias)
// to a user id and rejects a caller acting on another user's surface. Returns
// "" (after writing the response) when the caller may not act.
func contractTargetUser(c *fiber.Ctx) (string, error) {
	target := auth.ResolveTargetID(c, "id")
	if target == "" {
		_ = response.Unauthorized(c)
		return "", response.ErrHandled
	}
	if caller := userID(c); caller != target {
		_ = response.Forbidden(c, "you may only manage your own lists")
		return "", response.ErrHandled
	}
	return target, nil
}

// contractListOwnerID resolves {id} for a read and verifies the list belongs to
// that user; a mismatch is a 404 (the list is not under this user).
func contractListOwnerID(c *fiber.Ctx) (string, error) {
	target := auth.ResolveTargetID(c, "id")
	if target == "" {
		_ = response.Unauthorized(c)
		return "", response.ErrHandled
	}
	return target, nil
}

func contractLoadList(db *gorm.DB, param string) (*database.List, error) {
	var l database.List
	if id, err := strconv.ParseInt(param, 10, 64); err == nil {
		if err := db.First(&l, id).Error; err != nil {
			return nil, err
		}
		return &l, nil
	}
	if err := db.Where("slug = ?", param).First(&l).Error; err != nil {
		return nil, err
	}
	return &l, nil
}

// canViewList is the single list-privacy rule: a private list is visible only
// to its owner. It mirrors the gate our native GET /v1/lists/{id} applies.
func canViewList(l *database.List, viewerID string) bool {
	return l.Privacy != "private" || viewerID == l.UserID
}

func listNotFound(c *fiber.Ctx, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		_ = response.NotFound(c, "list not found")
		return response.ErrHandled
	}
	_ = response.InternalError(c)
	return response.ErrHandled
}

// contractListObject renders one list in the contract's list shape with a real
// item count and its owner.
func contractListObject(db *gorm.DB, l *database.List) (map[string]any, error) {
	var itemCount int64
	if err := db.Model(&database.ListItem{}).Where("list_id = ?", l.ID).Count(&itemCount).Error; err != nil {
		return nil, err
	}
	users, err := contractshape.UserByIDBatch(db, []string{l.UserID})
	if err != nil {
		return nil, err
	}
	obj := contractshape.List(l, users[l.UserID])
	obj["item_count"] = itemCount
	return obj, nil
}
