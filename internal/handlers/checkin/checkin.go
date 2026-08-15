package checkin

import (
	"encoding/json"
	"fmt"
	"time"

	"trakt-api/internal/auth"
	"trakt-api/internal/cache"
	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"
	"trakt-api/internal/outbox"
	"trakt-api/redis"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"trakt-api/internal/response"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "POST", Path: "/v1/checkin", Summary: "Check into a media item", Tags: []string{"Checkin"}, Auth: "required", StatusCodes: map[int]string{200: "Checked in", 400: "Invalid request", 401: "Unauthorized", 409: "Already checked in"}},
		{Method: "DELETE", Path: "/v1/checkin", Summary: "Clear active checkin", Tags: []string{"Checkin"}, Auth: "required", StatusCodes: map[int]string{204: "Cleared", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/checkin", Summary: "Get current active checkin", Tags: []string{"Checkin"}, Auth: "required", StatusCodes: map[int]string{200: "Active checkin", 204: "No active checkin", 401: "Unauthorized"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires checkin route handlers.
func RegisterRoutes(api fiber.Router) {
	checkinGroup := api.Group("/checkin")

	checkinGroup.Post("/", auth.AuthRequired(), handleCheckinCreate)
	checkinGroup.Delete("/", auth.AuthRequired(), handleCheckinDelete)
	checkinGroup.Get("/", auth.AuthRequired(), handleCheckinGet)
}

// ---------------------------------------------------------------------------
// Auth middleware wrapper
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

type checkinRequest struct {
	Movie   *checkinItem    `json:"movie,omitempty"`
	Episode *checkinItem    `json:"episode,omitempty"`
	Show    *checkinItem    `json:"show,omitempty"`
	Message string          `json:"message,omitempty"`
	Sharing *checkinSharing `json:"sharing,omitempty"`
}

type checkinItem struct {
	IDs    checkinIDs `json:"ids"`
	Season int        `json:"season,omitempty"`
	Number int        `json:"number,omitempty"`
}

type checkinIDs struct {
	TMDB int `json:"tmdb"`
}

type checkinSharing struct {
	Twitter  bool `json:"twitter"`
	Tumblr   bool `json:"tumblr"`
	Facebook bool `json:"facebook"`
}

type checkinResponse struct {
	ID        int64            `json:"id"`
	ExpiresAt string           `json:"expires_at"`
	WatchedAt string           `json:"watched_at"`
	Movie     *checkinMediaRef `json:"movie,omitempty"`
	Episode   *checkinMediaRef `json:"episode,omitempty"`
	Show      *checkinMediaRef `json:"show,omitempty"`
	Message   string           `json:"message,omitempty"`
}

type checkinMediaRef struct {
	IDs   mediaRefIDs `json:"ids"`
	Title string      `json:"title"`
	Year  int         `json:"year,omitempty"`
}

type mediaRefIDs struct {
	TMDB int `json:"tmdb"`
}

func getUserID(c *fiber.Ctx) string {
	uid, _ := c.Locals("user_id").(string)
	return uid
}

// resolveCheckinItem resolves the checkin item to media type/id.
func resolveCheckinItem(c *fiber.Ctx, req *checkinRequest) (string, int64, *int64, *int64, error) {
	if req.Episode != nil {
		mt, id, err := media.ResolveID(c, fmt.Sprintf("%d", req.Episode.IDs.TMDB))
		if err != nil {
			return "", 0, nil, nil, err
		}
		if mt != "show" {
			return "", 0, nil, nil, fmt.Errorf("id not a show")
		}

		db, dbErr := database.GetDB()
		if dbErr != nil {
			return "", 0, nil, nil, fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
		}

		var season database.Season
		if err := db.Where("show_id = ? AND season_number = ?", id, req.Episode.Season).First(&season).Error; err != nil {
			return "", 0, nil, nil, fiber.NewError(fiber.StatusNotFound, "season not found")
		}

		var episode database.Episode
		if err := db.Where("season_id = ? AND episode_number = ?", season.ID, req.Episode.Number).First(&episode).Error; err != nil {
			return "", 0, nil, nil, fiber.NewError(fiber.StatusNotFound, "episode not found")
		}

		sid := season.ID
		eid := episode.ID
		return "episode", episode.ID, &sid, &eid, nil
	}

	if req.Movie != nil {
		mt, id, err := media.ResolveID(c, fmt.Sprintf("%d", req.Movie.IDs.TMDB))
		if err != nil {
			return "", 0, nil, nil, err
		}
		if mt != "movie" {
			return "", 0, nil, nil, fmt.Errorf("id not a movie")
		}
		return "movie", id, nil, nil, nil
	}

	if req.Show != nil {
		mt, id, err := media.ResolveID(c, fmt.Sprintf("%d", req.Show.IDs.TMDB))
		if err != nil {
			return "", 0, nil, nil, err
		}
		if mt != "show" {
			return "", 0, nil, nil, fmt.Errorf("id not a show")
		}
		return "show", id, nil, nil, nil
	}

	return "", 0, nil, nil, fmt.Errorf("no media item specified")
}

// defaultCheckinDuration returns the default checkin expiry (2 hours).
func defaultCheckinDuration() time.Duration {
	return 2 * time.Hour
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleCheckinCreate handles POST /v1/checkin
func handleCheckinCreate(c *fiber.Ctx) error {
	var req checkinRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return response.WriteError(c, fiber.StatusBadRequest, "invalid_request", "invalid JSON body")
	}

	userID := getUserID(c)
	if userID == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "user not authenticated")
	}

	mediaType, mediaID, seasonID, episodeID, err := resolveCheckinItem(c, &req)
	if err != nil {
		return response.WriteError(c, fiber.StatusBadRequest, "invalid_request", err.Error())
	}

	expiresAt := time.Now().Add(defaultCheckinDuration())
	now := time.Now()

	var result *checkinResponse
	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		// Advisory lock for checkin exclusivity
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", userID).Error; err != nil {
			return nil, fmt.Errorf("advisory lock: %w", err)
		}

		// Check if user has an active (non-expired) checkin
		var existing database.UserCheckin
		if result := tx.Where("user_id = ? AND expires_at > ?", userID, now).First(&existing); result.Error == nil {
			return nil, fmt.Errorf("already checked in")
		}

		// Delete any expired checkins for the user
		tx.Where("user_id = ?", userID).Delete(&database.UserCheckin{})

		// Create new checkin
		checkin := &database.UserCheckin{
			UserID:    userID,
			MediaType: mediaType,
			MediaID:   mediaID,
			SeasonID:  seasonID,
			EpisodeID: episodeID,
			Message:   req.Message,
			ExpiresAt: expiresAt,
			CreatedAt: now,
		}
		if err := tx.Create(checkin).Error; err != nil {
			return nil, fmt.Errorf("create checkin: %w", err)
		}

		result = &checkinResponse{
			ID:        checkin.ID,
			ExpiresAt: expiresAt.Format(time.RFC3339),
			WatchedAt: now.Format(time.RFC3339),
			Message:   req.Message,
		}
		if mediaType == "movie" {
			result.Movie = &checkinMediaRef{IDs: mediaRefIDs{TMDB: int(mediaID)}}
		} else if mediaType == "episode" {
			result.Episode = &checkinMediaRef{IDs: mediaRefIDs{TMDB: int(mediaID)}}
		}

		uvk := redis.BuildUserVersionKey(userID)
		cacheTx.AddInvalidation(uvk, map[string]interface{}{
			"user_id":      userID,
			"version_key":  uvk,
			"version_only": true,
		})
		mvk := redis.BuildMediaVersionKey(mediaType, mediaID, nil, nil)
		cacheTx.AddInvalidation(mvk, map[string]interface{}{
			"media_id":     mediaID,
			"media_type":   mediaType,
			"version_key":  mvk,
			"version_only": true,
		})

		if err := outbox.PublishEvent(tx, outbox.EventCheckinCreated, userID, map[string]any{
			"action":     "checkin",
			"media_type": mediaType,
			"media_id":   mediaID,
		}); err != nil {
			return nil, err
		}
		return cacheTx, nil
	})

	if err != nil {
		if err.Error() == "already checked in" {
			return response.WriteError(c, fiber.StatusConflict, "conflict", "you already have an active checkin")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", err.Error())
	}

	return c.JSON(result)
}

