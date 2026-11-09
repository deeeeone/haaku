package sync

// Episode-level collection (issue #71).
//
// `user_collection` rows with media_type='episode' are the source of truth for
// episode state; a season (or a show with seasons/episodes) is expanded to
// those rows on write. The UNIQUE(user_id, media_type, media_id) key makes the
// expansion idempotent: adding a season that contains an already-collected
// episode counts it as existing, never as a second row.
//
// media_type='show' rows keep their old meaning and behaviour: a show posted
// without seasons still writes one show row, and removing it removes only that
// row.

import (
	"errors"
	"strconv"
	"time"

	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/outbox"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// collectionRequest is the Trakt bulk body shared by add and remove. Shows may
// carry seasons (all episodes when `episodes` is omitted); a top-level season
// names its show by `ids` and its season by `number` (or names the season
// itself by one of its external ids when `number` is absent).
type collectionRequest struct {
	Movies   []collectionMovie   `json:"movies,omitempty"`
	Shows    []collectionShow    `json:"shows,omitempty"`
	Seasons  []collectionSeason  `json:"seasons,omitempty"`
	Episodes []collectionEpisode `json:"episodes,omitempty"`
}

type collectionMovie struct {
	IDs         idsObj `json:"ids"`
	CollectedAt string `json:"collected_at,omitempty"`
}

type collectionShow struct {
	IDs         idsObj             `json:"ids"`
	Seasons     []collectionSeason `json:"seasons,omitempty"`
	CollectedAt string             `json:"collected_at,omitempty"`
}

type collectionSeason struct {
	IDs         idsObj              `json:"ids,omitempty"`
	Number      int                 `json:"number,omitempty"`
	Episodes    []collectionEpisode `json:"episodes,omitempty"`
	CollectedAt string              `json:"collected_at,omitempty"`
}

type collectionEpisode struct {
	IDs         idsObj `json:"ids"`
	Season      int    `json:"season,omitempty"`
	Number      int    `json:"number,omitempty"`
	CollectedAt string `json:"collected_at,omitempty"`
}

// collectionCounts is the Trakt added/updated/existing/deleted shape. Seasons
// is kept for the web client's existing SyncAddResponse type; season writes
// expand to episode rows, so it stays 0.
type collectionCounts struct {
	Movies   int `json:"movies"`
	Shows    int `json:"shows"`
	Episodes int `json:"episodes"`
	Seasons  int `json:"seasons"`
}

type collectionAddResponse struct {
	Added      collectionCounts       `json:"added"`
	Updated    collectionCounts       `json:"updated"`
	Existing   collectionCounts       `json:"existing"`
	Duplicates collectionCounts       `json:"duplicates"`
	NotFound   map[string]interface{} `json:"not_found"`
}

type collectionRemoveResponse struct {
	Deleted  collectionCounts       `json:"deleted"`
	NotFound map[string]interface{} `json:"not_found"`
}

func addCollection(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req collectionRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}

	resp := collectionAddResponse{}
	var notFound notFoundList
	now := time.Now()
	var movieIDs, showIDs []int64
	var episodes []database.Episode

	dbErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		refs, err := resolveCollectionRefs(tx, req)
		if err != nil {
			return nil, err
		}

		for _, m := range req.Movies {
			ref, ok := refs[idRefFor(m.IDs).WithType("movie")]
			if !ok || ref.MediaType != "movie" {
				notFound.movie(m.IDs)
				continue
			}
			at, err := parseCollectionTime(m.CollectedAt, now, nil)
			if err != nil {
				notFound.movie(map[string]interface{}{"ids": m.IDs, "error": "invalid collected_at"})
				continue
			}
			added, err := insertCollectionRow(tx, userID, "movie", ref.MediaID, at, &resp.Added.Movies, &resp.Existing.Movies)
			if err != nil {
				return nil, err
			}
			if added {
				movieIDs = append(movieIDs, ref.MediaID)
			}
		}

		for _, s := range req.Shows {
			ref, ok := refs[idRefFor(s.IDs).WithType("show")]
			if !ok || ref.MediaType != "show" {
				notFound.show(s.IDs)
				continue
			}
			showID := ref.MediaID

			// A show without nested seasons keeps the legacy show-level row.
			if len(s.Seasons) == 0 {
				at, err := parseCollectionTime(s.CollectedAt, now, nil)
				if err != nil {
					notFound.show(map[string]interface{}{"ids": s.IDs, "error": "invalid collected_at"})
					continue
				}
				added, err := insertCollectionRow(tx, userID, "show", showID, at, &resp.Added.Shows, &resp.Existing.Shows)
				if err != nil {
					return nil, err
				}
				if added {
					showIDs = append(showIDs, showID)
				}
				continue
			}

			// A show with seasons expands to the episodes in those seasons.
			for _, season := range s.Seasons {
				seasonRow, ok := findSeason(tx, showID, season.Number)
				if !ok {
					notFound.season(map[string]interface{}{"ids": s.IDs, "season": season.Number})
					continue
				}
				at, err := parseCollectionTime(firstNonEmpty(season.CollectedAt, s.CollectedAt), now, nil)
				if err != nil {
					notFound.season(map[string]interface{}{"ids": s.IDs, "season": season.Number, "error": "invalid collected_at"})
					continue
				}
				if err := collectSeason(tx, userID, seasonRow, season.Episodes, at, now, &resp, &notFound, &episodes); err != nil {
					return nil, err
				}
			}
		}

		for _, s := range req.Seasons {
			seasonRow, ok := resolveCollectionSeason(tx, refs, s)
			if !ok {
				notFound.season(map[string]interface{}{"ids": s.IDs, "number": s.Number})
				continue
			}
			at, err := parseCollectionTime(s.CollectedAt, now, nil)
			if err != nil {
				notFound.season(map[string]interface{}{"ids": s.IDs, "number": s.Number, "error": "invalid collected_at"})
				continue
			}
			if err := collectSeason(tx, userID, seasonRow, s.Episodes, at, now, &resp, &notFound, &episodes); err != nil {
				return nil, err
			}
		}

		for _, e := range req.Episodes {
			epRow, ids, ok, err := resolveCollectionEpisode(tx, refs, e)
			if err != nil {
				return nil, err
			}
			if !ok {
				notFound.episode(map[string]interface{}{"ids": ids, "season": e.Season, "number": e.Number})
				continue
			}
			at, err := parseCollectionTime(e.CollectedAt, now, epRow.AiredAt)
			if err != nil {
				notFound.episode(map[string]interface{}{"ids": ids, "season": e.Season, "number": e.Number, "error": "invalid collected_at"})
				continue
			}
			if err := collectEpisode(tx, userID, *epRow, at, &resp, &episodes); err != nil {
				return nil, err
			}
		}

		resp.Duplicates = resp.Existing
		resp.NotFound = notFound.payload()

		if countsChanged(resp.Added) {
			queueUserInvalidation(cacheTx, userID)
			for _, id := range movieIDs {
				queueMediaInvalidation(cacheTx, "movie", id)
			}
			for _, id := range showIDs {
				queueMediaInvalidation(cacheTx, "show", id)
			}
			for _, ep := range episodes {
				queueEpisodeInvalidations(tx, cacheTx, ep)
			}
			if err := outbox.PublishEvent(tx, outbox.EventCollectionAdded, userID, map[string]any{
				"movies_added":   resp.Added.Movies,
				"shows_added":    resp.Added.Shows,
				"episodes_added": resp.Added.Episodes,
			}); err != nil {
				return nil, err
			}
		}

		return cacheTx, nil
	})
	if dbErr != nil {
		return response.InternalError(c)
	}

	return c.JSON(resp)
}

