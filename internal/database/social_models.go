package database

import "time"

// ---------------------------------------------------------------------------
// T4.3 — List Collaborators
// ---------------------------------------------------------------------------

// ListCollaborator records a collaboration membership on a list.
type ListCollaborator struct {
	ListID     int64      `gorm:"primaryKey" json:"list_id"`
	UserID     string     `gorm:"primaryKey;type:text" json:"user_id"`
	Role       string     `gorm:"type:text;not null" json:"role"` // editor | viewer
	InvitedAt  time.Time  `json:"invited_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
}

func (ListCollaborator) TableName() string { return "list_collaborators" }

// ---------------------------------------------------------------------------
// T4.4 — User Follows
// ---------------------------------------------------------------------------

// UserFollow records a follow relationship. If approved_at is nil, the
// follow is pending (target user has a private profile and hasn't approved yet).
type UserFollow struct {
	FollowerID  string     `gorm:"primaryKey;type:text" json:"follower_id"`
	FollowingID string     `gorm:"primaryKey;type:text" json:"following_id"`
	ApprovedAt  *time.Time `json:"approved_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (UserFollow) TableName() string { return "user_follows" }

// IsPending returns true if the follow has not yet been approved.
func (f *UserFollow) IsPending() bool { return f.ApprovedAt == nil }

// ---------------------------------------------------------------------------
// T4.4 — User Blocks
// ---------------------------------------------------------------------------

// UserBlock records a block relationship (blocker blocks blocked).
type UserBlock struct {
	BlockerID string    `gorm:"primaryKey;type:text" json:"blocker_id"`
	BlockedID string    `gorm:"primaryKey;type:text" json:"blocked_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (UserBlock) TableName() string { return "user_blocks" }

// ---------------------------------------------------------------------------
// SocialModels returns all social-related models for AutoMigrate.
// ---------------------------------------------------------------------------

func SocialModels() []interface{} {
	return []interface{}{
		&ListCollaborator{},
		&UserFollow{},
		&UserBlock{},
	}
}
