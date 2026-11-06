package database

import "time"

// ---------------------------------------------------------------------------
// T5.x — User Notes
// ---------------------------------------------------------------------------

// UserNote is a private text note attached to a media item by a user.
// One note per user per (media_type, media_id). Upsert semantics.
type UserNote struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    string    `gorm:"type:text;not null" json:"user_id"`
	MediaType string    `gorm:"type:text;not null" json:"media_type"`
	MediaID   int64     `gorm:"not null" json:"media_id"`
	Note      string    `gorm:"type:text;not null" json:"note"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (UserNote) TableName() string { return "user_notes" }

// ---------------------------------------------------------------------------
// T5.x — Saved Filters
// ---------------------------------------------------------------------------

// SavedFilter is a user's saved UI filter configuration for a section.
// Max 20 per user per section.
type SavedFilter struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    string    `gorm:"type:text;not null" json:"user_id"`
	Section   string    `gorm:"type:text;not null" json:"section"`
	Name      string    `gorm:"type:text;not null" json:"name"`
	Filter    JSONMap   `gorm:"type:jsonb;not null;default:'{}'" json:"filter"`
	CreatedAt time.Time `json:"created_at"`
}

func (SavedFilter) TableName() string { return "saved_filters" }

// ---------------------------------------------------------------------------
// T5.x — Content Reports
// ---------------------------------------------------------------------------

// ContentReport is a user's report of problematic content for moderation.
// One report per reporter per (media_type, media_id).
//
// Lifecycle (issue #10 §2): a report starts "open" and a moderator moves it to
// a terminal state — "resolved" (action taken) or "dismissed" (no action
// needed). ResolvedBy/ResolvedAt/ResolutionNote record who actioned it and why
// (the audit trail); all three are empty while the report is open.
type ContentReport struct {
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	ReporterID     string     `gorm:"type:text;not null" json:"reporter_id"`
	MediaType      string     `gorm:"type:text;not null" json:"media_type"`
	MediaID        int64      `gorm:"not null" json:"media_id"`
	Reason         string     `gorm:"type:text;not null" json:"reason"`
	Description    string     `gorm:"type:text;default:''" json:"description"`
	Status         string     `gorm:"type:text;not null;default:'open'" json:"status"`
	ResolutionNote string     `gorm:"type:text;default:''" json:"resolution_note"`
	ResolvedBy     string     `gorm:"type:text" json:"resolved_by,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (ContentReport) TableName() string { return "content_reports" }

// IsOpen reports whether the report still needs moderation action.
func (r ContentReport) IsOpen() bool { return r.Status == "" || r.Status == "open" }

// ---------------------------------------------------------------------------
// NotesFiltersReportsModels returns all models for AutoMigrate.
// ---------------------------------------------------------------------------

func NotesFiltersReportsModels() []interface{} {
	return []interface{}{
		&UserNote{},
		&SavedFilter{},
		&ContentReport{},
	}
}
