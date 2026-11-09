package progress

import (
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/response"
	"github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ---------------------------------------------------------------------------
// Watch / collection progress for a show (Trakt: /shows/{id}/progress/*)
// ---------------------------------------------------------------------------

// ShowProgressEpisode is one episode inside a season's progress breakdown.
//
// Watched progress lists every aired episode with completed + last_watched_at.
// Collection progress lists only episodes the caller has collected, carrying
// collected_at. Both use `number` as the episode number within the season.
type ShowProgressEpisode struct {
	Number        int     `json:"number"`
	Completed     bool    `json:"completed"`
	LastWatchedAt *string `json:"last_watched_at,omitempty"`
	CollectedAt   *string `json:"collected_at,omitempty"`
}

// ShowSeasonProgress is the per-season slice of a show's progress.
type ShowSeasonProgress struct {
	Number    int                   `json:"number"`
	Aired     int                   `json:"aired"`
	Completed int                   `json:"completed"`
	Episodes  []ShowProgressEpisode `json:"episodes"`
}

// ShowProgressResponse is the watched/collection progress payload.
//
// Counts are real: `aired` is episodes whose aired_at is today or earlier,
// and `completed` is how many of those the caller has watched (history) or
// collected. Episodes with no aired_at are treated as not aired — the schema
// cannot prove they aired, so counting them would be fabrication.
//
// The contract types `reset_at` as null, so it is not emitted. A reset point
// is stored per user and show (user_show_progress_resets); watched progress
// ignores plays older than it while the history rows themselves stay intact.
// Show-level rank/plays/watchers are not part of this payload.
type ShowProgressResponse struct {
	Aired           int                    `json:"aired"`
	Completed       int                    `json:"completed"`
	LastWatchedAt   *string                `json:"last_watched_at,omitempty"`
	LastCollectedAt *string                `json:"last_collected_at,omitempty"`
	Seasons         []ShowSeasonProgress   `json:"seasons"`
	NextEpisode     *media.EpisodeResponse `json:"next_episode"`
}

// GET /v1/shows/{id}/progress/watched
func handleShowProgressWatched(c *fiber.Ctx) error {
	return showProgress(c, false)
}

// GET /v1/shows/{id}/progress/collection
func handleShowProgressCollection(c *fiber.Ctx) error {
	return showProgress(c, true)
}

// showProgress resolves the show and renders watched (collection=false) or
// collection (collection=true) progress for the caller.
func showProgress(c *fiber.Ctx, collection bool) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return response.Unauthorized(c)
	}

	_, showID, err := media.ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c, "database unavailable")
	}

	resp, err := buildShowProgress(db, userID, showID, collection)
	if err != nil {
		return response.InternalError(c, "database query error")
	}

	return c.JSON(resp)
}

