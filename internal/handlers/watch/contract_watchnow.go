package watch

import (
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped watch-now routes.
//
// Trakt's /movies/{id}/watchnow/{country} and /shows/{id}/watchnow/{country}
// carry the country as a path segment; our /v1/{movies,shows}/{id}/watch takes
// it as ?country=. These routes resolve the same row, read the same cached
// provider set, and emit the contract's category object (cable/free/cinema/
// subscription/purchase, each an array of provider entries).

func init() {
	openapi.Register(openapi.RouteDef{
		Method:      "GET",
		Path:        "/v1/episodes/{id}/watchnow/{country}",
		Summary:     "Episode watch-now providers for one country (contract path)",
		Tags:        []string{"Episodes"},
		Auth:        "none",
		StatusCodes: map[int]string{200: "Provider availability by category", 503: "Provider data temporarily unavailable"},
	})
	openapi.Register(openapi.RouteDef{
		Method:      "GET",
		Path:        "/v1/shows/{id}/seasons/{season}/episodes/{episode}/watchnow/{country}",
		Summary:     "Episode watch-now providers for one country (contract path)",
		Tags:        []string{"Shows"},
		Auth:        "none",
		StatusCodes: map[int]string{200: "Provider availability by category", 503: "Provider data temporarily unavailable"},
	})
	openapi.Register(openapi.RouteDef{
		Method:      "GET",
		Path:        "/v1/movies/{id}/watchnow/justwatch_links/{country}",
		Summary:     "JustWatch links for a movie in one country (contract path)",
		Tags:        []string{"Movies"},
		Auth:        "none",
		StatusCodes: map[int]string{200: "JustWatch links", 503: "Provider data temporarily unavailable"},
	})
	for _, typ := range []string{"movies", "shows"} {
		openapi.Register(openapi.RouteDef{
			Method:  "GET",
			Path:    "/v1/" + typ + "/{id}/watchnow/{country}",
			Summary: "Watch-now providers for one country (contract path)",
			Tags:    []string{titleTag(typ)},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Provider availability by category",
				400: "Invalid country code",
				404: "Media not found",
				// A cache miss goes upstream to TMDB; when that fetch fails the handler
				// answers 503 rather than an empty 200, so the contract has to say so or
				// a client cannot discover the case.
				503: "Provider data temporarily unavailable",
			},
		})
	}
}

func titleTag(plural string) string {
	if plural == "movies" {
		return "Movies"
	}
	return "Shows"
}

// registerContractWatchRoutes mounts the contract-shaped watch-now routes.
func registerContractWatchRoutes(api fiber.Router) {
	// The justwatch_links path is registered before the parameterised
	// /movies/:id/watchnow/:country route so "justwatch_links" cannot be read
	// as a country segment.
	api.Get("/movies/:id/watchnow/justwatch_links/:country", handleMovieJustWatchLinks)
	api.Get("/movies/:id/watchnow/:country", handleContractMovieWatch)
	api.Get("/shows/:id/watchnow/:country", handleContractShowWatch)
	api.Get("/episodes/:id/watchnow/:country", handleContractFlatEpisodeWatch)
	api.Get("/shows/:id/seasons/:season/episodes/:episode/watchnow/:country", handleContractEpisodeWatch)
}

