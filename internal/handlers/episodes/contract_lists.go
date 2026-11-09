package episodes

import (
	"errors"
	"strconv"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped nested episode lists.
//
// Trakt: /shows/{id}/seasons/{season}/episodes/{episode}/lists/{type}/{sort}.
// We serve lists containing an episode flat at /v1/episodes/{id}/lists. This
// route resolves show+season+episode to our episode id, filters by list kind
// and sort, and returns the contract's list-object array.

func init() {
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/shows/{id}/seasons/{season}/episodes/{episode}/lists/{type}/{sort}",
		Summary: "Lists containing an episode, filtered by kind and sort (contract path)",
		Tags:    []string{"Episodes"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "Lists",
			400: "Invalid type, sort, season or episode",
			404: "Show, season or episode not found",
		},
	})
}

// registerContractEpisodeListRoutes mounts the contract-shaped nested route.
func registerContractEpisodeListRoutes(api fiber.Router) {
	api.Get("/shows/:id/seasons/:season/episodes/:episode/lists/:type/:sort", handleContractEpisodeLists)
}

// episodeListTypeMedia maps the list-kind segment onto list_items.media_type.
var episodeListTypeMedia = map[string]string{"movies": "movie", "shows": "show", "people": "person"}

// episodeListOrder maps the contract's sort vocabulary onto list ordering.
var episodeListOrder = map[string]string{
	"added":    "list_items.added_at DESC",
	"popular":  "lists.like_count DESC, lists.created_at DESC",
	"likes":    "lists.like_count DESC, lists.created_at DESC",
	"comments": "lists.comment_count DESC, lists.created_at DESC",
}

func handleContractEpisodeLists(c *fiber.Ctx) error {
	listMedia, ok := episodeListTypeMedia[c.Params("type")]
	if !ok {
		return response.BadRequest(c, "invalid type: must be one of movies, shows, people")
	}
	order, ok := episodeListOrder[c.Params("sort")]
	if !ok {
		return response.BadRequest(c, "invalid sort: must be one of added, popular, likes, comments")
	}

	_, showID, err := media.ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}
	seasonNum, aerr := strconv.Atoi(c.Params("season"))
	if aerr != nil {
		return response.BadRequest(c, "invalid season number")
	}
	episodeNum, aerr := strconv.Atoi(c.Params("episode"))
	if aerr != nil {
		return response.BadRequest(c, "invalid episode number")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", showID, seasonNum).First(&season).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "season not found")
		}
		return response.InternalError(c)
	}
	var episode database.Episode
	if err := db.Where("season_id = ? AND episode_number = ?", season.ID, episodeNum).First(&episode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "episode not found")
		}
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 25)

	build := func() *gorm.DB {
		return db.Model(&database.List{}).
			Joins("JOIN list_items ON list_items.list_id = lists.id").
			Where("list_items.media_type = ? AND list_items.media_id = ?", "episode", episode.ID).
			Where("lists.privacy IN ?", []string{"public", "link_only"}).
			Where("EXISTS (SELECT 1 FROM list_items li2 WHERE li2.list_id = lists.id AND li2.media_type = ?)", listMedia)
	}

	var total int64
	if err := build().Distinct("lists.id").Count(&total).Error; err != nil {
		return response.InternalError(c)
	}

	var lists []database.List
	if err := build().Select("lists.*").Group("lists.id").Order(order).
		Offset((page - 1) * limit).Limit(limit).Find(&lists).Error; err != nil {
		return response.InternalError(c)
	}

	users, err := contractshape.UserByIDBatch(db, listOwnerIDs(lists))
	if err != nil {
		return response.InternalError(c)
	}
	out := make([]map[string]any, 0, len(lists))
	for i := range lists {
		out = append(out, contractshape.List(&lists[i], users[lists[i].UserID]))
	}

	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}

func listOwnerIDs(lists []database.List) []string {
	ids := make([]string, 0, len(lists))
	for i := range lists {
		ids = append(ids, lists[i].UserID)
	}
	return ids
}
