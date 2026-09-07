package stats

import (
	"errors"
	"log"
	"time"

	"trakt-api/internal/cache"
	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
	statsengine "trakt-api/internal/stats"
	projredis "trakt-api/redis"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"trakt-api/internal/response"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{
			Method:  "GET",
			Path:    "/v1/users/{id}/stats",
			Summary: "Get user statistics (plays, minutes, unique media counts, ratings)",
			Tags:    []string{"Users"},
			Auth:    "optional",
			StatusCodes: map[int]string{
				200: "User stats",
				404: "User not found",
			},
		},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires stats routes to the Fiber router.
func RegisterRoutes(api fiber.Router) {
	// Nested under /users/{id}/stats
	api.Get("/users/:id/stats", handleGetUserStats)
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// handleGetUserStats returns computed user statistics.
// Cached via user_version keying; recomputed on demand if stale > 1h.
func handleGetUserStats(c *fiber.Ctx) error {
	userID := c.Params("id")
	if userID == "" {
		return response.BadRequest(c, "user id is required")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Check if user exists.
	var user database.User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "user not found")
		}
		// A DB outage is not the same as a missing user — don't mask it as 404.
		log.Printf("[stats] lookup user %s failed: %v", userID, err)
		return response.ServiceUnavailable(c, "stats are temporarily unavailable")
	}

	// Try fetching cached stats.
	cachedStats, err := statsengine.GetUserStats(db, userID)
	if err != nil {
		log.Printf("[stats] failed to fetch cached stats for %s: %v", userID, err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to fetch stats")
	}

	// Recompute if no stats or stale > 1h.
	if cachedStats == nil || isStale(cachedStats.ComputedAt) {
		log.Printf("[stats] recomputing stats for user %s", userID)
		if err := statsengine.ComputeUserStats(db, userID); err != nil {
			// A recompute failure is a service-side fault — surface it instead of
			// hiding the outage behind a 200-with-zeroed-stats body.
			log.Printf("[stats] failed to recompute stats for %s: %v", userID, err)
			return response.ServiceUnavailable(c, "stats are temporarily unavailable")
		}

		// Re-fetch the freshly computed stats.
		fresh, ferr := statsengine.GetUserStats(db, userID)
		if ferr != nil {
			log.Printf("[stats] failed to re-fetch recomputed stats for %s: %v", userID, ferr)
			return response.ServiceUnavailable(c, "stats are temporarily unavailable")
		}
		cachedStats = fresh

		// Bump user version for cache invalidation.
		if rdb := projredis.GetClient(); rdb != nil {
			if _, err := projredis.IncrementUserVersion(userID); err != nil {
				log.Printf("[stats] failed to bump user version for %s: %v", userID, err)
			}
		}
	}

	if cachedStats == nil {
		// Return zero stats.
		return c.JSON(StatsResponse{
			UserID:         userID,
			Plays:          0,
			Minutes:        0,
			UniqueMovies:   0,
			UniqueShows:    0,
			UniqueEpisodes: 0,
			RatingsCount:   0,
		})
	}

	// Try Redis cache for version-keyed response.
	if rdb := projredis.GetClient(); rdb != nil {
		// Check if we can serve a version-keyed cached response.
		version, _ := cache.GetUserVersion(userID)
		_ = version
	}

	return c.JSON(StatsResponse{
		UserID:         cachedStats.UserID,
		Plays:          cachedStats.Plays,
		Minutes:        cachedStats.Minutes,
		UniqueMovies:   cachedStats.UniqueMovies,
		UniqueShows:    cachedStats.UniqueShows,
		UniqueEpisodes: cachedStats.UniqueEpisodes,
		RatingsCount:   cachedStats.RatingsCount,
	})
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// StatsResponse is the JSON response for GET /v1/users/:id/stats.
type StatsResponse struct {
	UserID         string `json:"user_id"`
	Plays          int64  `json:"plays"`
	Minutes        int64  `json:"minutes"`
	UniqueMovies   int    `json:"unique_movies"`
	UniqueShows    int    `json:"unique_shows"`
	UniqueEpisodes int    `json:"unique_episodes"`
	RatingsCount   int    `json:"ratings_count"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func isStale(t *time.Time) bool {
	if t == nil {
		return true
	}
	return time.Since(*t) > time.Hour
}
