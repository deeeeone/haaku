package media

import (
	"errors"
	"strconv"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/gorm"
)

func init() {
	registerOpenAPIRoutes()
}

// registerOpenAPIRoutes registers all media read endpoints in the OpenAPI registry.
func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		// Movies
		{Method: "GET", Path: "/v1/movies/{id}", Summary: "Get a movie", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "Movie"}},
		{Method: "GET", Path: "/v1/movies/{id}/people", Summary: "Get cast and crew for a movie", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "People"}},
		{Method: "GET", Path: "/v1/movies/{id}/ratings", Summary: "Get ratings for a movie", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "Rating summary"}},
		{Method: "GET", Path: "/v1/movies/{id}/stats", Summary: "Get stats for a movie", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "Stats"}},
		{Method: "GET", Path: "/v1/movies/{id}/lists", Summary: "Get lists containing a movie", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "Lists", 501: "Not implemented (requires lists engine)"}},
		{Method: "GET", Path: "/v1/movies/{id}/watching", Summary: "Users watching a movie right now", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "Watching", 501: "Not implemented (requires activity engine)"}},
		{Method: "GET", Path: "/v1/movies/{id}/comments/{sort}", Summary: "Get comments for a movie, sorted (newest, oldest, likes, replies, trending)", Tags: []string{"Movies"}, Auth: "optional", StatusCodes: map[int]string{200: "Comments", 400: "Invalid sort", 404: "Movie not found"}},

		// Shows
		{Method: "GET", Path: "/v1/shows/{id}", Summary: "Get a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Show"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons", Summary: "Get seasons for a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Seasons"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}", Summary: "Get a single season", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Season"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/episodes", Summary: "Get episodes for a season", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Episodes"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/episodes/{episode}", Summary: "Get a single episode", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Episode"}},
		{Method: "GET", Path: "/v1/shows/{id}/people", Summary: "Get cast and crew for a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "People"}},
		{Method: "GET", Path: "/v1/shows/{id}/ratings", Summary: "Get ratings for a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Rating summary"}},
		{Method: "GET", Path: "/v1/shows/{id}/stats", Summary: "Get stats for a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Stats"}},
		{Method: "GET", Path: "/v1/shows/{id}/lists", Summary: "Get lists containing a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Lists", 501: "Not implemented (requires lists engine)"}},
		{Method: "GET", Path: "/v1/shows/{id}/watching", Summary: "Users watching a show right now", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Watching", 501: "Not implemented (requires activity engine)"}},
		{Method: "GET", Path: "/v1/shows/{id}/next_episode", Summary: "Get the next scheduled episode for a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Next episode", 204: "No next episode", 404: "Show not found"}},
		{Method: "GET", Path: "/v1/shows/{id}/last_episode", Summary: "Get the most recently aired episode for a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Last episode", 204: "No aired episode", 404: "Show not found"}},
		{Method: "GET", Path: "/v1/shows/{id}/comments/{sort}", Summary: "Get comments for a show, sorted (newest, oldest, likes, replies, trending)", Tags: []string{"Shows"}, Auth: "optional", StatusCodes: map[int]string{200: "Comments", 400: "Invalid sort", 404: "Show not found"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/comments/{sort}", Summary: "Get comments for a season, sorted (newest, oldest, likes, replies, trending)", Tags: []string{"Shows"}, Auth: "optional", StatusCodes: map[int]string{200: "Comments", 400: "Invalid sort or season number", 404: "Show or season not found"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/episodes/{episode}/comments/{sort}", Summary: "Get comments for an episode, sorted (newest, oldest, likes, replies, trending)", Tags: []string{"Shows"}, Auth: "optional", StatusCodes: map[int]string{200: "Comments", 400: "Invalid sort, season or episode number", 404: "Show, season or episode not found"}},

		// People
		{Method: "GET", Path: "/v1/people/{id}", Summary: "Get a person", Tags: []string{"People"}, Auth: "none", StatusCodes: map[int]string{200: "Person"}},
		{Method: "GET", Path: "/v1/people/{id}/movies", Summary: "Get movie credits for a person", Tags: []string{"People"}, Auth: "none", StatusCodes: map[int]string{200: "Movie credits"}},
		{Method: "GET", Path: "/v1/people/{id}/shows", Summary: "Get show credits for a person", Tags: []string{"People"}, Auth: "none", StatusCodes: map[int]string{200: "Show credits"}},
	}

	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires all media route handlers to the Fiber router.
