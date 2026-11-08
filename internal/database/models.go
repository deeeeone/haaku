package database

import (
	"time"

	"gorm.io/gorm"
)

// User mirrors Clerk identity. id IS the Clerk user id (text PK).
// Profile fields synced via Clerk webhooks; local fields (bio, privacy, role, plan)
// are ours. See ../haaku-plan/02-architecture/data-model.md §1.
type User struct {
	ID string `gorm:"primaryKey;type:text" json:"id"`
	// `unique`, not `uniqueIndex`: migration 0001 declares username as an
	// inline UNIQUE constraint (users_username_key), and AutoMigrate against a
	// real database compares the column's unique flag, not an index name. With
	// uniqueIndex set, GORM saw unique=true on the column but field.Unique=false
	// and tried to DROP CONSTRAINT uni_users_username, which does not exist.
	Username        string         `gorm:"unique;type:text;not null" json:"username"`
	Email           string         `gorm:"type:text" json:"email,omitempty"`
	Name            string         `gorm:"type:text" json:"name"`
	Bio             string         `gorm:"type:text" json:"bio,omitempty"`
	AvatarURL       string         `gorm:"type:text" json:"avatar_url,omitempty"`
	Location        string         `gorm:"type:text" json:"location,omitempty"`
	IsPrivate       bool           `gorm:"default:false" json:"is_private"`
	IsAdmin         bool           `gorm:"default:false" json:"is_admin"`
	IsModerator     bool           `gorm:"default:false" json:"is_moderator"`
	Status          string         `gorm:"type:text;default:active" json:"status"`
	Plan            string         `gorm:"type:text;default:free" json:"plan"`
	ClerkSyncStatus string         `gorm:"type:text;default:pending" json:"clerk_sync_status,omitempty"`
	ClerkSyncedAt   *time.Time     `json:"clerk_synced_at,omitempty"` // last successful Clerk identity sync
	Timezone        string         `gorm:"type:text" json:"timezone,omitempty"`
	Language        string         `gorm:"type:text" json:"language,omitempty"`
	Settings        string         `gorm:"type:jsonb;default:'{}'" json:"settings,omitempty"`
	LastActiveAt    *time.Time     `json:"last_active_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (User) TableName() string { return "users" }

// UserBlock and UserFollow are defined in social_models.go (T4.4).