func removeCollection(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req collectionRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}

	resp := collectionRemoveResponse{}
	var notFound notFoundList
	var movieIDs, showIDs []int64
	var episodes []database.Episode

	dbErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		refs, err := resolveCollectionRefs(tx, req)
		if err != nil {
			return nil, err
		}

		for _, m := range req.Movies {
			ref, ok := refs[idRefFor(m.IDs).WithType("movie")]
			if !ok || ref.MediaType != "movie" {
				notFound.movie(m.IDs)
				continue
			}
			result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "movie", ref.MediaID).
				Delete(&database.UserCollection{})
			if result.RowsAffected > 0 {
				resp.Deleted.Movies += int(result.RowsAffected)
				movieIDs = append(movieIDs, ref.MediaID)
			}
		}

		for _, s := range req.Shows {
			ref, ok := refs[idRefFor(s.IDs).WithType("show")]
			if !ok || ref.MediaType != "show" {
				notFound.show(s.IDs)
				continue
			}
			showID := ref.MediaID

			// No seasons: the legacy show-level row, exactly as before.
			if len(s.Seasons) == 0 {
				result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "show", showID).
					Delete(&database.UserCollection{})
				if result.RowsAffected > 0 {
					resp.Deleted.Shows += int(result.RowsAffected)
					showIDs = append(showIDs, showID)
				}
				continue
			}

			for _, season := range s.Seasons {
				seasonRow, ok := findSeason(tx, showID, season.Number)
				if !ok {
					notFound.season(map[string]interface{}{"ids": s.IDs, "season": season.Number})
					continue
				}
				if err := removeSeasonEpisodes(tx, userID, seasonRow, season.Episodes, &resp.Deleted, &notFound, &episodes); err != nil {
					return nil, err
				}
			}
		}

		for _, s := range req.Seasons {
			seasonRow, ok := resolveCollectionSeason(tx, refs, s)
			if !ok {
				notFound.season(map[string]interface{}{"ids": s.IDs, "number": s.Number})
				continue
			}
			if err := removeSeasonEpisodes(tx, userID, seasonRow, s.Episodes, &resp.Deleted, &notFound, &episodes); err != nil {
				return nil, err
			}
		}

		for _, e := range req.Episodes {
			epRow, ids, ok, err := resolveCollectionEpisode(tx, refs, e)
			if err != nil {
				return nil, err
			}
			if !ok {
				notFound.episode(map[string]interface{}{"ids": ids, "season": e.Season, "number": e.Number})
				continue
			}
			result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "episode", epRow.ID).
				Delete(&database.UserCollection{})
			if result.RowsAffected > 0 {
				resp.Deleted.Episodes += int(result.RowsAffected)
				episodes = append(episodes, *epRow)
			}
		}

		resp.NotFound = notFound.payload()

		if countsChanged(resp.Deleted) {
			queueUserInvalidation(cacheTx, userID)
			for _, id := range movieIDs {
				queueMediaInvalidation(cacheTx, "movie", id)
			}
			for _, id := range showIDs {
				queueMediaInvalidation(cacheTx, "show", id)
			}
			for _, ep := range episodes {
				queueEpisodeInvalidations(tx, cacheTx, ep)
			}
			// EventCollectionAdded's wire value is "collection.updated",
			// which correctly covers both adds and removals.
			if err := outbox.PublishEvent(tx, outbox.EventCollectionAdded, userID, map[string]any{
				"movies_deleted":   resp.Deleted.Movies,
				"shows_deleted":    resp.Deleted.Shows,
				"episodes_deleted": resp.Deleted.Episodes,
			}); err != nil {
				return nil, err
			}
		}

		return cacheTx, nil
	})
	if dbErr != nil {
		return response.InternalError(c)
	}

	return c.JSON(resp)
}

