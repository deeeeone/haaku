package database

import "time"

// EventsOutbox is the transactional outbox table (T3.6).
// Domain events are inserted here within the same transaction as the
// data mutation, then published asynchronously to Redis Streams.
type EventsOutbox struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	EventType string    `gorm:"type:text;not null" json:"event_type"`
	Payload   JSONMap   `gorm:"type:jsonb;default:'{}'" json:"payload"`
	UserID    string    `gorm:"type:text" json:"user_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Published bool      `gorm:"default:false" json:"published"`
}

func (EventsOutbox) TableName() string { return "events_outbox" }
