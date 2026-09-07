package progress

import (
	"strconv"

	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"trakt-api/internal/auth"
	"trakt-api/internal/response"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{
			Method:  "GET",
			Path:    "/v1/sync/playback/movies",
			Summary: "Get paused/resumed movie playback positions for current user",
			Tags:    []string{"Sync"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Playback positions with movie info",
				401: "Unauthorized",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/sync/playback/episodes",
			Summary: "Get paused/resumed episode playback positions for current user",
			Tags:    []string{"Sync"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Playback positions with episode+show info",
				401: "Unauthorized",
			},
		},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires progress route handlers to the Fiber router.
func RegisterRoutes(api fiber.Router) {
	sync := api.Group("/sync/playback")
	sync.Get("/movies", auth.AuthRequired(), handlePlaybackMovies)
	sync.Get("/episodes", auth.AuthRequired(), handlePlaybackEpisodes)
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// MoviePlaybackResponse is the enriched movie playback response.
type MoviePlaybackResponse struct {
	Progress float64             `json:"progress"`
	PausedAt string              `json:"paused_at"`
	Movie    media.MovieResponse `json:"movie"`
}

// EpisodePlaybackResponse is the enriched episode playback response.
type EpisodePlaybackResponse struct {
	Progress float64             `json:"progress"`
	PausedAt string              `json:"paused_at"`
	Episode  EpisodePlaybackInfo `json:"episode"`
	Show     ShowPlaybackInfo    `json:"show"`
}

// EpisodePlaybackInfo holds minimal episode info for playback responses.
type EpisodePlaybackInfo struct {
	Title  string `json:"title"`
	Season int    `json:"season"`
	Number int    `json:"number"`
}

// ShowPlaybackInfo holds minimal show info for playback responses.
type ShowPlaybackInfo struct {
	Title string    `json:"title"`
	IDs   media.IDs `json:"ids"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handlePlaybackMovies returns paused/resumed movie positions with movie info.
func handlePlaybackMovies(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 25)

	var total int64
	if err := db.Model(&database.UserPlayback{}).
		Where("user_id = ? AND media_type = ?", userID, "movie").
		Count(&total).Error; err != nil {
		return response.InternalError(c)
	}

	var rows []database.UserPlayback
	if err := db.Where("user_id = ? AND media_type = ?", userID, "movie").
		Order("paused_at DESC, id DESC"). // stable order for pagination
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	// Batch-fetch the movies so we can reuse media.BuildMovieResponse for
	// response enrichment without a per-item N+1.
	movieIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		movieIDs = append(movieIDs, row.MediaID)
	}
	movies := make(map[int64]database.Movie, len(movieIDs))
	if len(movieIDs) > 0 {
		var fetched []database.Movie
		if err := db.Where("id IN ?", movieIDs).Find(&fetched).Error; err != nil {
			return response.InternalError(c)
		}
		for _, m := range fetched {
			movies[m.ID] = m
		}
	}

	extended := media.GetExtended(c)
	resp := make([]MoviePlaybackResponse, 0, len(rows))
	for _, row := range rows {
		movieResp := media.MovieResponse{}
		if m, ok := movies[row.MediaID]; ok {
			movieResp = media.BuildMovieResponse(&m, extended)
		}
		resp = append(resp, MoviePlaybackResponse{
			Progress: row.Progress,
			PausedAt: response.FormatTime(row.PausedAt),
			Movie:    movieResp,
		})
	}

	setPaginationHeaders(c, page, limit, total, len(resp))

	return c.JSON(resp)
}

// handlePlaybackEpisodes returns paused/resumed episode positions with show+episode info.
func handlePlaybackEpisodes(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 25)

	var total int64
	if err := db.Model(&database.UserPlayback{}).
		Where("user_id = ? AND media_type = ?", userID, "episode").
		Count(&total).Error; err != nil {
		return response.InternalError(c)
	}

	// Query user_playback joined with episodes, seasons, and shows.
	type playbackEpisodeRow struct {
		database.UserPlayback
		EpisodeTitle  string `json:"episode_title"`
		EpisodeNumber int    `json:"episode_number"`
		SeasonNumber  int    `json:"season_number"`
		ShowTitle     string `json:"show_title"`
		ShowID        int64  `json:"show_id"`
		ShowTmdbID    int    `json:"show_tmdb_id"`
		ShowImdbID    string `json:"show_imdb_id"`
	}

	var rows []playbackEpisodeRow
	if err := db.Table("user_playback up").
		Select("up.*, e.title AS episode_title, e.episode_number, s.season_number, sh.id AS show_id, sh.title AS show_title, sh.tmdb_id AS show_tmdb_id, sh.imdb_id AS show_imdb_id").
		Joins("JOIN episodes e ON e.id = up.media_id AND up.media_type = 'episode'").
		Joins("JOIN seasons s ON s.id = e.season_id").
		Joins("JOIN shows sh ON sh.id = e.show_id").
		Where("up.user_id = ?", userID).
		Order("up.paused_at DESC, up.id DESC"). // stable order for pagination
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	resp := make([]EpisodePlaybackResponse, 0, len(rows))
	for _, row := range rows {
		showIDs := media.IDs{ID: row.ShowID}
		if row.ShowTmdbID != 0 {
			showIDs.TMDB = row.ShowTmdbID
		}
		if row.ShowImdbID != "" {
			showIDs.IMDB = row.ShowImdbID
		}

		resp = append(resp, EpisodePlaybackResponse{
			Progress: row.Progress,
			PausedAt: response.FormatTime(row.PausedAt),
			Episode: EpisodePlaybackInfo{
				Title:  row.EpisodeTitle,
				Season: row.SeasonNumber,
				Number: row.EpisodeNumber,
			},
			Show: ShowPlaybackInfo{
				Title: row.ShowTitle,
				IDs:   showIDs,
			},
		})
	}

	setPaginationHeaders(c, page, limit, total, len(resp))

	return c.JSON(resp)
}

// setPaginationHeaders emits the §7 pagination headers (api-conventions.md)
// plus a Link: rel="next" header when more pages exist.
func setPaginationHeaders(c *fiber.Ctx, page, limit int, total int64, itemsCount int) {
	totalPages := int64(0)
	if total > 0 {
		totalPages = (total + int64(limit) - 1) / int64(limit)
	}
	c.Set("X-Pagination-Page", strconv.Itoa(page))
	c.Set("X-Pagination-Limit", strconv.Itoa(limit))
	c.Set("X-Pagination-Total-Pages", strconv.FormatInt(totalPages, 10))
	c.Set("X-Pagination-Total-Items", strconv.FormatInt(total, 10))
	c.Set("X-Pagination-Items-Count", strconv.Itoa(itemsCount))
	if page < int(totalPages) {
		c.Set("Link", `<`+c.Path()+`?page=`+strconv.Itoa(page+1)+`&limit=`+strconv.Itoa(limit)+`>; rel="next"`)
	}
}
