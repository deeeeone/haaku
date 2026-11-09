package database

import "time"

// List represents a user-created list (T4.1).
type List struct {
	ID            int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID        string `gorm:"type:text;not null" json:"user_id"`
	Name          string `gorm:"type:text;not null" json:"name"`
	Description   string `gorm:"type:text;default:''" json:"description,omitempty"`
	Slug          string `gorm:"type:text;uniqueIndex;not null" json:"slug"`
	Privacy       string `gorm:"type:text;not null;default:public" json:"privacy"`
	SortBy        string `gorm:"type:text;default:rank" json:"sort_by,omitempty"`
	SortHow       string `gorm:"type:text;default:asc" json:"sort_how,omitempty"`
	IsRanked      bool   `gorm:"default:false" json:"is_ranked"`
	CoverURL      string `gorm:"type:text;default:''" json:"cover_url,omitempty"`
	AllowComments bool   `gorm:"default:true" json:"allow_comments"`
	// SortOrder is the list's position among its owner's lists, set by
	// POST /users/{id}/lists/reorder. It is not part of our own list JSON
	// (json:"-") — it exists so the contract's list-reorder can persist an
	// order rather than accept and ignore the body.
	SortOrder    int       `gorm:"column:sort_order;default:0" json:"-"`
	LikeCount    int       `gorm:"default:0" json:"like_count"`
	CommentCount int       `gorm:"default:0" json:"comment_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (List) TableName() string { return "lists" }

// ListItem represents an item within a list (T4.1).
type ListItem struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ListID    int64     `gorm:"not null;uniqueIndex:idx_listitem_dedupe,priority:1" json:"list_id"`
	MediaType string    `gorm:"type:text;not null;uniqueIndex:idx_listitem_dedupe,priority:2" json:"media_type"`
	MediaID   int64     `gorm:"not null;uniqueIndex:idx_listitem_dedupe,priority:3" json:"media_id"`
	Rank      int       `gorm:"not null;default:0" json:"rank"`
	Notes     string    `gorm:"type:text;default:''" json:"notes,omitempty"`
	AddedBy   string    `gorm:"type:text;not null" json:"added_by"`
	AddedAt   time.Time `json:"added_at"`
}

func (ListItem) TableName() string { return "list_items" }

// SmartList represents a dynamic smart list (T4.2).
type SmartList struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    string    `gorm:"type:text;not null" json:"user_id"`
	Name      string    `gorm:"type:text;not null" json:"name"`
	Filters   JSONMap   `gorm:"type:jsonb;default:'{}'" json:"filters"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (SmartList) TableName() string { return "smart_lists" }

// ListLike records a like on a list.
type ListLike struct {
	ListID    int64     `gorm:"primaryKey;autoIncrement:false" json:"list_id"`
	UserID    string    `gorm:"primaryKey;type:text" json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (ListLike) TableName() string { return "list_likes" }

// ListComment records a comment on a list.
type ListComment struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ListID    int64     `gorm:"not null" json:"list_id"`
	UserID    string    `gorm:"type:text;not null" json:"user_id"`
	Body      string    `gorm:"type:text;not null" json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ListComment) TableName() string { return "list_comments" }

// ListModels returns all list-related models for AutoMigrate.
func ListModels() []interface{} {
	return []interface{}{
		&List{},
		&ListItem{},
		&SmartList{},
		&ListLike{},
		&ListComment{},
	}
}