func handleContractMovieWatch(c *fiber.Ctx) error {
	_, id, err := media.ResolveIDAs(c, c.Params("id"), "movie")
	if err != nil {
		return err
	}
	country, cerr := contractCountry(c)
	if cerr != nil {
		return cerr
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
	if movie.TmdbID == 0 {
		return response.NotFound(c, "movie has no TMDB ID")
	}
	return serveContractWatchProviders(c, movie.Metadata, country, "movie", func() error {
		return db.Model(&movie).Update("metadata", movie.Metadata).Error
	}, func() (*tmdb.WatchProvidersResponse, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		return client.GetMovieWatchProviders(movie.TmdbID)
	})
}

func handleContractShowWatch(c *fiber.Ctx) error {
	_, id, err := media.ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}
	country, cerr := contractCountry(c)
	if cerr != nil {
		return cerr
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
	if show.TmdbID == 0 {
		return response.NotFound(c, "show has no TMDB ID")
	}
	return serveContractWatchProviders(c, show.Metadata, country, "show", func() error {
		return db.Model(&show).Update("metadata", show.Metadata).Error
	}, func() (*tmdb.WatchProvidersResponse, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		return client.GetShowWatchProviders(show.TmdbID)
	})
}

// handleContractEpisodeWatch serves
// GET /v1/shows/{id}/seasons/{season}/episodes/{episode}/watchnow/{country}.
func handleContractEpisodeWatch(c *fiber.Ctx) error {
	show, season, episode, db, err := loadEpisodeForWatch(c)
	if err != nil {
		return err
	}
	return serveEpisodeWatch(c, show, season, episode, db)
}

// handleContractFlatEpisodeWatch serves
// GET /v1/episodes/{id}/watchnow/{country} — the flat episode path reusing the
// nested handler's builder. The path id is always numeric, so a non-integer is
// a client error (400), unlike the slug-capable show lookup.
func handleContractFlatEpisodeWatch(c *fiber.Ctx) error {
	id, err := response.ParseIDParam(c, "episode")
	if err != nil {
		return err
	}
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}
	var episode database.Episode
	if err := db.First(&episode, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return stopWatch(response.NotFound(c, "episode not found"))
		}
		return stopWatch(response.InternalError(c, "database query error"))
	}
	var season database.Season
	if err := db.First(&season, episode.SeasonID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return stopWatch(response.NotFound(c, "season not found"))
		}
		return stopWatch(response.InternalError(c, "database query error"))
	}
	var show database.Show
	if err := db.First(&show, episode.ShowID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return stopWatch(response.NotFound(c, "show not found"))
		}
		return stopWatch(response.InternalError(c, "database query error"))
	}
	return serveEpisodeWatch(c, &show, &season, &episode, db)
}

// serveEpisodeWatch is the shared tail of both episode watch-now routes.
func serveEpisodeWatch(c *fiber.Ctx, show *database.Show, season *database.Season, episode *database.Episode, db *gorm.DB) error {
	country, cerr := contractCountry(c)
	if cerr != nil {
		return cerr
	}
	if show.TmdbID == 0 {
		return stopWatch(response.NotFound(c, "show has no TMDB ID"))
	}
	client, clientErr := getTMDBClient()
	if clientErr != nil {
		// TMDB is not configured: serve whatever was already stored (an empty
		// category object when nothing was), which the contract allows.
		return c.JSON(contractWatchFromCache(episode.Metadata, country))
	}
	return serveContractWatchProviders(c, episode.Metadata, country, "episode", func() error {
		return db.Model(episode).Update("metadata", episode.Metadata).Error
	}, func() (*tmdb.WatchProvidersResponse, error) {
		return client.GetEpisodeWatchProviders(show.TmdbID, season.SeasonNumber, episode.EpisodeNumber)
	})
}

// contractWatchFromCache renders the stored providers for one country without
// a provider call, or the contract's empty category object.
func contractWatchFromCache(meta database.JSONMap, country string) map[string]any {
	if cached, ok := loadCachedProviders(meta); ok {
		if result, found := cached.Results[strings.ToUpper(country)]; found {
			return contractWatchResponse(result)
		}
	}
	return emptyContractWatch()
}

// loadEpisodeForWatch resolves show+season+episode for the nested watch-now
// route. It mirrors media's loader but stays local so the watch package does not
// have to export it.
func loadEpisodeForWatch(c *fiber.Ctx) (*database.Show, *database.Season, *database.Episode, *gorm.DB, error) {
	_, showID, err := media.ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return nil, nil, nil, nil, err
	}
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return nil, nil, nil, nil, fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var show database.Show
	if err := db.First(&show, showID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, nil, stopWatch(response.NotFound(c, "show not found"))
		}
		return nil, nil, nil, nil, stopWatch(response.InternalError(c, "database query error"))
	}

	seasonNumber, aerr := strconv.Atoi(c.Params("season"))
	if aerr != nil {
		return nil, nil, nil, nil, stopWatch(response.NotFound(c, "season not found"))
	}
	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", show.ID, seasonNumber).
		First(&season).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, nil, stopWatch(response.NotFound(c, "season not found"))
		}
		return nil, nil, nil, nil, stopWatch(response.InternalError(c, "database query error"))
	}

	episodeNumber, aerr := strconv.Atoi(c.Params("episode"))
	if aerr != nil {
		return nil, nil, nil, nil, stopWatch(response.NotFound(c, "episode not found"))
	}
	var episode database.Episode
	if err := db.Where("season_id = ? AND episode_number = ?", season.ID, episodeNumber).
		First(&episode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, nil, stopWatch(response.NotFound(c, "episode not found"))
		}
		return nil, nil, nil, nil, stopWatch(response.InternalError(c, "database query error"))
	}
	return &show, &season, &episode, db, nil
}

