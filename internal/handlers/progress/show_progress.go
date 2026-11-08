package progress

import (
	"sort"
	"time"

	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/outbox"
	"github.com/haaku-tv/haaku/internal/response"
	"github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
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
// Deliberately absent: Trakt's `reset_at`. Nothing stores when a reset
// happened, so the field is omitted rather than invented; a client that needs
// it can track the reset call itself. Show-level rank/plays/watchers would
// need the stats engine (issue #5) and are not part of this payload.
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

	markers, err := loadMarkers(db, userID, ids, collection)
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
func loadMarkers(db *gorm.DB, userID string, episodeIDs []int64, collection bool) (map[int64]time.Time, error) {
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
		if prev, ok := markers[r.MediaID]; !ok || r.WatchedAt.After(prev) {
			markers[r.MediaID] = r.WatchedAt
		}
	}
	return markers, nil
}

// publishHistoryRemoved is the outbox hook the reset handler calls. It is a
// variable so the sqlite-backed handler tests can substitute a recorder: the
// real outbox insert uses Postgres-only `?::jsonb` SQL that sqlite cannot
// execute. Production always uses outbox.PublishEvent.
var publishHistoryRemoved = outbox.PublishEvent

// POST /v1/shows/{id}/progress/watched/reset
//
// Deletes this caller's episode watch-history rows for this show only:
//
//	DELETE FROM user_history
//	 WHERE user_id = <caller>
//	   AND media_type = 'episode'
//	   AND media_id IN (SELECT id FROM episodes WHERE show_id = <show>)
//
// It must not — and does not — touch the caller's ratings, collection,
// playback/resume positions, comments, or history for any other show/movie.
// The show-level history row (media_type='show'), if one exists, is also left
// alone: this is an episode-progress reset, not "forget the show".
//
// Returns the recomputed watched progress after the delete (Trakt's shape).
func handleShowProgressReset(c *fiber.Ctx) error {
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

	var episodeIDs []int64
	if err := db.Model(&database.Episode{}).Where("show_id = ?", showID).Pluck("id", &episodeIDs).Error; err != nil {
		return response.InternalError(c, "database query error")
	}

	deleted := 0
	txErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if len(episodeIDs) > 0 {
			result := tx.Where("user_id = ? AND media_type = ? AND media_id IN ?", userID, "episode", episodeIDs).
				Delete(&database.UserHistory{})
			if result.Error != nil {
				return nil, result.Error
			}
			deleted = int(result.RowsAffected)
		}

		if deleted > 0 {
			queueUserInvalidation(cacheTx, userID)
			queueShowInvalidation(cacheTx, showID)
			if err := publishHistoryRemoved(tx, outbox.EventHistoryRemoved, userID, map[string]any{
				"show_id": showID,
				"deleted": deleted,
			}); err != nil {
				return nil, err
			}
		}

		return cacheTx, nil
	})
	if txErr != nil {
		return response.InternalError(c, "failed to reset progress")
	}

	resp, err := buildShowProgress(db, userID, showID, false)
	if err != nil {
		return response.InternalError(c, "database query error")
	}

	return c.JSON(resp)
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