// buildShowProgress computes show progress from the episodes table plus either
// user_history (watched) or user_collection (collection). One query each for
// episodes, seasons and the caller's markers — no per-episode queries.
func buildShowProgress(db *gorm.DB, userID string, showID int64, collection bool) (*ShowProgressResponse, error) {
	var episodes []database.Episode
	if err := db.Where("show_id = ?", showID).Find(&episodes).Error; err != nil {
		return nil, err
	}

	var seasons []database.Season
	if err := db.Where("show_id = ?", showID).Find(&seasons).Error; err != nil {
		return nil, err
	}
	seasonNumbers := make(map[int64]int, len(seasons))
	for _, s := range seasons {
		seasonNumbers[s.ID] = s.SeasonNumber
	}

	ids := make([]int64, 0, len(episodes))
	for _, e := range episodes {
		ids = append(ids, e.ID)
	}

	var resetAt *time.Time
	if !collection {
		var reset database.UserShowProgressReset
		err := db.Where("user_id = ? AND show_id = ?", userID, showID).First(&reset).Error
		switch {
		case err == nil:
			at := reset.ResetAt
			resetAt = &at
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return nil, err
		}
	}

	markers, err := loadMarkers(db, userID, ids, collection, resetAt)
	if err != nil {
		return nil, err
	}

	// Season-major, episode-minor order keeps the breakdown and the watched
	// episode lists deterministic regardless of insertion order.
	sort.SliceStable(episodes, func(i, j int) bool {
		si, sj := seasonNumbers[episodes[i].SeasonID], seasonNumbers[episodes[j].SeasonID]
		if si != sj {
			return si < sj
		}
		return episodes[i].EpisodeNumber < episodes[j].EpisodeNumber
	})

	today := time.Now().UTC().Format("2006-01-02")

	resp := &ShowProgressResponse{
		Seasons: make([]ShowSeasonProgress, 0),
	}
	seasonIndex := make(map[int]int)
	var lastAt *time.Time
	var next *database.Episode

	for i := range episodes {
		ep := &episodes[i]
		seasonNo, known := seasonNumbers[ep.SeasonID]
		if !known {
			// A missing season row means the episode's season number is
			// unknowable; report it under 0 rather than dropping the row.
			seasonNo = 0
		}

		marker, hasMarker := markers[ep.ID]
		if hasMarker && (lastAt == nil || marker.After(*lastAt)) {
			t := marker
			lastAt = &t
		}

		aired := ep.AiredAt != nil && !ep.AiredAt.IsZero() && ep.AiredAt.Format("2006-01-02") <= today
		if !aired {
			if ep.AiredAt != nil && !ep.AiredAt.IsZero() {
				// Earliest air date wins; ties keep the earlier season/episode
				// because the slice is already in that order.
				if next == nil || ep.AiredAt.Format("2006-01-02") < next.AiredAt.Format("2006-01-02") {
					next = ep
				}
			}
			continue
		}

		idx, ok := seasonIndex[seasonNo]
		if !ok {
			seasonIndex[seasonNo] = len(resp.Seasons)
			idx = len(resp.Seasons)
			resp.Seasons = append(resp.Seasons, ShowSeasonProgress{
				Number:   seasonNo,
				Episodes: make([]ShowProgressEpisode, 0),
			})
		}

		season := &resp.Seasons[idx]
		season.Aired++
		resp.Aired++

		completed := hasMarker
		if completed {
			season.Completed++
			resp.Completed++
		}

		entry := ShowProgressEpisode{
			Number:    ep.EpisodeNumber,
			Completed: completed,
		}
		if collection {
			// Collection progress lists what is owned, so an aired episode
			// the caller has not collected is not a row here.
			if !completed {
				continue
			}
			at := response.FormatTime(marker.UTC())
			entry.CollectedAt = &at
		} else if completed {
			at := response.FormatTime(marker.UTC())
			entry.LastWatchedAt = &at
		}

		season.Episodes = append(season.Episodes, entry)
	}

	if lastAt != nil {
		formatted := response.FormatTime(lastAt.UTC())
		if collection {
			resp.LastCollectedAt = &formatted
		} else {
			resp.LastWatchedAt = &formatted
		}
	}

	if next != nil {
		episode := media.BuildEpisodeResponse(next)
		resp.NextEpisode = &episode
	}

	return resp, nil
}

// loadMarkers returns episode id -> most recent watched_at/collected_at for
// the caller. Multiple history rows can exist per episode (the dedupe index
// includes source/source_id), so the newest marker per episode wins; the fold
// happens in Go because MAX() over a timestamp loses the column's declared
// type and sqlite returns it as an unparseable string.
//
// For watched progress, a resetAt point hides plays before it: the row is
// skipped here, never deleted, so undoing the reset restores the progress.
func loadMarkers(db *gorm.DB, userID string, episodeIDs []int64, collection bool, resetAt *time.Time) (map[int64]time.Time, error) {
	markers := make(map[int64]time.Time, len(episodeIDs))
	if len(episodeIDs) == 0 {
		return markers, nil
	}

	if collection {
		var rows []struct {
			MediaID     int64
			CollectedAt time.Time
		}
		if err := db.Model(&database.UserCollection{}).
			Select("media_id, collected_at").
			Where("user_id = ? AND media_type = ? AND media_id IN ?", userID, "episode", episodeIDs).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			if prev, ok := markers[r.MediaID]; !ok || r.CollectedAt.After(prev) {
				markers[r.MediaID] = r.CollectedAt
			}
		}
		return markers, nil
	}

	var rows []struct {
		MediaID   int64
		WatchedAt time.Time
	}
	if err := db.Model(&database.UserHistory{}).
		Select("media_id, watched_at").
		Where("user_id = ? AND media_type = ? AND media_id IN ?", userID, "episode", episodeIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if resetAt != nil && r.WatchedAt.Before(*resetAt) {
			continue
		}
		if prev, ok := markers[r.MediaID]; !ok || r.WatchedAt.After(prev) {
			markers[r.MediaID] = r.WatchedAt
		}
	}
	return markers, nil
}

