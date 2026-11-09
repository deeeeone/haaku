package media

import (
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// handleGetMovieStats serves GET /v1/movies/{id}/stats.
func handleGetMovieStats(c *fiber.Ctx) error {
	return serveMediaStats(c, "movie")
}

// handleGetShowStats serves GET /v1/shows/{id}/stats.
func handleGetShowStats(c *fiber.Ctx) error {
	return serveMediaStats(c, "show")
}

// serveMediaStats resolves the route's id (numeric, slug or ?id_type=) and
// answers the contract's Stats object computed from stored rows. ResolveIDAs
// owns the 404 for an unknown item, so a missing id never reaches a query.
func serveMediaStats(c *fiber.Ctx, mediaType string) error {
	_, id, err := ResolveIDAs(c, c.Params("id"), mediaType)
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	stats, err := queryMediaStats(db, mediaType, id)
	if err != nil {
		return response.InternalError(c)
	}
	return c.JSON(stats)
}

// queryMediaStats counts the rows behind a movie's or show's Stats object.
//
// Watchers and plays for a show are aggregated over its episodes: user_history
// stores a row per watched episode, never a row per show, so counting
// media_type='show' would report 0 forever. Every other field counts rows whose
// media_type is the item itself.
//
// No query joins a collection-valued column (genres, images), so a row with
// several genres cannot fan out a count — the joins are user_history -> episodes
// only, one episode row per history row.
func queryMediaStats(db *gorm.DB, mediaType string, mediaID int64) (*StatsResponse, error) {
	stats := &StatsResponse{}

	if mediaType == "show" {
		var err error
		if stats.Watchers, err = countShowWatchers(db, mediaID); err != nil {
			return nil, err
		}
		if stats.Plays, err = countShowPlays(db, mediaID); err != nil {
			return nil, err
		}
	} else {
		var err error
		if stats.Watchers, err = countDistinctUsers(db, &database.UserHistory{}, mediaType, mediaID); err != nil {
			return nil, err
		}
		if stats.Plays, err = countRows(db, &database.UserHistory{}, mediaType, mediaID); err != nil {
			return nil, err
		}
	}

	var err error
	if stats.Collectors, err = countDistinctUsers(db, &database.UserCollection{}, mediaType, mediaID); err != nil {
		return nil, err
	}
	if stats.Comments, err = countMediaRows(db, &database.Comment{}, mediaType, mediaID); err != nil {
		return nil, err
	}
	if stats.Lists, err = countVisibleListItems(db, mediaType, mediaID); err != nil {
		return nil, err
	}
	if stats.Votes, err = countMediaRows(db, &database.UserRating{}, mediaType, mediaID); err != nil {
		return nil, err
	}
	if stats.Favorited, err = countMediaRows(db, &database.UserFavorite{}, mediaType, mediaID); err != nil {
		return nil, err
	}

	// Recommended counts users who recommended the item to another user. We do
	// not store that action (there is no recommendations table), so the field is
	// null — a fabricated 0 would be indistinguishable from "nobody recommended
	// it" and would be shown to clients as a real count.
	stats.Recommended = nil
	return stats, nil
}

// countDistinctUsers counts distinct user_id rows for a (media_type, media_id).
func countDistinctUsers(db *gorm.DB, model interface{}, mediaType string, mediaID int64) (int64, error) {
	var n int64
	err := db.Model(model).
		Where("media_type = ? AND media_id = ?", mediaType, mediaID).
		Distinct("user_id").
		Count(&n).Error
	return n, err
}

// countRows counts rows for a (media_type, media_id).
func countRows(db *gorm.DB, model interface{}, mediaType string, mediaID int64) (int64, error) {
	var n int64
	err := db.Model(model).
		Where("media_type = ? AND media_id = ?", mediaType, mediaID).
		Count(&n).Error
	return n, err
}

// countMediaRows counts rows for a (media_type, media_id); it exists as a named
// seam so a future model with a different key column does not silently reuse
// countRows.
func countMediaRows(db *gorm.DB, model interface{}, mediaType string, mediaID int64) (int64, error) {
	return countRows(db, model, mediaType, mediaID)
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

// countShowWatchers counts distinct users who watched any episode of the show.
func countShowWatchers(db *gorm.DB, showID int64) (int64, error) {
	var n int64
	err := db.Model(&database.UserHistory{}).
		Joins("JOIN episodes ON episodes.id = user_history.media_id").
		Where("user_history.media_type = ? AND episodes.show_id = ?", "episode", showID).
		Distinct("user_history.user_id").
		Count(&n).Error
	return n, err
}

// countShowPlays counts episode plays across the show.
func countShowPlays(db *gorm.DB, showID int64) (int64, error) {
	var n int64
	err := db.Model(&database.UserHistory{}).
		Joins("JOIN episodes ON episodes.id = user_history.media_id").
		Where("user_history.media_type = ? AND episodes.show_id = ?", "episode", showID).
		Count(&n).Error
	return n, err
}
