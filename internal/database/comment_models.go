package database

import "time"

// Comment is a threaded comment (shout or review) on media or a list.
type Comment struct {
	ID          int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      string     `gorm:"type:text;not null" json:"user_id"`
	ParentID    *int64     `json:"parent_id,omitempty"`
	MediaType   string     `gorm:"type:text" json:"media_type,omitempty"`
	MediaID     *int64     `json:"media_id,omitempty"`
	ListID      *int64     `json:"list_id,omitempty"`
	CommentType string     `gorm:"type:text;not null;default:shout" json:"comment_type"`
	Body        string     `gorm:"type:text;not null" json:"body"`
	Spoiler     bool       `gorm:"default:false" json:"spoiler"`
	LikeCount   int        `gorm:"default:0" json:"like_count"`
	ReplyCount  int        `gorm:"default:0" json:"reply_count"`
	EditedAt    *time.Time `json:"edited_at,omitempty"`
	// GIFURL/GIFSlug carry the contract's optional comment gif object
	// ({url, slug}). Nil means the comment has no gif.
	GIFURL    *string   `gorm:"type:text" json:"gif_url,omitempty"`
	GIFSlug   *string   `gorm:"type:text" json:"gif_slug,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Comment) TableName() string { return "comments" }

// CommentLike records a user's like on a comment.
type CommentLike struct {
	CommentID int64     `gorm:"primaryKey" json:"comment_id"`
	UserID    string    `gorm:"primaryKey;type:text" json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (CommentLike) TableName() string { return "comment_likes" }

// CommentReaction records a user's emoji reaction on a comment.
type CommentReaction struct {
	CommentID    int64     `gorm:"primaryKey" json:"comment_id"`
	UserID       string    `gorm:"primaryKey;type:text" json:"user_id"`
	ReactionType string    `gorm:"primaryKey;type:text" json:"reaction_type"`
	CreatedAt    time.Time `json:"created_at"`
}

func (CommentReaction) TableName() string { return "comment_reactions" }

// CommentReport records a user's report of a comment for moderation.
type CommentReport struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	CommentID  int64     `gorm:"not null" json:"comment_id"`
	ReporterID string    `gorm:"type:text;not null" json:"reporter_id"`
	Reason     string    `gorm:"type:text;not null" json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}

func (CommentReport) TableName() string { return "comment_reports" }

// CommentModels returns all comment-related models for AutoMigrate.
func CommentModels() []interface{} {
	return []interface{}{
		&Comment{},
		&CommentLike{},
		&CommentReaction{},
		&CommentReport{},
	}
}
