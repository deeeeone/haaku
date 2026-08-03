package checkin

import (
	"encoding/json"
	"fmt"
	"time"

	"trakt-api/internal/auth"
	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
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

	checkinGroup.Post("/", requireAuth, handleCheckinCreate)
	checkinGroup.Delete("/", requireAuth, handleCheckinDelete)
	checkinGroup.Get("/", requireAuth, handleCheckinGet)
}

// ---------------------------------------------------------------------------
// Auth middleware wrapper
// ---------------------------------------------------------------------------

func requireAuth(c *fiber.Ctx) error {
	return auth.AuthRequired()(c)
}

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

func writeAPIError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{
			"code":    code,
			"message": message,
		},
	})
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
		return writeAPIError(c, fiber.StatusBadRequest, "invalid_request", "invalid JSON body")
	}

	userID := getUserID(c)
	if userID == "" {
		return writeAPIError(c, fiber.StatusUnauthorized, "unauthorized", "user not authenticated")
	}

	mediaType, mediaID, seasonID, episodeID, err := resolveCheckinItem(c, &req)
	if err != nil {
		return writeAPIError(c, fiber.StatusBadRequest, "invalid_request", err.Error())
	}

	expiresAt := time.Now().Add(defaultCheckinDuration())
	now := time.Now()

	var response *checkinResponse
	err = database.WithTransaction(func(tx *gorm.DB) error {
		// Advisory lock for checkin exclusivity
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", userID).Error; err != nil {
			return fmt.Errorf("advisory lock: %w", err)
		}

		// Check if user has an active (non-expired) checkin
		var existing database.UserCheckin
		if result := tx.Where("user_id = ? AND expires_at > ?", userID, now).First(&existing); result.Error == nil {
			return fmt.Errorf("already checked in")
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
			return fmt.Errorf("create checkin: %w", err)
		}

		response = &checkinResponse{
			ID:        checkin.ID,
			ExpiresAt: expiresAt.Format(time.RFC3339),
			WatchedAt: now.Format(time.RFC3339),
			Message:   req.Message,
		}
		if mediaType == "movie" {
			response.Movie = &checkinMediaRef{IDs: mediaRefIDs{TMDB: int(mediaID)}}
		} else if mediaType == "episode" {
			response.Episode = &checkinMediaRef{IDs: mediaRefIDs{TMDB: int(mediaID)}}
		}
		return nil
	})

	if err != nil {
		if err.Error() == "already checked in" {
			return writeAPIError(c, fiber.StatusConflict, "conflict", "you already have an active checkin")
		}
		return writeAPIError(c, fiber.StatusInternalServerError, "internal", err.Error())
	}

	return c.JSON(response)
}

// handleCheckinDelete handles DELETE /v1/checkin
func handleCheckinDelete(c *fiber.Ctx) error {
	userID := getUserID(c)
	if userID == "" {
		return writeAPIError(c, fiber.StatusUnauthorized, "unauthorized", "user not authenticated")
	}

	err := database.WithTransaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", userID).Error; err != nil {
			return fmt.Errorf("advisory lock: %w", err)
		}

		result := tx.Where("user_id = ?", userID).Delete(&database.UserCheckin{})
		if result.Error != nil {
			return fmt.Errorf("delete checkin: %w", result.Error)
		}
		return nil
	})

	if err != nil {
		return writeAPIError(c, fiber.StatusInternalServerError, "internal", err.Error())
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// handleCheckinGet handles GET /v1/checkin
func handleCheckinGet(c *fiber.Ctx) error {
	userID := getUserID(c)
	if userID == "" {
		return writeAPIError(c, fiber.StatusUnauthorized, "unauthorized", "user not authenticated")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeAPIError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
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