// ============================================================================
// WRITE HELPERS
// ============================================================================

// resolveCollectionRefs batch-resolves every reference that names a show (a
// top-level season with a number, an episode with season/number) or another
// media type. Episode and season ids that name the row itself are resolved by
// a direct external_ids lookup, because those types have no resolver columns.
func resolveCollectionRefs(tx *gorm.DB, req collectionRequest) (map[media.IDRef]media.ResolvedMedia, error) {
	var refs []media.IDRef
	for _, m := range req.Movies {
		refs = append(refs, idRefFor(m.IDs).WithType("movie"))
	}
	for _, s := range req.Shows {
		refs = append(refs, idRefFor(s.IDs).WithType("show"))
	}
	for _, s := range req.Seasons {
		if s.Number > 0 {
			refs = append(refs, idRefFor(s.IDs).WithType("show"))
		}
	}
	for _, e := range req.Episodes {
		if e.Season > 0 && e.Number > 0 {
			refs = append(refs, idRefFor(e.IDs).WithType("show"))
		}
	}
	return media.ResolveIDsBatch(tx, refs)
}

func resolveCollectionSeason(tx *gorm.DB, refs map[media.IDRef]media.ResolvedMedia, s collectionSeason) (*database.Season, bool) {
	if s.Number > 0 {
		ref, ok := refs[idRefFor(s.IDs).WithType("show")]
		if !ok || ref.MediaType != "show" {
			return nil, false
		}
		return findSeason(tx, ref.MediaID, s.Number)
	}

	// No number: the ids name the season row itself.
	seasonID, ok := lookupExternalMediaID(tx, s.IDs, "season")
	if !ok {
		return nil, false
	}
	var season database.Season
	if err := tx.First(&season, seasonID).Error; err != nil {
		return nil, false
	}
	return &season, true
}

