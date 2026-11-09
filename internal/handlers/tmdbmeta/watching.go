package tmdbmeta

import (
	"time"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Watching rows: the users currently watching a season or an episode.
//
// The predicate is the one this repo already trusts for "watching right now"
// (see users.handleUserWatching): a live, non-expired scrobble session whose
// action is a start/scrobble, not a pause/stop. A saved resume position
// (user_playback.paused_at) is not evidence of current playback and is not
// used. Every value in the response comes from a stored session row joined to a
// stored user row.

func handleSeasonWatching(c *fiber.Ctx) error {
	db, _, season, err := resolveShowSeason(c)
	if err != nil {
		return err
	}

	var episodeIDs []int64
	if err := db.Model(&database.Episode{}).Where("season_id = ?", season.ID).
		Pluck("id", &episodeIDs).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	if len(episodeIDs) == 0 {
		return c.JSON([]any{})
	}

	var userIDs []string
	if err := db.Model(&database.ScrobbleSession{}).
		Where("episode_id IN ?", episodeIDs).
		Where("expires_at > ?", time.Now()).
		Where("action NOT IN ?", []string{"pause", "stop"}).
		Distinct().
		Pluck("user_id", &userIDs).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	return serveWatchingUsers(c, db, userIDs)
}

func handleEpisodeWatching(c *fiber.Ctx) error {
	db, _, _, episode, err := resolveShowEpisode(c)
	if err != nil {
		return err
	}

	var userIDs []string
	if err := db.Model(&database.ScrobbleSession{}).
		Where("episode_id = ?", episode.ID).
		Where("expires_at > ?", time.Now()).
		Where("action NOT IN ?", []string{"pause", "stop"}).
		Distinct().
		Pluck("user_id", &userIDs).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	return serveWatchingUsers(c, db, userIDs)
}

// serveWatchingUsers renders the distinct watching users as the contract's user
// objects, in a stable order.
func serveWatchingUsers(c *fiber.Ctx, db *gorm.DB, userIDs []string) error {
	users, err := contractshape.UserByIDBatch(db, userIDs)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	out := make([]map[string]any, 0, len(userIDs))
	seen := map[string]bool{}
	for _, id := range userIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, contractshape.User(users[id]))
	}
	return c.JSON(out)
}
