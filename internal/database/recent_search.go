package database

import (
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecentSearch is one (item, query) counter in the global recent-search store
// that backs Trakt's POST /search/recent/ and
// GET /search/recent_by_id/global/{type} (issue #70).
//
// It is not per-user history: every caller contributes to the same counters.
// `query` is stored lower-cased and trimmed, and the unique key makes a repeat
// search an increment rather than a duplicate row.
type RecentSearch struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	MediaType      string    `gorm:"type:text;not null;uniqueIndex:uq_recent_searches_item_query,priority:1" json:"media_type"`
	MediaID        int64     `gorm:"not null;uniqueIndex:uq_recent_searches_item_query,priority:2" json:"media_id"`
	Query          string    `gorm:"type:text;not null;uniqueIndex:uq_recent_searches_item_query,priority:3" json:"query"`
	Count          int       `gorm:"not null;default:1" json:"count"`
	LastSearchedAt time.Time `gorm:"not null" json:"last_searched_at"`
}

func (RecentSearch) TableName() string { return "recent_searches" }

// RecentSearchModels returns the model for AutoMigrate in the sqlite-backed
// test suites. Production schema is owned by the SQL migration.
func RecentSearchModels() []interface{} {
	return []interface{}{&RecentSearch{}}
}

// UpsertRecentSearch records one search for an item and query, incrementing the
// existing counter when the (media_type, media_id, query) key already exists.
func UpsertRecentSearch(db *gorm.DB, mediaType string, mediaID int64, query string, at time.Time) error {
	row := RecentSearch{
		MediaType:      mediaType,
		MediaID:        mediaID,
		Query:          query,
		Count:          1,
		LastSearchedAt: at,
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "media_type"},
			{Name: "media_id"},
			{Name: "query"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"count":            gorm.Expr("recent_searches.count + 1"),
			"last_searched_at": at,
		}),
	}).Create(&row).Error
}

// TrendingSearch is one item's aggregated recent-search row. The newest-search
// timestamp is only an ORDER BY input, not a response field, so it is not
// scanned: SQLite returns aggregate timestamps as strings.
type TrendingSearch struct {
	MediaID int64
	Count   int64
}

// TrendingRecentSearches groups one media type's stored searches by item and
// returns them by summed count (descending), then by newest search. An optional
// filter matches the stored query case-insensitively. total is the number of
// distinct items, not the number of stored queries.
func TrendingRecentSearches(db *gorm.DB, mediaType, filter string, offset, limit int) ([]TrendingSearch, int64, error) {
	base := func() *gorm.DB {
		query := db.Model(&RecentSearch{}).Where("media_type = ?", mediaType)
		if filter != "" {
			query = query.Where("LOWER(query) LIKE ?", "%"+strings.ToLower(filter)+"%")
		}
		return query
	}

	var total int64
	if err := base().Distinct("media_id").Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []TrendingSearch
	if err := base().
		Select("media_id, SUM(count) AS count, MAX(last_searched_at) AS last_searched_at").
		Group("media_id").
		Order("count DESC, last_searched_at DESC, media_id DESC").
		Offset(offset).
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ExactTitleMovies returns a page of movies whose title exactly matches (case
// insensitively) the supplied title, plus the unpaged total.
func ExactTitleMovies(db *gorm.DB, title string, offset, limit int) ([]Movie, int64, error) {
	normalized := strings.ToLower(strings.TrimSpace(title))
	where := "LOWER(title) = ?"

	var total int64
	if err := db.Model(&Movie{}).Where(where, normalized).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []Movie
	if err := db.Model(&Movie{}).Where(where, normalized).
		Order("title ASC, id ASC").
		Offset(offset).Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ExactTitleShows is ExactTitleMovies for shows.
func ExactTitleShows(db *gorm.DB, title string, offset, limit int) ([]Show, int64, error) {
	normalized := strings.ToLower(strings.TrimSpace(title))
	where := "LOWER(title) = ?"

	var total int64
	if err := db.Model(&Show{}).Where(where, normalized).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []Show
	if err := db.Model(&Show{}).Where(where, normalized).
		Order("title ASC, id ASC").
		Offset(offset).Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