func resolveCollectionEpisode(tx *gorm.DB, refs map[media.IDRef]media.ResolvedMedia, e collectionEpisode) (*database.Episode, idsObj, bool, error) {
	if e.Season > 0 && e.Number > 0 {
		ref, ok := refs[idRefFor(e.IDs).WithType("show")]
		if !ok || ref.MediaType != "show" {
			return nil, e.IDs, false, nil
		}
		season, ok := findSeason(tx, ref.MediaID, e.Season)
		if !ok {
			return nil, e.IDs, false, nil
		}
		ep, ok := findEpisode(tx, season.ID, e.Number)
		return ep, e.IDs, ok, nil
	}

	// No season/episode number: the ids name the episode row itself.
	episodeID, ok := lookupExternalMediaID(tx, e.IDs, "episode")
	if !ok {
		return nil, e.IDs, false, nil
	}
	var ep database.Episode
	if err := tx.First(&ep, episodeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, e.IDs, false, nil
		}
		return nil, e.IDs, false, err
	}
	return &ep, e.IDs, true, nil
}

// lookupExternalMediaID resolves an id that names a season or episode row via
// the external_ids table. Those types have no tmdb_id/tvdb_id columns, so this
// is the only way a standalone season/episode reference can be resolved.
func lookupExternalMediaID(tx *gorm.DB, ids idsObj, mediaType string) (int64, bool) {
	candidates := []struct {
		source string
		value  string
	}{
		{"tmdb", optionalInt(ids.TMDB)},
		{"tvdb", optionalInt(ids.TVDB)},
		{"imdb", ids.IMDB},
	}
	for _, candidate := range candidates {
		if candidate.value == "" {
			continue
		}
		var ext database.ExternalID
		if err := tx.Where("media_type = ? AND source = ? AND external_id = ?",
			mediaType, candidate.source, candidate.value).First(&ext).Error; err == nil {
			return ext.MediaID, true
		}
	}
	return 0, false
}

