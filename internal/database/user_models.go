package database

import "time"

// UserStats holds per-user aggregated statistics (T3.8).
// Populated by stats.ComputeUserStats, cached via user_version keying.
type UserStats struct {
	UserID         string     `gorm:"primaryKey;type:text" json:"user_id"`
	Plays          int64      `gorm:"default:0" json:"plays"`
	Minutes        int64      `gorm:"default:0" json:"minutes"`
	UniqueMovies   int        `gorm:"default:0" json:"unique_movies"`
	UniqueShows    int        `gorm:"default:0" json:"unique_shows"`
	UniqueEpisodes int        `gorm:"default:0" json:"unique_episodes"`
	RatingsCount   int        `gorm:"default:0" json:"ratings_count"`
	ComputedAt     *time.Time `json:"computed_at,omitempty"`
}

func (UserStats) TableName() string { return "user_stats" }

// SyncSession tracks batch sync progress (T3.9).
type SyncSession struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID         string    `gorm:"type:text;not null" json:"user_id"`
	Kind           string    `gorm:"type:text;not null" json:"kind"`   // history | ratings
	Status         string    `gorm:"type:text;default:pending" json:"status"` // pending | processing | completed | failed
	TotalItems     int       `gorm:"default:0" json:"total_items"`
	ProcessedItems int       `gorm:"default:0" json:"processed_items"`
	Errors         JSONMap   `gorm:"type:jsonb;default:'[]'" json:"errors"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (SyncSession) TableName() string { return "sync_sessions" }

// UserHidden records items hidden by a user in a specific section (T3.10).
type UserHidden struct {
	UserID    string    `gorm:"type:text;not null;uniqueIndex:idx_user_hidden_unique,priority:1" json:"user_id"`
	Section   string    `gorm:"type:text;not null;uniqueIndex:idx_user_hidden_unique,priority:2" json:"section"`
	MediaType string    `gorm:"type:text;not null;uniqueIndex:idx_user_hidden_unique,priority:3" json:"media_type"`
	MediaID   int64     `gorm:"not null;uniqueIndex:idx_user_hidden_unique,priority:4" json:"media_id"`
	HiddenAt  time.Time `json:"hidden_at"`
}

func (UserHidden) TableName() string { return "user_hidden" }
