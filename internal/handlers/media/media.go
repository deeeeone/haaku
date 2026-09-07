package media

import (
	"errors"
	"strconv"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"trakt-api/internal/response"
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
		{Method: "GET", Path: "/v1/movies/{id}/lists", Summary: "Get lists containing a movie", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "Lists"}},
		{Method: "GET", Path: "/v1/movies/{id}/watching", Summary: "Users watching a movie right now", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "Watching"}},

		// Shows
		{Method: "GET", Path: "/v1/shows/{id}", Summary: "Get a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Show"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons", Summary: "Get seasons for a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Seasons"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}", Summary: "Get a single season", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Season"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/episodes", Summary: "Get episodes for a season", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Episodes"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/episodes/{episode}", Summary: "Get a single episode", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Episode"}},
		{Method: "GET", Path: "/v1/shows/{id}/people", Summary: "Get cast and crew for a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "People"}},
		{Method: "GET", Path: "/v1/shows/{id}/ratings", Summary: "Get ratings for a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Rating summary"}},
		{Method: "GET", Path: "/v1/shows/{id}/stats", Summary: "Get stats for a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Stats"}},
		{Method: "GET", Path: "/v1/shows/{id}/lists", Summary: "Get lists containing a show", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Lists"}},
		{Method: "GET", Path: "/v1/shows/{id}/watching", Summary: "Users watching a show right now", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Watching"}},

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
	mediaType, id, err := ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}
	if mediaType != "movie" {
		return response.NotFound(c, "not a movie")
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
	_, id, err := ResolveID(c, c.Params("id"))
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
	_, id, err := ResolveID(c, c.Params("id"))
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

	return c.JSON(buildRatingSummary(movie.RatingAvg, movie.RatingCount))
}

func handleGetMovieStats(c *fiber.Ctx) error {
	_, _, err := ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}

	// Stub: return zero stats until user activity tables exist (Phase 2+).
	return c.JSON(StatsResponse{
		Watchers:   0,
		Plays:      0,
		Collectors: 0,
		Lists:      0,
	})
}

func handleGetMovieLists(c *fiber.Ctx) error {
	_, _, err := ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}

	// Stub until lists are implemented (Phase 1+).
	return c.JSON(ListsResponse{})
}

func handleGetMovieWatching(c *fiber.Ctx) error {
	_, _, err := ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}

	// Stub — return empty array for now.
	return c.JSON(WatchingResponse{})
}

// ---------------------------------------------------------------------------
// Shows
// ---------------------------------------------------------------------------

func handleGetShow(c *fiber.Ctx) error {
	mediaType, id, err := ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}
	if mediaType != "show" {
		return response.NotFound(c, "not a show")
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
	resp := BuildShowResponse(&show, extended)

	// Fetch seasons.
	var seasons []database.Season
	db.Where("show_id = ?", show.ID).Order("season_number ASC").Find(&seasons)
	for _, s := range seasons {
		resp.Seasons = append(resp.Seasons, BuildSeasonResponse(&s))
	}

	return c.JSON(resp)
}

func handleGetShowSeasons(c *fiber.Ctx) error {
	mediaType, id, err := ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}
	if mediaType != "show" {
		return response.NotFound(c, "not a show")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var seasons []database.Season
	if err := db.Where("show_id = ?", id).Order("season_number ASC").Find(&seasons).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	resp := make([]SeasonResponse, 0, len(seasons))
	for _, s := range seasons {
		resp = append(resp, BuildSeasonResponse(&s))
	}

	return c.JSON(resp)
}

func handleGetShowSeason(c *fiber.Ctx) error {
	showMediaType, showID, err := ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}
	if showMediaType != "show" {
		return response.NotFound(c, "not a show")
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
	showMediaType, showID, err := ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}
	if showMediaType != "show" {
		return response.NotFound(c, "not a show")
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

	resp := make([]EpisodeResponse, 0, len(episodes))
	for _, e := range episodes {
		resp = append(resp, BuildEpisodeResponse(&e))
	}

	return c.JSON(resp)
}

func handleGetShowEpisode(c *fiber.Ctx) error {
	showMediaType, showID, err := ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}
	if showMediaType != "show" {
		return response.NotFound(c, "not a show")
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
	_, id, err := ResolveID(c, c.Params("id"))
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
	_, id, err := ResolveID(c, c.Params("id"))
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

	return c.JSON(buildRatingSummary(show.RatingAvg, show.RatingCount))
}

func handleGetShowStats(c *fiber.Ctx) error {
	_, _, err := ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}

	return c.JSON(StatsResponse{
		Watchers:   0,
		Plays:      0,
		Collectors: 0,
		Lists:      0,
	})
}

func handleGetShowLists(c *fiber.Ctx) error {
	_, _, err := ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}

	return c.JSON(ListsResponse{})
}

func handleGetShowWatching(c *fiber.Ctx) error {
	_, _, err := ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}

	return c.JSON(WatchingResponse{})
}

// ---------------------------------------------------------------------------
// People
// ---------------------------------------------------------------------------

func handleGetPerson(c *fiber.Ctx) error {
	mediaType, id, err := ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}
	if mediaType != "person" {
		return response.NotFound(c, "not a person")
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

	resp := buildPersonResponse(&person)

	// Include credits.
	var credits []database.Credit
	db.Where("person_id = ?", person.ID).Order("\"order\" ASC").Find(&credits)

	movieCredits := make([]CreditResponse, 0)
	showCredits := make([]CreditResponse, 0)
	for _, cr := range credits {
		crResp := creditToResponse(cr)
		if cr.MediaType == "movie" {
			movieCredits = append(movieCredits, crResp)
		} else if cr.MediaType == "show" {
			showCredits = append(showCredits, crResp)
		}
	}
	resp.Credits = &PersonCredits{
		Cast: append(movieCredits, showCredits...),
	}

	return c.JSON(resp)
}

func handleGetPersonMovies(c *fiber.Ctx) error {
	_, personID, err := ResolveID(c, c.Params("id"))
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
	_, personID, err := ResolveID(c, c.Params("id"))
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

	for _, cr := range credits {
		var person database.Person
		if err := db.First(&person, cr.PersonID).Error; err != nil {
			continue // skip if person not found
		}

		if cr.CreditType == "cast" {
			cast = append(cast, CastMemberResponse{
				Person:    buildPersonResponse(&person),
				Character: cr.Character,
				Order:     cr.Order,
			})
		} else {
			crew = append(crew, CrewMemberResponse{
				Person:     buildPersonResponse(&person),
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
