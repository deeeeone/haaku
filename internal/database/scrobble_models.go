package database

import (
	"time"
)

// --- Scrobble & Playback Models (T3.3, T3.4, T3.5) ---

// ScrobbleSession records a scrobble event: start, pause, stop, or scrobble.
type ScrobbleSession struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID     string    `gorm:"type:text;not null;index:idx_scrobble_user_media" json:"user_id"`
	MediaType  string    `gorm:"type:text;not null;index:idx_scrobble_user_media" json:"media_type"`
	MediaID    int64     `gorm:"not null;index:idx_scrobble_user_media" json:"media_id"`
	SeasonID   *int64    `json:"season_id,omitempty"`
	EpisodeID  *int64    `json:"episode_id,omitempty"`
	Action     string    `gorm:"type:text;not null" json:"action"` // start, pause, stop, scrobble
	Progress   float64   `gorm:"not null;default:0" json:"progress"`
	ExpiresAt  time.Time `gorm:"not null" json:"expires_at"`
	CreatedAt  time.Time `gorm:"not null;default:now()" json:"created_at"`
}

func (ScrobbleSession) TableName() string { return "scrobble_sessions" }

// UserPlayback records a user's playback position for resuming.
type UserPlayback struct {
	UserID    string    `gorm:"type:text;not null;uniqueIndex:uq_playback_user_media" json:"user_id"`
	MediaType string    `gorm:"type:text;not null;uniqueIndex:uq_playback_user_media" json:"media_type"`
	MediaID   int64     `gorm:"not null;uniqueIndex:uq_playback_user_media" json:"media_id"`
	Progress  float64   `gorm:"not null;default:0" json:"progress"`
	PausedAt  time.Time `gorm:"not null;default:now()" json:"paused_at"`
}

func (UserPlayback) TableName() string { return "user_playback" }

// UserCheckin records a user's active checkin (one per user).
type UserCheckin struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID     string    `gorm:"type:text;not null;uniqueIndex:uq_user_checkins" json:"user_id"`
	MediaType  string    `gorm:"type:text;not null" json:"media_type"`
	MediaID    int64     `gorm:"not null" json:"media_id"`
	SeasonID   *int64    `json:"season_id,omitempty"`
	EpisodeID  *int64    `json:"episode_id,omitempty"`
	Message    string    `gorm:"type:text" json:"message,omitempty"`
	ExpiresAt  time.Time `gorm:"not null" json:"expires_at"`
	CreatedAt  time.Time `gorm:"not null;default:now()" json:"created_at"`
}

func (UserCheckin) TableName() string { return "user_checkins" }
