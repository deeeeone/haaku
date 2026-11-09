package media

// Contract-shaped "watching right now" reads for a season and an episode.
//
// The source is the same live state GET /v1/users/{id}/watching reads: the
// newest non-expired scrobble session, where a pause/stop session means the user
// is not watching. A season's viewers are anyone whose live session names that
// season or one of its episodes; an episode's viewers are anyone whose session
// names that episode.
//
// The candidate accounts are filtered through auth.VisibleUserIDs with the same
// "activity" section the per-user route enforces, so a private profile, a
// restricting section flag, or a block in either direction hides the account
// from the list instead of leaking it.

import (
	"time"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func init() {
	routes := []openapi.RouteDef{
		{
			Method:      "GET",
			Path:        "/v1/shows/{id}/seasons/{season}/watching",
			Summary:     "Get users watching this season right now",
			Tags:        []string{"Shows"},
			Auth:        "optional",
			StatusCodes: map[int]string{200: "Users watching"},
		},
		{
			Method:      "GET",
			Path:        "/v1/shows/{id}/seasons/{season}/episodes/{episode}/watching",
			Summary:     "Get users watching this episode right now",
			Tags:        []string{"Shows"},
			Auth:        "optional",
			StatusCodes: map[int]string{200: "Users watching"},
		},
	}
	for _, route := range routes {
		openapi.Register(route)
	}
}

// registerContractWatchingRoutes mounts the season/episode watching reads.
func registerContractWatchingRoutes(api fiber.Router) {
	api.Get("/shows/:id/seasons/:season/watching", auth.OptionalAuth(), handleSeasonWatching)
	api.Get("/shows/:id/seasons/:season/episodes/:episode/watching", auth.OptionalAuth(), handleEpisodeWatching)
}

// serveMovieWatching answers GET /v1/movies/{id}/watching: the users whose
// newest live scrobble session names the movie. The session source and privacy
// gate are the same ones the season/episode and per-user routes use.
func serveMovieWatching(c *fiber.Ctx) error {
	_, id, err := ResolveIDAs(c, c.Params("id"), "movie")
	if err != nil {
		return err
	}
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c, "database unavailable")
	}
	return serveWatchingUsers(c, db, liveSessionsQuery(db).
		Where("media_type = ? AND media_id = ?", "movie", id))
}

// serveShowWatching answers GET /v1/shows/{id}/watching. A session names the
// show directly, one of its seasons, or one of its episodes; all three forms
// are the same live activity.
func serveShowWatching(c *fiber.Ctx) error {
	_, showID, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c, "database unavailable")
	}

	var seasonIDs, episodeIDs []int64
	if err := db.Model(&database.Season{}).Where("show_id = ?", showID).Pluck("id", &seasonIDs).Error; err != nil {
		return response.InternalError(c, "database query error")
	}
	if err := db.Model(&database.Episode{}).Where("show_id = ?", showID).Pluck("id", &episodeIDs).Error; err != nil {
		return response.InternalError(c, "database query error")
	}

	where := "(media_type = ? AND media_id = ?)"
	args := []any{"show", showID}
	if len(seasonIDs) > 0 {
		where += " OR season_id IN ?"
		args = append(args, seasonIDs)
	}
	if len(episodeIDs) > 0 {
		where += " OR episode_id IN ? OR (media_type = ? AND media_id IN ?)"
		args = append(args, episodeIDs, "episode", episodeIDs)
	}

	return serveWatchingUsers(c, db, liveSessionsQuery(db).Where(where, args...))
}

// handleSeasonWatching serves GET /v1/shows/{id}/seasons/{season}/watching.
func handleSeasonWatching(c *fiber.Ctx) error {
	_, season, db, err := loadSeasonRow(c)
	if err != nil {
		return err
	}

	var episodeIDs []int64
	if err := db.Model(&database.Episode{}).Where("season_id = ?", season.ID).
		Pluck("id", &episodeIDs).Error; err != nil {
		return response.InternalError(c, "database query error")
	}

	where := "season_id = ?"
	args := []any{season.ID}
	if len(episodeIDs) > 0 {
		where += " OR episode_id IN ?"
		args = append(args, episodeIDs)
	}
	return serveWatchingUsers(c, db, liveSessionsQuery(db).Where(where, args...))
}

// handleEpisodeWatching serves
// GET /v1/shows/{id}/seasons/{season}/episodes/{episode}/watching.
func handleEpisodeWatching(c *fiber.Ctx) error {
	_, _, episode, db, err := loadEpisodeRow(c)
	if err != nil {
		return err
	}
	return serveWatchingUsers(c, db, liveSessionsQuery(db).
		Where("episode_id = ? OR (media_type = ? AND media_id = ?)", episode.ID, "episode", episode.ID))
}

// liveSessionsQuery is the one "currently watching" predicate: not expired, and
// the newest action is not a pause or a stop.
func liveSessionsQuery(db *gorm.DB) *gorm.DB {
	return db.Model(&database.ScrobbleSession{}).
		Where("expires_at > ?", time.Now()).
		Where("action NOT IN ?", []string{"pause", "stop"})
}

// serveWatchingUsers renders the distinct accounts behind a live-session query
// as the contract's profile array, newest session first, after applying the
// same visibility rule as /v1/users/{id}/watching.
func serveWatchingUsers(c *fiber.Ctx, db *gorm.DB, query *gorm.DB) error {
	var sessions []database.ScrobbleSession
	if err := query.Select("user_id").Order("created_at DESC, id DESC").Find(&sessions).Error; err != nil {
		return response.InternalError(c, "database query error")
	}

	candidates := make([]string, 0, len(sessions))
	for _, session := range sessions {
		candidates = append(candidates, session.UserID)
	}

	viewer, _ := c.Locals("user_id").(string)
	visible, err := auth.VisibleUserIDs(db, viewer, candidates, "activity")
	if err != nil {
		return response.InternalError(c, "database query error")
	}

	users, err := contractshape.UserByIDBatch(db, visible)
	if err != nil {
		return response.InternalError(c, "database query error")
	}

	out := make([]map[string]any, 0, len(visible))
	for _, id := range visible {
		if user := users[id]; user != nil {
			out = append(out, contractshape.Profile(user))
		}
	}
	return c.JSON(out)
}
