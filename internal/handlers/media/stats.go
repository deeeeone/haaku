package media

import (
	"fmt"
	"strconv"

	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// queryMovieStats computes the Trakt movie stats from rows we already store.
// No engine or materialised table is involved: each field is a direct aggregate.
func queryMovieStats(db *gorm.DB, movieID int64) (*StatsResponse, error) {
	stats := &StatsResponse{}

	if err := db.Model(&database.UserHistory{}).
		Where("media_type = ? AND media_id = ?", "movie", movieID).
		Distinct("user_id").Count(&stats.Watchers).Error; err != nil {
		return nil, fmt.Errorf("count watchers: %w", err)
	}
	if err := db.Model(&database.UserHistory{}).
		Where("media_type = ? AND media_id = ?", "movie", movieID).
		Count(&stats.Plays).Error; err != nil {
		return nil, fmt.Errorf("count plays: %w", err)
	}
	if err := db.Model(&database.UserCollection{}).
		Where("media_type = ? AND media_id = ?", "movie", movieID).
		Distinct("user_id").Count(&stats.Collectors).Error; err != nil {
		return nil, fmt.Errorf("count collectors: %w", err)
	}
	if err := db.Model(&database.Comment{}).
		Where("media_type = ? AND media_id = ?", "movie", movieID).
		Count(&stats.Comments).Error; err != nil {
		return nil, fmt.Errorf("count comments: %w", err)
	}
	var listsErr error
	if stats.Lists, listsErr = countVisibleListItems(db, "movie", movieID); listsErr != nil {
		return nil, fmt.Errorf("count lists: %w", listsErr)
	}
	if err := db.Model(&database.UserRating{}).
		Where("media_type = ? AND media_id = ?", "movie", movieID).
		Count(&stats.Votes).Error; err != nil {
		return nil, fmt.Errorf("count votes: %w", err)
	}
	if err := db.Model(&database.UserFavorite{}).
		Where("media_type = ? AND media_id = ?", "movie", movieID).
		Distinct("user_id").Count(&stats.Favorited).Error; err != nil {
		return nil, fmt.Errorf("count favorited: %w", err)
	}
	// Recommended counts users who recommended the item to another user. We do
	// not store that action (there is no recommendations table), so the field is
	// null - a fabricated 0 would be indistinguishable from "nobody recommended
	// it" and would be shown to clients as a real count. (main's #126 review
	// decided this; the PR's "favorites renamed" claim is not in the contract.)
	stats.Recommended = nil

	return stats, nil
}

// queryShowStats computes Trakt show stats. Plays and watchers count the
// history rows of the show's episodes (user_history keys episodes by
// media_type='episode', media_id=episode id). Collectors count users who
// collected the show or any of its episodes; ratings, comments, lists and
// favorites are title-level (media_type='show').
func queryShowStats(db *gorm.DB, showID int64) (*StatsResponse, error) {
	stats := &StatsResponse{}
	episodes := db.Model(&database.Episode{}).Select("id").Where("show_id = ?", showID)

	if err := db.Model(&database.UserHistory{}).
		Where("media_type = ? AND media_id IN (?)", "episode", episodes).
		Distinct("user_id").Count(&stats.Watchers).Error; err != nil {
		return nil, fmt.Errorf("count watchers: %w", err)
	}
	if err := db.Model(&database.UserHistory{}).
		Where("media_type = ? AND media_id IN (?)", "episode", episodes).
		Count(&stats.Plays).Error; err != nil {
		return nil, fmt.Errorf("count plays: %w", err)
	}
	// Trakt counts a collector as a user who collected the show OR any of its
	// episodes, so an episode-only collector is still a collector.
	if err := db.Model(&database.UserCollection{}).
		Where("(media_type = ? AND media_id = ?) OR (media_type = ? AND media_id IN (?))",
			"show", showID, "episode", episodes).
		Distinct("user_id").Count(&stats.Collectors).Error; err != nil {
		return nil, fmt.Errorf("count collectors: %w", err)
	}
	var collectedEpisodes int64
	if err := db.Model(&database.UserCollection{}).
		Where("media_type = ? AND media_id IN (?)", "episode", episodes).
		Count(&collectedEpisodes).Error; err != nil {
		return nil, fmt.Errorf("count collected episodes: %w", err)
	}
	stats.CollectedEpisodes = &collectedEpisodes
	if err := db.Model(&database.Comment{}).
		Where("media_type = ? AND media_id = ?", "show", showID).
		Count(&stats.Comments).Error; err != nil {
		return nil, fmt.Errorf("count comments: %w", err)
	}
	var showListsErr error
	if stats.Lists, showListsErr = countVisibleListItems(db, "show", showID); showListsErr != nil {
		return nil, fmt.Errorf("count lists: %w", showListsErr)
	}
	if err := db.Model(&database.UserRating{}).
		Where("media_type = ? AND media_id = ?", "show", showID).
		Count(&stats.Votes).Error; err != nil {
		return nil, fmt.Errorf("count votes: %w", err)
	}
	if err := db.Model(&database.UserFavorite{}).
		Where("media_type = ? AND media_id = ?", "show", showID).
		Distinct("user_id").Count(&stats.Favorited).Error; err != nil {
		return nil, fmt.Errorf("count favorited: %w", err)
	}
	// Recommended counts users who recommended the item to another user. We do
	// not store that action (there is no recommendations table), so the field is
	// null - a fabricated 0 would be indistinguishable from "nobody recommended
	// it" and would be shown to clients as a real count. (main's #126 review
	// decided this; the PR's "favorites renamed" claim is not in the contract.)
	stats.Recommended = nil

	return stats, nil
}

// queryRatingDistribution groups user_ratings into Trakt's 1..10 histogram.
// Every bucket is present, including the empty ones.
func queryRatingDistribution(db *gorm.DB, mediaType string, mediaID int64) (map[string]int64, error) {
	distribution := emptyRatingDistribution()

	var rows []struct {
		Rating int
		Count  int64
	}
	if err := db.Model(&database.UserRating{}).
		Select("rating, count(*) as count").
		Where("media_type = ? AND media_id = ?", mediaType, mediaID).
		Group("rating").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("group ratings: %w", err)
	}
	for _, r := range rows {
		if r.Rating >= 1 && r.Rating <= 10 {
			distribution[strconv.Itoa(r.Rating)] = r.Count
		}
	}
	return distribution, nil
}

// countVisibleListItems counts the lists that contain the item and that any
// reader may open. A private list is visible only to its owner, so counting its
// items would leak to every reader of /stats that a private list holds this
// item. list_items carries no user column, so the privacy gate has to come from
// the owning list: join and keep only public/link_only. Columns are qualified
// because both tables have an `id` (and lists also has `user_id`).
func countVisibleListItems(db *gorm.DB, mediaType string, mediaID int64) (int64, error) {
	var n int64
	err := db.Model(&database.ListItem{}).
		Joins("JOIN lists ON lists.id = list_items.list_id").
		Where("list_items.media_type = ? AND list_items.media_id = ?", mediaType, mediaID).
		Where("lists.privacy IN ?", []string{"public", "link_only"}).
		Count(&n).Error
	return n, err
}