// stopWatch converts an envelope writer's nil-on-success into the sentinel that
// stops the handler after the response has already been written.
func stopWatch(err error) error {
	if err != nil {
		return err
	}
	return response.ErrHandled
}

// handleMovieJustWatchLinks serves
// GET /v1/movies/{id}/watchnow/justwatch_links/{country}.
//
// TMDB's watch/providers payload carries one JustWatch-sourced `link` per
// country. The contract's schema is an open string-to-string record, so the
// real link is served under the key "web" and an absent link is an empty
// object — no key or URL is invented.
func handleMovieJustWatchLinks(c *fiber.Ctx) error {
	_, id, err := media.ResolveIDAs(c, c.Params("id"), "movie")
	if err != nil {
		return err
	}
	country, cerr := contractCountry(c)
	if cerr != nil {
		return cerr
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
	if movie.TmdbID == 0 {
		return response.NotFound(c, "movie has no TMDB ID")
	}

	client, clientErr := getTMDBClient()
	if clientErr != nil {
		// TMDB is not configured: serve whatever the stored provider set has,
		// which is an empty record when nothing was ever fetched.
		return c.JSON(justWatchLinkBody(storedJustWatchLink(movie.Metadata, country)))
	}

	persist := func() error {
		return db.Model(&movie).Update("metadata", movie.Metadata).Error
	}
	fetch := func() (*tmdb.WatchProvidersResponse, error) {
		return client.GetMovieWatchProviders(movie.TmdbID)
	}
	return serveJustWatchLink(c, movie.Metadata, country, persist, fetch)
}

// serveJustWatchLink reads the cached provider set once, fetches it when the
// requested country's absence was not recently confirmed, and returns the
// country's JustWatch link as the contract's record.
func serveJustWatchLink(
	c *fiber.Ctx,
	meta database.JSONMap,
	country string,
	persist func() error,
	fetch func() (*tmdb.WatchProvidersResponse, error),
) error {
	country = strings.ToUpper(country)

	if cached, ok := loadCachedProviders(meta); ok {
		if result, found := cached.Results[country]; found {
			return c.JSON(justWatchLinkBody(result.Link))
		}
		if countryAbsenceFresh(meta, country) {
			return c.JSON(map[string]string{})
		}
	}

	resp, err := fetch()
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "TMDB watch providers unavailable")
	}

	cached := cachedWatchProviders{
		SyncedAt: time.Now().UTC().Format(time.RFC3339),
		Results:  make(map[string]cachedCountryResult),
	}
	for code, cp := range resp.Results {
		cached.Results[code] = cachedCountryResult{Link: cp.Link, Flatrate: cp.Flatrate, Rent: cp.Rent, Buy: cp.Buy}
	}
	if err := storeCachedProviders(meta, cached); err != nil {
		log.Printf("[watch] failed to cache watch providers: %v", err)
	}

	result, found := cached.Results[country]
	if found {
		clearCountryAbsence(meta, country)
	} else {
		recordCountryAbsence(meta, country)
	}
	if err := persist(); err != nil {
		log.Printf("[watch] failed to persist watch providers cache: %v", err)
	}
	return c.JSON(justWatchLinkBody(result.Link))
}

// storedJustWatchLink reads the country's link from an already-stored provider
// set without a provider call.
func storedJustWatchLink(meta database.JSONMap, country string) string {
	if cached, ok := loadCachedProviders(meta); ok {
		if result, found := cached.Results[strings.ToUpper(country)]; found {
			return result.Link
		}
	}
	return ""
}