// ShowProgressResetResponse is the contract's reset acknowledgement.
type ShowProgressResetResponse struct {
	ResetAt string `json:"reset_at"`
}

// POST /v1/shows/{id}/progress/watched/reset
//
// Records a reset point for the caller and this show. This is Trakt's
// semantics: history is NOT deleted. Watched progress counts only plays at or
// after reset_at, so the caller can re-watch from here while the old rows stay
// in their history. The optional body accepts the contract's reset_at (defaults
// to now, may not be in the future).
//
// Ratings, collection, playback positions and every other show's history are
// untouched. The status stays 200 because that is the status this route has
// always advertised; the body is the contract's {reset_at}.
func handleShowProgressReset(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return response.Unauthorized(c)
	}

	_, showID, err := media.ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	if _, dbErr := database.GetDB(); dbErr != nil {
		return response.InternalError(c, "database unavailable")
	}

	resetAt := time.Now().UTC()
	if len(c.Body()) > 0 {
		var body struct {
			ResetAt *time.Time `json:"reset_at"`
		}
		if err := json.Unmarshal(c.Body(), &body); err != nil {
			return response.BadRequest(c, "invalid reset body")
		}
		if body.ResetAt != nil {
			if body.ResetAt.After(time.Now()) {
				return response.BadRequest(c, "reset_at cannot be in the future")
			}
			resetAt = body.ResetAt.UTC()
		}
	}

	txErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		row := database.UserShowProgressReset{UserID: userID, ShowID: showID, ResetAt: resetAt}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "show_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"reset_at"}),
		}).Create(&row).Error; err != nil {
			return nil, err
		}
		queueUserInvalidation(cacheTx, userID)
		queueShowInvalidation(cacheTx, showID)
		return cacheTx, nil
	})
	if txErr != nil {
		return response.InternalError(c, "failed to reset progress")
	}

	return c.JSON(ShowProgressResetResponse{ResetAt: response.FormatTime(resetAt)})
}

// DELETE /v1/shows/{id}/progress/watched/reset
//
// Undo a reset: deleting the row restores the progress the caller had before
// it. The contract answers 204, and deleting nothing is still 204 (the call is
// idempotent).
func handleShowProgressResetUndo(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return response.Unauthorized(c)
	}

	_, showID, err := media.ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	if _, dbErr := database.GetDB(); dbErr != nil {
		return response.InternalError(c, "database unavailable")
	}

	txErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := tx.Where("user_id = ? AND show_id = ?", userID, showID).
			Delete(&database.UserShowProgressReset{}).Error; err != nil {
			return nil, err
		}
		queueUserInvalidation(cacheTx, userID)
		queueShowInvalidation(cacheTx, showID)
		return cacheTx, nil
	})
	if txErr != nil {
		return response.InternalError(c, "failed to undo progress reset")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// queueUserInvalidation bumps the caller's user version key so cached
// user-scoped payloads are invalidated in O(1) after the reset.
func queueUserInvalidation(cacheTx *cache.CacheTransaction, userID string) {
	vk := redis.BuildUserVersionKey(userID)
	cacheTx.AddInvalidation(vk, map[string]interface{}{
		"user_id":      userID,
		"version_key":  vk,
		"version_only": true,
	})
}

// queueShowInvalidation bumps the show's media version key (cached show
// payloads embed watch state).
func queueShowInvalidation(cacheTx *cache.CacheTransaction, showID int64) {
	vk := redis.BuildMediaVersionKey("show", showID, nil, nil)
	cacheTx.AddInvalidation(vk, map[string]interface{}{
		"media_id":     showID,
		"media_type":   "show",
		"version_key":  vk,
		"version_only": true,
	})
}
