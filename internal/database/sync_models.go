package database

import "time"

// UserHistory records a watch for a movie or episode.
type UserHistory struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    string    `gorm:"type:text;not null;uniqueIndex:idx_history_dedupe,priority:1" json:"user_id"`
	MediaType string    `gorm:"type:text;not null;uniqueIndex:idx_history_dedupe,priority:2" json:"media_type"`
	MediaID   int64     `gorm:"not null;uniqueIndex:idx_history_dedupe,priority:3" json:"media_id"`
	SeasonID  *int64    `json:"season_id,omitempty"`
	EpisodeID *int64    `json:"episode_id,omitempty"`
	WatchedAt time.Time `gorm:"not null" json:"watched_at"`
	Source    string    `gorm:"type:text;not null;default:api;uniqueIndex:idx_history_dedupe,priority:4" json:"source"`
	SourceID  *string   `gorm:"uniqueIndex:idx_history_dedupe,priority:5" json:"source_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (UserHistory) TableName() string { return "user_history" }

// UserCollection records a collection entry (movie/show the user owns).
type UserCollection struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	UserID      string    `gorm:"type:text;not null;uniqueIndex:idx_collection_dedupe,priority:1" json:"user_id"`
	MediaType   string    `gorm:"type:text;not null;uniqueIndex:idx_collection_dedupe,priority:2" json:"media_type"`
	MediaID     int64     `gorm:"not null;uniqueIndex:idx_collection_dedupe,priority:3" json:"media_id"`
	CollectedAt time.Time `gorm:"not null" json:"collected_at"`
	CreatedAt   time.Time `json:"created_at"`
}

func (UserCollection) TableName() string { return "user_collection" }

// UserWatchlist records a watchlist entry.
type UserWatchlist struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    string    `gorm:"type:text;not null;uniqueIndex:idx_watchlist_dedupe,priority:1" json:"user_id"`
	MediaType string    `gorm:"type:text;not null;uniqueIndex:idx_watchlist_dedupe,priority:2" json:"media_type"`
	MediaID   int64     `gorm:"not null;uniqueIndex:idx_watchlist_dedupe,priority:3" json:"media_id"`
	Rank      int       `gorm:"not null;default:0" json:"rank"`
	Notes     *string   `json:"notes,omitempty"`
	ListedAt  time.Time `gorm:"not null" json:"listed_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (UserWatchlist) TableName() string { return "user_watchlist" }

// UserRating records a user rating for any media type.
type UserRating struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	UserID    string    `gorm:"type:text;not null;uniqueIndex:idx_ratings_dedupe,priority:1" json:"user_id"`
	MediaType string    `gorm:"type:text;not null;uniqueIndex:idx_ratings_dedupe,priority:2" json:"media_type"`
	MediaID   int64     `gorm:"not null;uniqueIndex:idx_ratings_dedupe,priority:3" json:"media_id"`
	Rating    int       `gorm:"not null" json:"rating"`
	RatedAt   time.Time `gorm:"not null" json:"rated_at"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (UserRating) TableName() string { return "user_ratings" }

// UserFavorite records a favorite entry.
type UserFavorite struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	UserID      string    `gorm:"type:text;not null;uniqueIndex:idx_favorites_dedupe,priority:1" json:"user_id"`
	MediaType   string    `gorm:"type:text;not null;uniqueIndex:idx_favorites_dedupe,priority:2" json:"media_type"`
	MediaID     int64     `gorm:"not null;uniqueIndex:idx_favorites_dedupe,priority:3" json:"media_id"`
	Rank        int       `gorm:"not null;default:0" json:"rank"`
	FavoritedAt time.Time `gorm:"not null" json:"favorited_at"`
	CreatedAt   time.Time `json:"created_at"`
}

func (UserFavorite) TableName() string { return "user_favorites" }

// SyncModels returns all sync-related models for AutoMigrate.
func SyncModels() []interface{} {
	return []interface{}{
		&UserHistory{},
		&UserCollection{},
		&UserWatchlist{},
		&UserRating{},
		&UserFavorite{},
	}
}
