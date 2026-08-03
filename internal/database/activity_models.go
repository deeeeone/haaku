package database

import "time"

// ActivityFeed records a fan-out activity event for a user's feed.
type ActivityFeed struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ActorID   string    `gorm:"type:text;not null" json:"actor_id"`
	EventType string    `gorm:"type:text;not null" json:"event_type"`
	MediaType string    `gorm:"type:text" json:"media_type,omitempty"`
	MediaID   *int64    `json:"media_id,omitempty"`
	Payload   JSONMap   `gorm:"type:jsonb;not null;default:'{}'" json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

func (ActivityFeed) TableName() string { return "activity_feed" }

// ActivityModels returns all activity-related models for AutoMigrate.
func ActivityModels() []interface{} {
	return []interface{}{
		&ActivityFeed{},
	}
}