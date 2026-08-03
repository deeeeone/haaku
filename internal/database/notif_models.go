package database

import "time"

// Notification is a user notification in the inbox.
type Notification struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    string    `gorm:"type:text;not null" json:"user_id"`
	Type      string    `gorm:"type:text;not null" json:"type"`
	ActorID   string    `gorm:"type:text" json:"actor_id,omitempty"`
	Payload   JSONMap   `gorm:"type:jsonb;not null;default:'{}'" json:"payload"`
	Read      bool      `gorm:"default:false" json:"read"`
	CreatedAt time.Time `json:"created_at"`
}

func (Notification) TableName() string { return "notifications" }

// NotifModels returns all notification-related models for AutoMigrate.
func NotifModels() []interface{} {
	return []interface{}{
		&Notification{},
	}
}