func RegisterRoutes(api fiber.Router) {
	// --- Movies ---
	movies := api.Group("/movies")

	movies.Get("/:id", handleGetMovie)
	movies.Get("/:id/people", handleGetMoviePeople)
	movies.Get("/:id/ratings", handleGetMovieRatingSummary)
	movies.Get("/:id/stats", handleGetMovieStats)
	movies.Get("/:id/lists", handleGetMovieLists)
	movies.Get("/:id/watching", handleGetMovieWatching)
	movies.Get("/:id/comments/:sort", handleGetMovieComments)

	// --- Shows ---
	shows := api.Group("/shows")

	shows.Get("/:id", handleGetShow)
	shows.Get("/:id/seasons", handleGetShowSeasons)
	shows.Get("/:id/seasons/:season", handleGetShowSeason)
	shows.Get("/:id/seasons/:season/episodes", handleGetShowSeasonEpisodes)
	shows.Get("/:id/seasons/:season/episodes/:episode", handleGetShowEpisode)
	shows.Get("/:id/people", handleGetShowPeople)
	shows.Get("/:id/ratings", handleGetShowRatingSummary)
	shows.Get("/:id/stats", handleGetShowStats)
	shows.Get("/:id/lists", handleGetShowLists)
	shows.Get("/:id/watching", handleGetShowWatching)
	shows.Get("/:id/next_episode", handleGetShowNextEpisode)
	shows.Get("/:id/last_episode", handleGetShowLastEpisode)
	shows.Get("/:id/comments/:sort", handleGetShowComments)
	shows.Get("/:id/seasons/:season/comments/:sort", handleGetShowSeasonComments)
	shows.Get("/:id/seasons/:season/episodes/:episode/comments/:sort", handleGetShowEpisodeComments)

	// --- People ---
	people := api.Group("/people")

	people.Get("/:id", handleGetPerson)
	people.Get("/:id/movies", handleGetPersonMovies)
	people.Get("/:id/shows", handleGetPersonShows)

	// --- Translation & alias routes ---
	RegisterTranslationRoutes(api)
}

// ---------------------------------------------------------------------------
// Movies
// ---------------------------------------------------------------------------

