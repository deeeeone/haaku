package progress

import (
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

	// Query user_playback joined with movies.
	type playbackMovieRow struct {
		database.UserPlayback
		Title    string `json:"title"`
		TmdbID   int    `json:"tmdb_id"`
		ImdbID   string `json:"imdb_id"`
		Year     int    `json:"year"`
		Overview string `json:"overview"`
		Runtime  int    `json:"runtime"`
	}

	var rows []playbackMovieRow
	if err := db.Table("user_playback up").
		Select("up.*, m.title, m.tmdb_id, m.imdb_id, m.overview, m.runtime").
		Joins("JOIN movies m ON m.id = up.media_id AND up.media_type = 'movie'").
		Where("up.user_id = ?", userID).
		Order("up.paused_at DESC").
		Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	resp := make([]MoviePlaybackResponse, 0, len(rows))
	for _, row := range rows {
		movieResp := media.MovieResponse{
			IDs:      media.IDs{ID: row.MediaID, TMDB: row.TmdbID},
			Title:    row.Title,
			Overview: row.Overview,
			Runtime:  row.Runtime,
		}
		if row.ImdbID != "" {
			movieResp.IDs.IMDB = row.ImdbID
		}
		if movieResp.Images == nil {
			movieResp.Images = make(map[string]any)
		}

		resp = append(resp, MoviePlaybackResponse{
			Progress: row.Progress,
			PausedAt: response.FormatTime(row.PausedAt),
			Movie:    movieResp,
		})
	}

	return c.JSON(resp)
}

// handlePlaybackEpisodes returns paused/resumed episode positions with show+episode info.
func handlePlaybackEpisodes(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
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
		Order("up.paused_at DESC").
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

	return c.JSON(resp)
}
