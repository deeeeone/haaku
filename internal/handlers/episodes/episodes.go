package episodes

import (
	"errors"
	"strconv"

	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
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
	Season *media.SeasonResponse  `json:"season"`
	Stats  *EpisodeStatsResponse  `json:"stats"`
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
	id, err := parseID(c)
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var episode database.Episode
	if err := db.First(&episode, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "episode not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	resp := EpisodeDetailResponse{
		EpisodeResponse: buildFullEpisodeResponse(&episode),
	}

	// Fetch season.
	var season database.Season
	if err := db.First(&season, episode.SeasonID).Error; err == nil {
		sr := buildFullSeasonResponse(&season)
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
	id, err := parseID(c)
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify episode exists.
	var episode database.Episode
	if err := db.First(&episode, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "episode not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	stats := queryEpisodeStats(db, id)
	return c.JSON(stats)
}

// GET /v1/episodes/{id}/ratings
func handleGetEpisodeRatings(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify episode exists.
	var episode database.Episode
	if err := db.First(&episode, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "episode not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	return c.JSON(queryRatingDistribution(db, "episode", id))
}

// GET /v1/episodes/{id}/comments
func handleGetEpisodeComments(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify episode exists.
	var episode database.Episode
	if err := db.First(&episode, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "episode not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	return queryMediaComments(c, db, "episode", id)
}

// GET /v1/episodes/{id}/lists
func handleGetEpisodeLists(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify episode exists.
	var episode database.Episode
	if err := db.First(&episode, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "episode not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	page, limit := parsePaginationHelper(c)

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

func parseID(c *fiber.Ctx) (int64, error) {
	idStr := c.Params("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, badRequest(c, "invalid episode id")
	}
	return id, nil
}

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
	page, limit := parsePaginationHelper(c)

	var totalCount int64
	db.Model(&database.Comment{}).Where("media_type = ? AND media_id = ?", mediaType, mediaID).Count(&totalCount)

	var comments []database.Comment
	db.Where("media_type = ? AND media_id = ?", mediaType, mediaID).
		Order("created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&comments)

	type commentResponse struct {
		ID          int64   `json:"id"`
		UserID      string  `json:"user_id"`
		ParentID    *int64  `json:"parent_id,omitempty"`
		MediaType   string  `json:"media_type,omitempty"`
		MediaID     *int64  `json:"media_id,omitempty"`
		CommentType string  `json:"comment_type"`
		Body        string  `json:"body"`
		Spoiler     bool    `json:"spoiler"`
		LikeCount   int     `json:"like_count"`
		ReplyCount  int     `json:"reply_count"`
		CreatedAt   string  `json:"created_at"`
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
			CreatedAt:   c.CreatedAt.Format("2006-01-02T15:04:05Z"),
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

func parsePaginationHelper(c *fiber.Ctx) (page, limit int) {
	page, limit = 1, 25
	if p := c.Query("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			if parsed > 100 {
				parsed = 100
			}
			limit = parsed
		}
	}
	return
}

func buildFullEpisodeResponse(e *database.Episode) media.EpisodeResponse {
	resp := media.EpisodeResponse{
		IDs:            buildEpisodeIDs(e),
		SeasonID:       e.SeasonID,
		ShowID:         e.ShowID,
		EpisodeNumber:  e.EpisodeNumber,
		Title:          e.Title,
		Overview:       e.Overview,
		Runtime:        e.Runtime,
		Images:         e.Images,
		AbsoluteNumber: e.AbsoluteNumber,
	}
	if e.AiredAt != nil {
		resp.AiredAt = e.AiredAt.String()
	}
	if resp.Images == nil {
		resp.Images = make(map[string]any)
	}
	return resp
}

func buildFullSeasonResponse(s *database.Season) media.SeasonResponse {
	resp := media.SeasonResponse{
		IDs:          buildSeasonIDs(s),
		ShowID:       s.ShowID,
		SeasonNumber: s.SeasonNumber,
		EpisodeCount: s.EpisodeCount,
		Images:       s.Images,
	}
	if s.AiredAt != nil {
		resp.AiredAt = s.AiredAt.String()
	}
	if resp.Images == nil || len(resp.Images) == 0 {
		resp.Images = make(map[string]any)
	}
	return resp
}

func buildEpisodeIDs(e *database.Episode) media.IDs {
	return media.IDs{ID: e.ID}
}

func buildSeasonIDs(s *database.Season) media.IDs {
	return media.IDs{ID: s.ID}
}

// ---------------------------------------------------------------------------
// Error helpers
// ---------------------------------------------------------------------------

func writeError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{
			"code":    code,
			"message": message,
		},
	})
}

func notFound(c *fiber.Ctx, msg string) error {
	return writeError(c, fiber.StatusNotFound, "not_found", msg)
}

func badRequest(c *fiber.Ctx, msg string) error {
	return writeError(c, fiber.StatusBadRequest, "invalid_request", msg)
}
