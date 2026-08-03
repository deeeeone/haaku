package database

import (
	"time"

	"gorm.io/gorm"
)

// User mirrors Clerk identity. id IS the Clerk user id (text PK).
// Profile fields synced via Clerk webhooks; local fields (bio, privacy, role, plan)
// are ours. See ../trakt-killer-plan/02-architecture/data-model.md §1.
type User struct {
	ID               string         `gorm:"primaryKey;type:text" json:"id"`
	Username         string         `gorm:"uniqueIndex;type:text;not null" json:"username"`
	Email            string         `gorm:"type:text" json:"email,omitempty"`
	Name             string         `gorm:"type:text" json:"name"`
	Bio              string         `gorm:"type:text" json:"bio,omitempty"`
	AvatarURL        string         `gorm:"type:text" json:"avatar_url,omitempty"`
	Location         string         `gorm:"type:text" json:"location,omitempty"`
	IsPrivate        bool           `gorm:"default:false" json:"is_private"`
	IsAdmin          bool           `gorm:"default:false" json:"is_admin"`
	IsModerator      bool           `gorm:"default:false" json:"is_moderator"`
	Status           string         `gorm:"type:text;default:active" json:"status"`
	Plan             string         `gorm:"type:text;default:free" json:"plan"`
	ClerkSyncStatus  string         `gorm:"type:text;default:pending" json:"clerk_sync_status,omitempty"`
	Timezone         string         `gorm:"type:text" json:"timezone,omitempty"`
	Language         string         `gorm:"type:text" json:"language,omitempty"`
	Settings         string         `gorm:"type:jsonb;default:'{}'" json:"settings,omitempty"`
	LastActiveAt     *time.Time     `json:"last_active_at,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (User) TableName() string { return "users" }

// UserBlock records a block relationship (blocker blocks blocked).
type UserBlock struct {
	BlockerID string    `gorm:"primaryKey;type:text" json:"blocker_id"`
	BlockedID string    `gorm:"primaryKey;type:text" json:"blocked_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (UserBlock) TableName() string { return "user_blocks" }

// UserFollow records a follow relationship.
type UserFollow struct {
	FollowerID string    `gorm:"primaryKey;type:text" json:"follower_id"`
	FollowedID string    `gorm:"primaryKey;type:text" json:"followed_id"`
	CreatedAt  time.Time `json:"created_at"`
}

func (UserFollow) TableName() string { return "user_follows" }