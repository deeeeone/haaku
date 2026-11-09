package tmdbmeta

import (
	"sort"
	"time"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Watching rows: the users currently watching a season or an episode.
//
// The predicate is the one this repo already trusts for "watching right now"
// (see users.handleUserWatching): a user's *latest* scrobble session is a live,
// non-expired start/scrobble, not a pause/stop. A saved resume position
// (user_playback.paused_at) is not evidence of current playback and is not used.
// Every value in the response comes from a stored session row joined to a stored
// user row.
//
// "Latest" is chosen before the episode/action/expiry filters are applied, and
// includes stop and scrobble events. A stop session (and a scrobble) is written
// with a zero expires_at, so filtering on expiry first would leave an earlier
// unexpired start marking the user as watching after they stopped.
const latestSessionPerUser = `id = (
	SELECT s2.id FROM scrobble_sessions s2
	WHERE s2.user_id = scrobble_sessions.user_id
	ORDER BY s2.created_at DESC, s2.id DESC
	LIMIT 1
)`

// watchingUserQuery is the shared base: each user's most recent session, still
// live, whose action is a start/scrobble. Callers add the episode scope.
func watchingUserQuery(db *gorm.DB) *gorm.DB {
	return db.Model(&database.ScrobbleSession{}).
		Where(latestSessionPerUser).
		Where("expires_at > ?", time.Now()).
		Where("action NOT IN ?", []string{"pause", "stop"})
}

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
	if err := watchingUserQuery(db).
		Where("episode_id IN ?", episodeIDs).
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
	if err := watchingUserQuery(db).
		Where("episode_id = ?", episode.ID).
		Pluck("user_id", &userIDs).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	return serveWatchingUsers(c, db, userIDs)
}

// serveWatchingUsers renders the distinct watching users as the contract's user
// objects, in a stable order, one requested page at a time.
func serveWatchingUsers(c *fiber.Ctx, db *gorm.DB, userIDs []string) error {
	// The list names other accounts, so every candidate is put through the same
	// activity-section visibility decision the profile routes enforce. Without
	// this, a user who hides their activity — or who blocks, or is blocked by,
	// the caller — would still appear here. A visibility-query failure is a 500,
	// never the unfiltered list: serving it would be the same leak with an extra
	// step.
	caller, _ := c.Locals("user_id").(string)
	visible, err := auth.VisibleUserIDs(db, caller, userIDs, "activity")
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	users, err := contractshape.UserByIDBatch(db, visible)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	// A stable order is required before slicing into pages, otherwise the same
	// user can land on two pages or none. Sort the full set, then return only
	// the requested page; the total comes from the full set.
	sort.Slice(visible, func(i, j int) bool { return visible[i] < visible[j] })
	out := make([]map[string]any, 0, len(visible))
	for _, id := range visible {
		out = append(out, contractshape.User(users[id]))
	}

	page, limit := response.ParsePagination(c, len(out))
	if limit < 1 {
		limit = 1
	}
	start := (page - 1) * limit
	if start > len(out) {
		start = len(out)
	}
	end := start + limit
	if end > len(out) {
		end = len(out)
	}
	pageItems := out[start:end]
	response.SetPaginationHeaders(c, page, limit, int64(len(out)), len(pageItems))
	return c.JSON(pageItems)
}
