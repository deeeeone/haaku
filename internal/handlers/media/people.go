package media

// Season and episode people (issue #68).
//
// The contract's peopleResponseSchema is an object: a cast array whose entries
// carry character/characters and an embedded person, and a crew object keyed by
// department. The credit rows live in the existing credits table under
// media_type "season" / "episode"; the one-time TMDB fetch is marked in the row's
// metadata so an honestly-empty credit set is not re-fetched on every read.

import (
	"log/slog"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/metadata"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func init() {
	routes := []openapi.RouteDef{
		{
			Method:      "GET",
			Path:        "/v1/shows/{id}/seasons/{season}/people",
			Summary:     "Get all cast and crew for a season",
			Tags:        []string{"Shows"},
			Auth:        "none",
			StatusCodes: map[int]string{200: "People"},
		},
		{
			Method:      "GET",
			Path:        "/v1/shows/{id}/seasons/{season}/episodes/{episode}/people",
			Summary:     "Get all cast and crew for an episode",
			Tags:        []string{"Shows"},
			Auth:        "none",
			StatusCodes: map[int]string{200: "People"},
		},
	}
	for _, route := range routes {
		openapi.Register(route)
	}
}

// GET /v1/shows/{id}/seasons/{season}/people
func handleShowSeasonPeople(c *fiber.Ctx) error {
	show, season, db, err := loadSeasonRow(c)
	if err != nil {
		return err
	}
	return servePeople(c, db, "season", season.ID, season.Metadata,
		func(client *tmdb.Client) (database.JSONMap, error) {
			return metadata.HydrateSeasonExtras(db, client, show, season)
		})
}

// GET /v1/shows/{id}/seasons/{season}/episodes/{episode}/people
func handleShowEpisodePeople(c *fiber.Ctx) error {
	show, season, episode, db, err := loadEpisodeRow(c)
	if err != nil {
		return err
	}
	return servePeople(c, db, "episode", episode.ID, episode.Metadata,
		func(client *tmdb.Client) (database.JSONMap, error) {
			return metadata.HydrateEpisodeExtras(db, client, show, season, episode)
		})
}

// servePeople hydrates a season/episode's credits once when they were never
// fetched, then renders the stored credit rows. A nil client means TMDB is not
// configured: the computed result is whatever is already in the credits table,
// which is an empty people object when nothing was ever fetched.
func servePeople(c *fiber.Ctx, db *gorm.DB, mediaType string, mediaID int64, meta database.JSONMap, hydrate hydrateFunc) error {
	client := tmdbClientOrNil()
	if !metadata.HasCredits(meta) && client != nil {
		if _, err := hydrate(client); err != nil {
			return providerUnavailable(c, metadata.KeyCredits, err)
		}
	}
	return writeContractPeople(c, db, mediaType, mediaID)
}

// writeContractPeople loads the media item's credits and their person rows, then
// renders the contract's people object. All persons are fetched in one query;
// a credit whose person row is missing is skipped by contractshape.People.
func writeContractPeople(c *fiber.Ctx, db *gorm.DB, mediaType string, mediaID int64) error {
	var credits []database.Credit
	if err := db.Where("media_type = ? AND media_id = ?", mediaType, mediaID).
		Order("\"order\" ASC").Find(&credits).Error; err != nil {
		return response.InternalError(c, "database query error")
	}

	personIDs := make([]int64, 0, len(credits))
	seen := make(map[int64]bool, len(credits))
	for _, credit := range credits {
		if credit.PersonID != 0 && !seen[credit.PersonID] {
			seen[credit.PersonID] = true
			personIDs = append(personIDs, credit.PersonID)
		}
	}

	persons := make(map[int64]*database.Person, len(personIDs))
	if len(personIDs) > 0 {
		var rows []database.Person
		if err := db.Where("id IN ?", personIDs).Find(&rows).Error; err != nil {
			return response.InternalError(c, "database query error")
		}
		for i := range rows {
			persons[rows[i].ID] = &rows[i]
		}
	}

	return c.JSON(contractshape.People(credits, persons))
}

// providerUnavailable is the one 502 the metadata reads emit: the stored data
// was absent and the provider could not answer. It is deliberately not an empty
// 200 — a provider outage is not "this title has no videos".
func providerUnavailable(c *fiber.Ctx, key string, err error) error {
	slog.Debug("[media] metadata hydration failed", "key", key, "error", err)
	return response.WriteError(c, fiber.StatusBadGateway, "provider_unavailable",
		"the metadata provider could not be reached")
}