// handleCheckinDelete handles DELETE /v1/checkin
func handleCheckinDelete(c *fiber.Ctx) error {
	userID := getUserID(c)
	if userID == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "user not authenticated")
	}

	err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", userID).Error; err != nil {
			return nil, fmt.Errorf("advisory lock: %w", err)
		}

		result := tx.Where("user_id = ?", userID).Delete(&database.UserCheckin{})
		if result.Error != nil {
			return nil, fmt.Errorf("delete checkin: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return cacheTx, nil
		}

		uvk := redis.BuildUserVersionKey(userID)
		cacheTx.AddInvalidation(uvk, map[string]interface{}{
			"user_id":      userID,
			"version_key":  uvk,
			"version_only": true,
		})
		if err := outbox.PublishEvent(tx, outbox.EventCheckinDeleted, userID, map[string]any{
			"action": "delete",
		}); err != nil {
			return nil, err
		}
		return cacheTx, nil
	})

	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", err.Error())
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// handleCheckinGet handles GET /v1/checkin
func handleCheckinGet(c *fiber.Ctx) error {
	userID := getUserID(c)
	if userID == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "user not authenticated")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	now := time.Now()
	var checkin database.UserCheckin
	if err := db.Where("user_id = ? AND expires_at > ?", userID, now).First(&checkin).Error; err != nil {
		// No active checkin
		return c.SendStatus(fiber.StatusNoContent)
	}

	return c.JSON(checkinResponse{
		ID:        checkin.ID,
		ExpiresAt: checkin.ExpiresAt.Format(time.RFC3339),
		WatchedAt: checkin.CreatedAt.Format(time.RFC3339),
		Message:   checkin.Message,
	})
}
