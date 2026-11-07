package sync

// Per-type read variants for the sync collections (issue #4).
//
// Trakt exposes every sync collection twice: as a mixed list and per media
// type (/sync/history/movies, /sync/ratings/seasons, ...). We already serve
// the mixed lists and they filter on ?type, so a per-type variant is the same
// handler with the media type pinned by the path segment — same query, same
// response builder, same pagination. Pinning is done by a small Locals
// middleware (forceMediaType) that the shared handlers read through
// mediaTypeFilter, so there is exactly one list implementation per collection.
//
// A variant is declared here only when the backing table can actually store
// rows of that media type:
//
//	user_history     media_type movie | episode                 -> movies, episodes
//	user_ratings     media_type movie | show | season | episode -> movies, shows, seasons, episodes
//	user_watchlist   media_type movie | show                    -> movies, shows
//	user_collection  media_type movie | show                    -> movies, shows
//	user_favorites   media_type movie | show                    -> movies, shows
//
// Everything else — history shows/seasons, watchlist seasons/episodes,
// collection seasons/episodes — has no rows that a filter could select and is
// deliberately NOT registered; see sync.md "Per-type reads" for each reason.

import (
	"github.com/gofiber/fiber/v2"

	"github.com/haaku-tv/haaku/internal/openapi"
)

// forcedMediaTypeKey is the Locals key a per-type route uses to pin the
// media_type filter for the shared list handler.
const forcedMediaTypeKey = "sync.media_type"

// perTypeRead is one registered per-type read variant. The route, its OpenAPI
// definition, and the handler tests all read this table, so a variant cannot
// be advertised without being wired (or wired without being declared).
type perTypeRead struct {
	kind      string // collection segment under /v1/sync
	mediaType string // media_type the route pins on the query
	segment   string // Trakt's path segment for that media type
	summary   string
	handler   fiber.Handler
}

var perTypeReads = []perTypeRead{
	// History: user_history stores movie and episode rows only.
	{kind: "history", mediaType: "movie", segment: "movies", summary: "Get watched movies", handler: handleGetHistory},
	{kind: "history", mediaType: "episode", segment: "episodes", summary: "Get watched episodes", handler: handleGetHistory},
	// Collection: user_collection stores movie and show rows only.
	{kind: "collection", mediaType: "movie", segment: "movies", summary: "Get collected movies", handler: handleGetCollection},
	{kind: "collection", mediaType: "show", segment: "shows", summary: "Get collected shows", handler: handleGetCollection},
	// Watchlist: user_watchlist stores movie and show rows only.
	{kind: "watchlist", mediaType: "movie", segment: "movies", summary: "Get watchlist movies", handler: handleGetWatchlist},
	{kind: "watchlist", mediaType: "show", segment: "shows", summary: "Get watchlist shows", handler: handleGetWatchlist},
	// Ratings: user_ratings stores all four media types.
	{kind: "ratings", mediaType: "movie", segment: "movies", summary: "Get movie ratings", handler: handleGetRatings},
	{kind: "ratings", mediaType: "show", segment: "shows", summary: "Get show ratings", handler: handleGetRatings},
	{kind: "ratings", mediaType: "season", segment: "seasons", summary: "Get season ratings", handler: handleGetRatings},
	{kind: "ratings", mediaType: "episode", segment: "episodes", summary: "Get episode ratings", handler: handleGetRatings},
	// Favorites: user_favorites stores movie and show rows only.
	{kind: "favorites", mediaType: "movie", segment: "movies", summary: "Get favorite movies", handler: handleGetFavorites},
	{kind: "favorites", mediaType: "show", segment: "shows", summary: "Get favorite shows", handler: handleGetFavorites},
}

func init() {
	registerPerTypeOpenAPIRoutes()
}

// registerPerTypeOpenAPIRoutes declares the per-type GETs in the OpenAPI
// registry, from the same table the router uses.
func registerPerTypeOpenAPIRoutes() {
	for _, r := range perTypeReads {
		openapi.Register(openapi.RouteDef{
			Method:      "GET",
			Path:        "/v1/sync/" + r.kind + "/" + r.segment,
			Summary:     r.summary,
			Tags:        []string{"Sync"},
			Auth:        "required",
			StatusCodes: map[int]string{200: "Items", 401: "Unauthorized"},
		})
	}
}

// registerPerTypeReads mounts every per-type read variant on the sync group.
func registerPerTypeReads(sync fiber.Router) {
	for _, r := range perTypeReads {
		sync.Get("/"+r.kind+"/"+r.segment, forceMediaType(r.mediaType), r.handler)
	}
}

// forceMediaType pins the media_type filter for the handler it precedes. It is
// a middleware, not a handler, so the list handlers stay the single
// implementation of each collection's query.
func forceMediaType(mediaType string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals(forcedMediaTypeKey, mediaType)
		return c.Next()
	}
}

// mediaTypeFilter is the media_type a read handler should filter on: the value
// pinned by a per-type route, else the ?type query parameter (mixed lists).
// The path segment wins, so ?type= on a per-type route cannot widen it.
func mediaTypeFilter(c *fiber.Ctx) string {
	if mt, ok := c.Locals(forcedMediaTypeKey).(string); ok && mt != "" {
		return mt
	}
	return c.Query("type")
}
