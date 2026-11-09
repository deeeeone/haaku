package watch

import (
	"errors"
	"log"
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
	api.Get("/movies/:id/watchnow/:country", handleContractMovieWatch)
	api.Get("/shows/:id/watchnow/:country", handleContractShowWatch)
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
