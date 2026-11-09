package database

import "time"

// UserShowProgressReset records the point a user restarted a show from.
//
// It is deliberately not a delete: user_history is untouched, and watched
// progress ignores plays older than ResetAt. Deleting the row (Trakt's DELETE
// undo) restores the previous progress.
type UserShowProgressReset struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    string    `gorm:"type:text;not null;uniqueIndex:uq_user_show_progress_reset,priority:1" json:"user_id"`
	ShowID    int64     `gorm:"not null;uniqueIndex:uq_user_show_progress_reset,priority:2" json:"show_id"`
	ResetAt   time.Time `gorm:"not null" json:"reset_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (UserShowProgressReset) TableName() string { return "user_show_progress_resets" }
