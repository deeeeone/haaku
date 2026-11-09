package database

import "time"

// UserReport records a report a signed-in user files against another account
// (Trakt's POST /users/{id}/report). It is unrelated to ContentReport, which
// covers media items: the reported subject here is a user, whose primary key is
// the string account id, so it cannot be stored in content_reports.media_id.
//
// One report per reporter per reported user; re-reporting updates the reason and
// message rather than accumulating duplicates (the unique index in the migration
// enforces it, and the handler upserts).
type UserReport struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ReporterID     string    `gorm:"type:text;not null;uniqueIndex:idx_user_reports_reporter_target,priority:1" json:"reporter_id"`
	ReportedUserID string    `gorm:"type:text;not null;uniqueIndex:idx_user_reports_reporter_target,priority:2" json:"reported_user_id"`
	Reason         string    `gorm:"type:text;not null" json:"reason"`
	Message        string    `gorm:"type:text;default:''" json:"message"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (UserReport) TableName() string { return "user_reports" }