// justWatchLinkBody renders the contract's open string record. The key names
// the platform the URL is served on; TMDB gives a single JustWatch web page, so
// there is exactly one entry when a link exists.
func justWatchLinkBody(link string) map[string]string {
	if link == "" {
		return map[string]string{}
	}
	return map[string]string{"web": link}
}

// contractCountry reads the country path segment.
func contractCountry(c *fiber.Ctx) (string, error) {
	country := strings.ToUpper(strings.TrimSpace(c.Params("country")))
	if len(country) != 2 {
		return "", response.BadRequest(c, "invalid country code: must be a 2-letter ISO code")
	}
	return country, nil
}

// serveContractWatchProviders mirrors serveWatchProviders but writes the
// contract's category object instead of our flat provider envelope.
func serveContractWatchProviders(
	c *fiber.Ctx,
	metadata database.JSONMap,
	country string,
	mediaType string,
	persist func() error,
	fetchFresh func() (*tmdb.WatchProvidersResponse, error),
) error {
	country = strings.ToUpper(country)

	if cached, ok := loadCachedProviders(metadata); ok {
		if countryResult, found := cached.Results[country]; found {
			return c.JSON(contractWatchResponse(countryResult))
		}
		if countryAbsenceFresh(metadata, country) {
			return c.JSON(emptyContractWatch())
		}
	}

	resp, err := fetchFresh()
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "TMDB watch providers unavailable")
	}

	cached := cachedWatchProviders{
		SyncedAt: time.Now().UTC().Format(time.RFC3339),
		Results:  make(map[string]cachedCountryResult),
	}
	for code, cp := range resp.Results {
		cached.Results[code] = cachedCountryResult{Link: cp.Link, Flatrate: cp.Flatrate, Rent: cp.Rent, Buy: cp.Buy}
	}
	if err := storeCachedProviders(metadata, cached); err != nil {
		log.Printf("[watch] failed to cache watch providers: %v", err)
	}

	countryResult, found := cached.Results[country]
	if found {
		clearCountryAbsence(metadata, country)
		if err := persist(); err != nil {
			log.Printf("[watch] failed to persist watch providers cache: %v", err)
		}
		return c.JSON(contractWatchResponse(countryResult))
	}

	recordCountryAbsence(metadata, country)
	if err := persist(); err != nil {
		log.Printf("[watch] failed to persist watch providers cache: %v", err)
	}
	return c.JSON(emptyContractWatch())
}

// contractWatchResponse maps the stored TMDB provider sets onto the contract's
// category object. TMDB has flatrate/rent/buy; flatrate is the subscription
// set, buy and rent are purchases. cable/free/cinema have no TMDB source and
// are reported empty rather than invented.
func contractWatchResponse(cp cachedCountryResult) map[string]any {
	return map[string]any{
		"cable":        []any{},
		"free":         []any{},
		"cinema":       []any{},
		"subscription": providersToContract(cp.Flatrate, cp.Link),
		"purchase":     providersToContract(mergePurchaseProviders(cp.Buy, cp.Rent), cp.Link),
	}
}

func emptyContractWatch() map[string]any {
	return contractWatchResponse(cachedCountryResult{})
}

// mergePurchaseProviders concatenates the buy and rent sets, keeping the first
// occurrence of each provider. A provider that both sells and rents a title is
// one row in the contract's purchase list, not two.
func mergePurchaseProviders(buy, rent []tmdb.Provider) []tmdb.Provider {
	out := make([]tmdb.Provider, 0, len(buy)+len(rent))
	seen := make(map[int]bool, len(buy)+len(rent))
	for _, set := range [][]tmdb.Provider{buy, rent} {
		for _, p := range set {
			if seen[p.ProviderID] {
				continue
			}
			seen[p.ProviderID] = true
			out = append(out, p)
		}
	}
	return out
}

func providersToContract(providers []tmdb.Provider, countryLink string) []map[string]any {
	out := make([]map[string]any, 0, len(providers))
	for _, p := range providers {
		out = append(out, map[string]any{
			"source":    strings.ToLower(p.ProviderName),
			"link":      countryLink,
			"uhd":       false,
			"currency":  nil,
			"prices":    map[string]any{"rent": nil, "purchase": nil},
			"link_tvos": nil,
		})
	}
	return out
}