func handleGetMovie(c *fiber.Ctx) error {
	_, id, err := ResolveIDAs(c, c.Params("id"), "movie")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var movie database.Movie
	if err := db.First(&movie, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "movie not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	extended := getExtended(c)
	resp := BuildMovieResponse(&movie, extended)
	return c.JSON(resp)
}

func handleGetMoviePeople(c *fiber.Ctx) error {
	_, id, err := ResolveIDAs(c, c.Params("id"), "movie")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	// Fetch cast and crew credits.
	var credits []database.Credit
	if err := db.Where("media_type = ? AND media_id = ?", "movie", id).
		Order("\"order\" ASC").Find(&credits).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	cast, crew := buildPeopleResponse(c, db, credits)
	return c.JSON(PeopleResponse{Cast: cast, Crew: crew})
}

func handleGetMovieRatingSummary(c *fiber.Ctx) error {
	_, id, err := ResolveIDAs(c, c.Params("id"), "movie")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var movie database.Movie
	if err := db.First(&movie, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "movie not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	distribution, err := queryRatingDistribution(db, "movie", movie.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	summary := buildRatingSummary(movie.RatingAvg, movie.RatingCount)
	summary.Distribution = distribution
	return c.JSON(summary)
}

func handleGetMovieStats(c *fiber.Ctx) error {
	id, err := response.ParseIDParam(c, "movie")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var movie database.Movie
	if err := db.First(&movie, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "movie not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	stats, err := queryMovieStats(db, movie.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	return c.JSON(stats)
}

func handleGetMovieLists(c *fiber.Ctx) error {
	// Lists containing a movie require the lists engine (not landed). Fails
	// closed with 501 rather than return a fabricated empty list.
	return response.NotImplemented(c, "lists require the lists engine, which is not implemented")
}

func handleGetMovieWatching(c *fiber.Ctx) error {
	// Users currently watching require scrobble/activity wiring (not landed).
	// Fails closed with 501 rather than return a fabricated empty array.
	return response.NotImplemented(c, "watching requires the activity engine, which is not implemented")
}

// ---------------------------------------------------------------------------
// Shows
// ---------------------------------------------------------------------------

func handleGetShow(c *fiber.Ctx) error {
	_, id, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var show database.Show
	if err := db.First(&show, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "show not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	extended := getExtended(c)

	// Fetch seasons.
	var seasons []database.Season
	db.Where("show_id = ?", show.ID).Order("season_number ASC").Find(&seasons)

	// Batch external_ids for the show + all its seasons in one query, then
	// build responses from that shared lookup (no per-item queries).
	refs := make([]mediaRef, 0, len(seasons)+1)
	refs = append(refs, mediaRef{mediaType: "show", mediaID: show.ID})
	for _, s := range seasons {
		refs = append(refs, mediaRef{mediaType: "season", mediaID: s.ID})
	}
	lookup := batchExternalIDs(db, refs)

	resp := buildShowResponse(lookup, &show, extended)
	for _, s := range seasons {
		resp.Seasons = append(resp.Seasons, buildSeasonResponse(lookup, &s))
	}

	return c.JSON(resp)
}

func handleGetShowSeasons(c *fiber.Ctx) error {
	_, id, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var seasons []database.Season
	if err := db.Where("show_id = ?", id).Order("season_number ASC").Find(&seasons).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	// Batch external_ids for all seasons in one query, then build responses
	// from that shared lookup.
	refs := make([]mediaRef, 0, len(seasons))
	for _, s := range seasons {
		refs = append(refs, mediaRef{mediaType: "season", mediaID: s.ID})
	}
	lookup := batchExternalIDs(db, refs)

	resp := make([]SeasonResponse, 0, len(seasons))
	for _, s := range seasons {
		resp = append(resp, buildSeasonResponse(lookup, &s))
	}

	return c.JSON(resp)
}

func handleGetShowSeason(c *fiber.Ctx) error {
	_, showID, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	seasonNum, err := strconv.Atoi(c.Params("season"))
	if err != nil {
		return response.BadRequest(c, "invalid season number")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", showID, seasonNum).
		First(&season).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "season not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	return c.JSON(BuildSeasonResponse(&season))
}

func handleGetShowSeasonEpisodes(c *fiber.Ctx) error {
	_, showID, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	seasonNum, err := strconv.Atoi(c.Params("season"))
	if err != nil {
		return response.BadRequest(c, "invalid season number")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", showID, seasonNum).
		First(&season).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "season not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	page, limit := response.ParsePagination(c, 25)

	var episodes []database.Episode
	if err := db.Where("season_id = ?", season.ID).
		Order("episode_number ASC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&episodes).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	// Batch external_ids for all episodes in one query, then build responses
	// from that shared lookup.
	refs := make([]mediaRef, 0, len(episodes))
	for _, e := range episodes {
		refs = append(refs, mediaRef{mediaType: "episode", mediaID: e.ID})
	}
	lookup := batchExternalIDs(db, refs)

	resp := make([]EpisodeResponse, 0, len(episodes))
	for _, e := range episodes {
		resp = append(resp, buildEpisodeResponse(lookup, &e))
	}

	return c.JSON(resp)
}

func handleGetShowEpisode(c *fiber.Ctx) error {
	_, showID, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	seasonNum, err := strconv.Atoi(c.Params("season"))
	if err != nil {
		return response.BadRequest(c, "invalid season number")
	}
	episodeNum, err := strconv.Atoi(c.Params("episode"))
	if err != nil {
		return response.BadRequest(c, "invalid episode number")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", showID, seasonNum).
		First(&season).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "season not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	var episode database.Episode
	if err := db.Where("season_id = ? AND episode_number = ?", season.ID, episodeNum).
		First(&episode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "episode not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	return c.JSON(BuildEpisodeResponse(&episode))
}

func handleGetShowPeople(c *fiber.Ctx) error {
	_, id, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var credits []database.Credit
	if err := db.Where("media_type = ? AND media_id = ?", "show", id).
		Order("\"order\" ASC").Find(&credits).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	cast, crew := buildPeopleResponse(c, db, credits)
	return c.JSON(PeopleResponse{Cast: cast, Crew: crew})
}

func handleGetShowRatingSummary(c *fiber.Ctx) error {
	_, id, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var show database.Show
	if err := db.First(&show, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "show not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	distribution, err := queryRatingDistribution(db, "show", show.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	summary := buildRatingSummary(show.RatingAvg, show.RatingCount)
	summary.Distribution = distribution
	return c.JSON(summary)
}

func handleGetShowStats(c *fiber.Ctx) error {
	id, err := response.ParseIDParam(c, "show")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var show database.Show
	if err := db.First(&show, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "show not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	stats, err := queryShowStats(db, show.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	return c.JSON(stats)
}

func handleGetShowLists(c *fiber.Ctx) error {
	// Lists containing a show require the lists engine (not landed). Fails
	// closed with 501 rather than return a fabricated empty list.
	return response.NotImplemented(c, "lists require the lists engine, which is not implemented")
}

func handleGetShowWatching(c *fiber.Ctx) error {
	// Users currently watching require scrobble/activity wiring (not landed).
	// Fails closed with 501 rather than return a fabricated empty array.
	return response.NotImplemented(c, "watching requires the activity engine, which is not implemented")
}

// handleGetShowNextEpisode returns the next scheduled episode for a show: the
// earliest episode airing today (UTC) or later. Matches Trakt's
// GET /shows/:id/next_episode, including the 204 when none is scheduled.
func handleGetShowNextEpisode(c *fiber.Ctx) error {
	return showBoundaryEpisode(c, "aired_at >= ?", "aired_at ASC, episode_number ASC")
}

// handleGetShowLastEpisode returns the most recently aired episode for a show:
// the latest episode airing today (UTC) or earlier. Matches Trakt's
// GET /shows/:id/last_episode, including the 204 when nothing has aired.
func handleGetShowLastEpisode(c *fiber.Ctx) error {
	return showBoundaryEpisode(c, "aired_at <= ?", "aired_at DESC, episode_number DESC")
}

// showBoundaryEpisode resolves the show in the route param and returns the one
// episode matching the date predicate and ordering, or 204 when there is none.
// The show must exist: an unknown id is a 404 from ResolveIDAs.
func showBoundaryEpisode(c *fiber.Ctx, datePredicate, order string) error {
	_, showID, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	today := time.Now().UTC().Format("2006-01-02")

	var episode database.Episode
	err = db.Where("show_id = ?", showID).
		Where(datePredicate, today).
		Order(order).
		First(&episode).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.SendStatus(fiber.StatusNoContent)
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	return c.JSON(BuildEpisodeResponse(&episode))
}

// ---------------------------------------------------------------------------
// People
// ---------------------------------------------------------------------------

func handleGetPerson(c *fiber.Ctx) error {
	_, id, err := ResolveIDAs(c, c.Params("id"), "person")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var person database.Person
	if err := db.First(&person, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "person not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	resp := BuildPersonResponse(&person)

	// Include credits.
	var credits []database.Credit
	db.Where("person_id = ?", person.ID).Order("\"order\" ASC").Find(&credits)

	movieCredits := make([]CreditResponse, 0)
	showCredits := make([]CreditResponse, 0)
	for _, cr := range credits {
		crResp := creditToResponse(cr)
		switch cr.MediaType {
		case "movie":
			movieCredits = append(movieCredits, crResp)
		case "show":
			showCredits = append(showCredits, crResp)
		}
	}
	resp.Credits = &PersonCredits{
		Cast: append(movieCredits, showCredits...),
	}

	return c.JSON(resp)
}

func handleGetPersonMovies(c *fiber.Ctx) error {
	_, personID, err := ResolveIDAs(c, c.Params("id"), "person")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var credits []database.Credit
	if err := db.Where("person_id = ? AND media_type = ?", personID, "movie").
		Order("\"order\" ASC").Find(&credits).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	resp := make([]CreditResponse, 0, len(credits))
	for _, cr := range credits {
		resp = append(resp, creditToResponse(cr))
	}

	return c.JSON(resp)
}

func handleGetPersonShows(c *fiber.Ctx) error {
	_, personID, err := ResolveIDAs(c, c.Params("id"), "person")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var credits []database.Credit
	if err := db.Where("person_id = ? AND media_type = ?", personID, "show").
		Order("\"order\" ASC").Find(&credits).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	resp := make([]CreditResponse, 0, len(credits))
	for _, cr := range credits {
		resp = append(resp, creditToResponse(cr))
	}

	return c.JSON(resp)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildPeopleResponse builds cast and crew responses from credits.
func buildPeopleResponse(c *fiber.Ctx, db *gorm.DB, credits []database.Credit) ([]CastMemberResponse, []CrewMemberResponse) {
	cast := make([]CastMemberResponse, 0)
	crew := make([]CrewMemberResponse, 0)

	// Batch-load persons referenced by the credits in one query.
	personIDs := make([]int64, 0, len(credits))
	seen := make(map[int64]bool, len(credits))
	for _, cr := range credits {
		if !seen[cr.PersonID] {
			seen[cr.PersonID] = true
			personIDs = append(personIDs, cr.PersonID)
		}
	}
	personMap := make(map[int64]*database.Person, len(personIDs))
	if len(personIDs) > 0 {
		var persons []database.Person
		if err := db.Where("id IN ?", personIDs).Find(&persons).Error; err == nil {
			for i := range persons {
				personMap[persons[i].ID] = &persons[i]
			}
		}
	}

	// Batch external_ids for all persons in one query.
	refs := make([]mediaRef, 0, len(personMap))
	for id := range personMap {
		refs = append(refs, mediaRef{mediaType: "person", mediaID: id})
	}
	lookup := batchExternalIDs(db, refs)

	for _, cr := range credits {
		person, ok := personMap[cr.PersonID]
		if !ok {
			continue // skip if person not found
		}

		if cr.CreditType == "cast" {
			cast = append(cast, CastMemberResponse{
				Person:    buildPersonResponse(lookup, person),
				Character: cr.Character,
				Order:     cr.Order,
			})
		} else {
			crew = append(crew, CrewMemberResponse{
				Person:     buildPersonResponse(lookup, person),
				Job:        cr.Job,
				Department: cr.Department,
			})
		}
	}

	return cast, crew
}

// creditToResponse converts a Credit to a CreditResponse.
func creditToResponse(cr database.Credit) CreditResponse {
	return CreditResponse{
		MediaType:     cr.MediaType,
		MediaID:       cr.MediaID,
		CreditType:    cr.CreditType,
		Job:           cr.Job,
		Character:     cr.Character,
		Department:    cr.Department,
		SeriesRegular: cr.SeriesRegular,
		EpisodeCount:  cr.EpisodeCount,
	}
}
