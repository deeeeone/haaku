package sync

// Collected episode reads (issue #71). The write side lives in collection.go;
// this file only projects the episode collection rows into Trakt's
// collectedEpisode shape.

import (
	"strconv"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ============================================================================
// COLLECTED EPISODES READ
// ============================================================================

type collectedEpisodeResponse struct {
	Type        string                 `json:"type"`
	CollectedAt string                 `json:"collected_at"`
	UpdatedAt   string                 `json:"updated_at"`
	Episode     collectedEpisodeObject `json:"episode"`
	Show        collectedShowObject    `json:"show"`
}

type collectedEpisodeObject struct {
	Season int         `json:"season"`
	Number int         `json:"number"`
	Title  string      `json:"title"`
	IDs    traktIDsObj `json:"ids"`
}

type collectedShowObject struct {
	Title string      `json:"title"`
	Year  int         `json:"year"`
	IDs   traktIDsObj `json:"ids"`
}

// traktIDsObj is the Trakt ids shape for this read: unlike /v1/movies/{id},
// TVDB is an integer here, matching Trakt's standard media objects.
type traktIDsObj struct {
	Trakt int    `json:"trakt,omitempty"`
	Slug  string `json:"slug,omitempty"`
	TVDB  int    `json:"tvdb,omitempty"`
	TMDB  int    `json:"tmdb,omitempty"`
	IMDB  string `json:"imdb,omitempty"`
}

// handleGetCollectionEpisodes returns the caller's collected episodes newest
// first, paginated like the other sync reads, in Trakt's collectedEpisode
// shape (array body, X-Pagination headers).
func handleGetCollectionEpisodes(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 25)
	query := db.Model(&database.UserCollection{}).
		Where("user_id = ? AND media_type = ?", userID, "episode")

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return response.InternalError(c)
	}

	var rows []database.UserCollection
	if err := query.Order("collected_at DESC").Order("id DESC").
		Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	results := make([]collectedEpisodeResponse, 0, len(rows))
	if len(rows) > 0 {
		episodeIDs := make([]int64, 0, len(rows))
		for _, row := range rows {
			episodeIDs = append(episodeIDs, row.MediaID)
		}

		var episodes []database.Episode
		if err := db.Where("id IN ?", episodeIDs).Find(&episodes).Error; err != nil {
			return response.InternalError(c)
		}
		episodeByID := make(map[int64]database.Episode, len(episodes))
		seasonIDs := make([]int64, 0, len(episodes))
		showIDs := make([]int64, 0, len(episodes))
		for _, ep := range episodes {
			episodeByID[ep.ID] = ep
			seasonIDs = append(seasonIDs, ep.SeasonID)
			showIDs = append(showIDs, ep.ShowID)
		}

		var seasons []database.Season
		if err := db.Where("id IN ?", seasonIDs).Find(&seasons).Error; err != nil {
			return response.InternalError(c)
		}
		seasonByID := make(map[int64]database.Season, len(seasons))
		for _, season := range seasons {
			seasonByID[season.ID] = season
		}

		var shows []database.Show
		if err := db.Where("id IN ?", showIDs).Find(&shows).Error; err != nil {
			return response.InternalError(c)
		}
		showByID := make(map[int64]database.Show, len(shows))
		for _, show := range shows {
			showByID[show.ID] = show
		}

		externalIDs, err := collectionExternalIDs(db, episodeIDs, showIDs)
		if err != nil {
			return response.InternalError(c)
		}

		for _, row := range rows {
			ep, ok := episodeByID[row.MediaID]
			if !ok {
				continue
			}
			season := seasonByID[ep.SeasonID]
			show := showByID[ep.ShowID]
			results = append(results, collectedEpisodeResponse{
				Type:        "episode",
				CollectedAt: response.FormatTime(row.CollectedAt),
				UpdatedAt:   response.FormatTime(row.CollectedAt),
				Episode: collectedEpisodeObject{
					Season: season.SeasonNumber,
					Number: ep.EpisodeNumber,
					Title:  ep.Title,
					IDs:    collectionEpisodeIDs(externalIDs, ep.ID),
				},
				Show: collectedShowObject{
					Title: show.Title,
					Year:  collectionShowYear(show),
					IDs:   collectionShowIDs(externalIDs, show),
				},
			})
		}
	}

	response.SetPaginationHeaders(c, page, limit, total, len(results))
	return c.JSON(results)
}

func collectionExternalIDs(db *gorm.DB, episodeIDs, showIDs []int64) (map[mediaRef][]database.ExternalID, error) {
	result := make(map[mediaRef][]database.ExternalID)
	if len(episodeIDs) > 0 {
		var rows []database.ExternalID
		if err := db.Where("media_type = ? AND media_id IN ?", "episode", episodeIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			ref := mediaRef{mediaType: row.MediaType, mediaID: row.MediaID}
			result[ref] = append(result[ref], row)
		}
	}
	if len(showIDs) > 0 {
		var rows []database.ExternalID
		if err := db.Where("media_type = ? AND media_id IN ?", "show", showIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			ref := mediaRef{mediaType: row.MediaType, mediaID: row.MediaID}
			result[ref] = append(result[ref], row)
		}
	}
	return result, nil
}

func collectionEpisodeIDs(lookup map[mediaRef][]database.ExternalID, episodeID int64) traktIDsObj {
	ids := traktIDsObj{}
	for _, ext := range lookup[mediaRef{mediaType: "episode", mediaID: episodeID}] {
		switch ext.Source {
		case "tmdb":
			ids.TMDB = atoiOrZero(ext.ExternalID)
		case "tvdb":
			ids.TVDB = atoiOrZero(ext.ExternalID)
		case "imdb":
			ids.IMDB = ext.ExternalID
		}
	}
	return ids
}

func collectionShowIDs(lookup map[mediaRef][]database.ExternalID, show database.Show) traktIDsObj {
	ids := traktIDsObj{
		Slug: show.Slug,
		TVDB: show.TvdbID,
		TMDB: show.TmdbID,
		IMDB: show.ImdbID,
	}
	for _, ext := range lookup[mediaRef{mediaType: "show", mediaID: show.ID}] {
		switch ext.Source {
		case "tmdb":
			ids.TMDB = atoiOrZero(ext.ExternalID)
		case "tvdb":
			ids.TVDB = atoiOrZero(ext.ExternalID)
		case "imdb":
			ids.IMDB = ext.ExternalID
		case "slug":
			ids.Slug = ext.ExternalID
		}
	}
	return ids
}

func collectionShowYear(show database.Show) int {
	if show.FirstAired != nil && !show.FirstAired.IsZero() {
		return show.FirstAired.Year()
	}
	return 0
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
