package episodes

import (
	"errors"
	"strconv"

	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"trakt-api/internal/response"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/episodes/{id}", Summary: "Get an episode with show and season embedded", Tags: []string{"Episodes"}, Auth: "none", StatusCodes: map[int]string{200: "Episode detail", 404: "Not found"}},
		{Method: "GET", Path: "/v1/episodes/{id}/stats", Summary: "Episode stats (watchers, plays, ratings)", Tags: []string{"Episodes"}, Auth: "none", StatusCodes: map[int]string{200: "Stats", 404: "Not found"}},
		{Method: "GET", Path: "/v1/episodes/{id}/ratings", Summary: "Episode rating distribution", Tags: []string{"Episodes"}, Auth: "none", StatusCodes: map[int]string{200: "Ratings", 404: "Not found"}},
		{Method: "GET", Path: "/v1/episodes/{id}/comments", Summary: "Comments on this episode", Tags: []string{"Episodes"}, Auth: "none", StatusCodes: map[int]string{200: "Comments", 404: "Not found"}},
		{Method: "GET", Path: "/v1/episodes/{id}/lists", Summary: "Lists containing this episode", Tags: []string{"Episodes"}, Auth: "none", StatusCodes: map[int]string{200: "Lists", 404: "Not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires all episode route handlers.
func RegisterRoutes(api fiber.Router) {
	ep := api.Group("/episodes")

	ep.Get("/:id", handleGetEpisode)
	ep.Get("/:id/stats", handleGetEpisodeStats)
	ep.Get("/:id/ratings", handleGetEpisodeRatings)
	ep.Get("/:id/comments", handleGetEpisodeComments)
	ep.Get("/:id/lists", handleGetEpisodeLists)
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// EpisodeDetailResponse is the full episode detail with embedded show and season.
type EpisodeDetailResponse struct {
	media.EpisodeResponse `json:",inline"`

	Show   *media.ShowResponse   `json:"show,omitempty"`
	Season *media.SeasonResponse `json:"season"`
	Stats  *EpisodeStatsResponse `json:"stats"`
}

// EpisodeStatsResponse holds public stats for an episode.
type EpisodeStatsResponse struct {
	Watchers int64   `json:"watchers"`
	Plays    int64   `json:"plays"`
	Rating   float64 `json:"rating"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// GET /v1/episodes/{id}
func handleGetEpisode(c *fiber.Ctx) error {
	id, err := response.ParseIDParam(c, "episode")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var episode database.Episode
	if err := db.First(&episode, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "episode not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	resp := EpisodeDetailResponse{
		EpisodeResponse: media.BuildEpisodeResponse(&episode),
	}

	// Fetch season.
	var season database.Season
	if err := db.First(&season, episode.SeasonID).Error; err == nil {
		sr := media.BuildSeasonResponse(&season)
		resp.Season = &sr
	}

	// Fetch show.
	var show database.Show
	if err := db.First(&show, episode.ShowID).Error; err == nil {
		extended := media.GetExtended(c)
		sr := media.BuildShowResponse(&show, extended)
		resp.Show = &sr
	}

	// Stats.
	resp.Stats = queryEpisodeStats(db, id)

	return c.JSON(resp)
}

// GET /v1/episodes/{id}/stats
func handleGetEpisodeStats(c *fiber.Ctx) error {
	id, err := response.ParseIDParam(c, "episode")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify episode exists.
	var episode database.Episode
	if err := db.First(&episode, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "episode not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	stats := queryEpisodeStats(db, id)
	return c.JSON(stats)
}

// GET /v1/episodes/{id}/ratings
func handleGetEpisodeRatings(c *fiber.Ctx) error {
	id, err := response.ParseIDParam(c, "episode")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify episode exists.
	var episode database.Episode
	if err := db.First(&episode, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "episode not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	return c.JSON(queryRatingDistribution(db, "episode", id))
}

// GET /v1/episodes/{id}/comments
func handleGetEpisodeComments(c *fiber.Ctx) error {
	id, err := response.ParseIDParam(c, "episode")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify episode exists.
	var episode database.Episode
	if err := db.First(&episode, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "episode not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	return queryMediaComments(c, db, "episode", id)
}

// GET /v1/episodes/{id}/lists
func handleGetEpisodeLists(c *fiber.Ctx) error {
	id, err := response.ParseIDParam(c, "episode")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify episode exists.
	var episode database.Episode
	if err := db.First(&episode, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "episode not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	page, limit := response.ParsePagination(c, 25)

	var totalCount int64
	db.Model(&database.ListItem{}).Where("media_type = ? AND media_id = ?", "episode", id).Count(&totalCount)

	var items []database.ListItem
	db.Where("media_type = ? AND media_id = ?", "episode", id).
		Order("added_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&items)

	// Fetch list details for each item.
	type listItemResult struct {
		ListItem database.ListItem `json:"list_item"`
		List     database.List     `json:"list"`
	}
	results := make([]listItemResult, 0, len(items))
	for _, item := range items {
		var list database.List
		if err := db.First(&list, item.ListID).Error; err == nil {
			if list.Privacy == "public" || list.Privacy == "link_only" {
				results = append(results, listItemResult{ListItem: item, List: list})
			}
		}
	}

	return c.JSON(fiber.Map{
		"data":        results,
		"page":        page,
		"limit":       limit,
		"total_count": totalCount,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func queryEpisodeStats(db *gorm.DB, episodeID int64) *EpisodeStatsResponse {
	var watchers int64
	db.Model(&database.UserHistory{}).Where("media_type = ? AND media_id = ?", "episode", episodeID).
		Distinct("user_id").Count(&watchers)

	var plays int64
	db.Model(&database.UserHistory{}).Where("media_type = ? AND media_id = ?", "episode", episodeID).Count(&plays)

	var avgRating float64
	db.Model(&database.UserRating{}).Where("media_type = ? AND media_id = ?", "episode", episodeID).
		Select("COALESCE(AVG(rating), 0)").Scan(&avgRating)

	return &EpisodeStatsResponse{
		Watchers: watchers,
		Plays:    plays,
		Rating:   avgRating,
	}
}

func queryRatingDistribution(db *gorm.DB, mediaType string, mediaID int64) *media.RatingSummary {
	var avgRating float64
	var count int64
	db.Model(&database.UserRating{}).Where("media_type = ? AND media_id = ?", mediaType, mediaID).
		Select("COALESCE(AVG(rating), 0)").Scan(&avgRating)
	db.Model(&database.UserRating{}).Where("media_type = ? AND media_id = ?", mediaType, mediaID).Count(&count)

	distribution := make(map[string]int64)
	var rows []struct {
		Rating int
		Count  int64
	}
	db.Model(&database.UserRating{}).
		Select("rating, count(*) as count").
		Where("media_type = ? AND media_id = ?", mediaType, mediaID).
		Group("rating").
		Scan(&rows)
	for _, r := range rows {
		distribution[strconv.Itoa(r.Rating)] = r.Count
	}

	return &media.RatingSummary{
		Rating:       avgRating,
		Votes:        count,
		Distribution: distribution,
	}
}

func queryMediaComments(c *fiber.Ctx, db *gorm.DB, mediaType string, mediaID int64) error {
	page, limit := response.ParsePagination(c, 25)

	var totalCount int64
	db.Model(&database.Comment{}).Where("media_type = ? AND media_id = ?", mediaType, mediaID).Count(&totalCount)

	var comments []database.Comment
	db.Where("media_type = ? AND media_id = ?", mediaType, mediaID).
		Order("created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&comments)

	type commentResponse struct {
		ID          int64  `json:"id"`
		UserID      string `json:"user_id"`
		ParentID    *int64 `json:"parent_id,omitempty"`
		MediaType   string `json:"media_type,omitempty"`
		MediaID     *int64 `json:"media_id,omitempty"`
		CommentType string `json:"comment_type"`
		Body        string `json:"body"`
		Spoiler     bool   `json:"spoiler"`
		LikeCount   int    `json:"like_count"`
		ReplyCount  int    `json:"reply_count"`
		CreatedAt   string `json:"created_at"`
	}

	out := make([]commentResponse, 0, len(comments))
	for _, c := range comments {
		mid := c.MediaID
		cr := commentResponse{
			ID:          c.ID,
			UserID:      c.UserID,
			ParentID:    c.ParentID,
			MediaType:   c.MediaType,
			MediaID:     mid,
			CommentType: c.CommentType,
			Body:        c.Body,
			Spoiler:     c.Spoiler,
			LikeCount:   c.LikeCount,
			ReplyCount:  c.ReplyCount,
			CreatedAt:   response.FormatTime(c.CreatedAt),
		}
		out = append(out, cr)
	}
	if out == nil {
		out = make([]commentResponse, 0)
	}

	return c.JSON(fiber.Map{
		"data":        out,
		"page":        page,
		"limit":       limit,
		"total_count": totalCount,
	})
}

// ---------------------------------------------------------------------------
// Error helpers
// ---------------------------------------------------------------------------