func optionalInt(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// collectSeason expands a season to its episode collection rows. When the
// payload names specific episodes, only those are written; otherwise every
// episode in the season is.
func collectSeason(tx *gorm.DB, userID string, season *database.Season, wanted []collectionEpisode, at, now time.Time, resp *collectionAddResponse, notFound *notFoundList, episodes *[]database.Episode) error {
	if len(wanted) == 0 {
		for _, ep := range findAllEpisodesInSeason(tx, season.ID) {
			if err := collectEpisode(tx, userID, ep, at, resp, episodes); err != nil {
				return err
			}
		}
		return nil
	}

	for _, w := range wanted {
		ep, ok := findEpisode(tx, season.ID, w.Number)
		if !ok {
			notFound.episode(map[string]interface{}{"ids": w.IDs, "season": season.SeasonNumber, "number": w.Number})
			continue
		}
		epAt, err := parseCollectionTime(w.CollectedAt, now, ep.AiredAt)
		if err != nil {
			notFound.episode(map[string]interface{}{"ids": w.IDs, "season": season.SeasonNumber, "number": w.Number, "error": "invalid collected_at"})
			continue
		}
		if err := collectEpisode(tx, userID, *ep, epAt, resp, episodes); err != nil {
			return err
		}
	}
	return nil
}

func collectEpisode(tx *gorm.DB, userID string, ep database.Episode, at time.Time, resp *collectionAddResponse, episodes *[]database.Episode) error {
	added, err := insertCollectionRow(tx, userID, "episode", ep.ID, at, &resp.Added.Episodes, &resp.Existing.Episodes)
	if err != nil {
		return err
	}
	if added {
		*episodes = append(*episodes, ep)
	}
	return nil
}

// insertCollectionRow inserts one collection row and reports whether it was
// new. The UNIQUE(user_id, media_type, media_id) key makes a repeat add an
// existing row rather than a duplicate.
func insertCollectionRow(tx *gorm.DB, userID, mediaType string, mediaID int64, at time.Time, added, existing *int) (bool, error) {
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&database.UserCollection{
		UserID: userID, MediaType: mediaType, MediaID: mediaID, CollectedAt: at,
	})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		*added++
		return true, nil
	}
	*existing++
	return false, nil
}

func removeSeasonEpisodes(tx *gorm.DB, userID string, season *database.Season, wanted []collectionEpisode, deleted *collectionCounts, notFound *notFoundList, episodes *[]database.Episode) error {
	if len(wanted) == 0 {
		eps := findAllEpisodesInSeason(tx, season.ID)
		ids := make([]int64, 0, len(eps))
		for _, ep := range eps {
			ids = append(ids, ep.ID)
		}
		if len(ids) == 0 {
			return nil
		}
		result := tx.Where("user_id = ? AND media_type = ? AND media_id IN ?", userID, "episode", ids).
			Delete(&database.UserCollection{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			deleted.Episodes += int(result.RowsAffected)
			*episodes = append(*episodes, eps...)
		}
		return nil
	}

	for _, w := range wanted {
		ep, ok := findEpisode(tx, season.ID, w.Number)
		if !ok {
			notFound.episode(map[string]interface{}{"ids": w.IDs, "season": season.SeasonNumber, "number": w.Number})
			continue
		}
		result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "episode", ep.ID).
			Delete(&database.UserCollection{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			deleted.Episodes += int(result.RowsAffected)
			*episodes = append(*episodes, *ep)
		}
	}
	return nil
}

func countsChanged(c collectionCounts) bool {
	return c.Movies+c.Shows+c.Episodes+c.Seasons > 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseCollectionTime accepts the Trakt collected_at forms: empty means now,
// "released" means the item's air/release date when known, and otherwise an
// RFC3339 timestamp.
func parseCollectionTime(val string, now time.Time, airedAt *database.Date) (time.Time, error) {
	switch val {
	case "":
		return now, nil
	case "released":
		if airedAt != nil && !airedAt.IsZero() {
			return airedAt.Time, nil
		}
		return now, nil
	default:
		t, err := time.Parse(time.RFC3339, val)
		if err != nil {
			return time.Time{}, err
		}
		return t, nil
	}
}
