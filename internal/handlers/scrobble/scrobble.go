package scrobble

import (
	"encoding/json"
	"fmt"
	"strconv"
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
)

func init() {
	registerOpenAPIRoutes()
}

// Scrobble session timeout: if no update for 5 hours, session is considered
// abandoned/inactive. The next scrobble event will start a fresh session.
const scrobbleSessionTimeout = 5 * time.Hour

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		// Scrobble
		{Method: "POST", Path: "/v1/scrobble/start", Summary: "Start watching a media item", Tags: []string{"Scrobble"}, Auth: "required", StatusCodes: map[int]string{200: "Watching started", 400: "Invalid request", 401: "Unauthorized", 422: "Unprocessable"}},
		{Method: "POST", Path: "/v1/scrobble/pause", Summary: "Pause watching a media item", Tags: []string{"Scrobble"}, Auth: "required", StatusCodes: map[int]string{200: "Watching paused", 400: "Invalid request", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/scrobble/stop", Summary: "Stop watching — scrobbles if ≥80% progress", Tags: []string{"Scrobble"}, Auth: "required", StatusCodes: map[int]string{200: "Scrobbled or paused", 400: "Invalid request", 409: "Already scrobbled", 422: "Progress too low"}},
		// Playback
		{Method: "GET", Path: "/v1/sync/playback", Summary: "Get all saved playback positions", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Playback list"}},
		{Method: "DELETE", Path: "/v1/sync/playback/{media_type}/{media_id}", Summary: "Remove a playback position", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{204: "Removed", 404: "Not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires scrobble + playback route handlers.
func RegisterRoutes(api fiber.Router) {
	scrobbleGroup := api.Group("/scrobble")
	syncGroup := api.Group("/sync")

	scrobbleGroup.Post("/start", requireAuth, handleScrobbleStart)
	scrobbleGroup.Post("/pause", requireAuth, handleScrobblePause)
	scrobbleGroup.Post("/stop", requireAuth, handleScrobbleStop)

	syncGroup.Get("/playback", requireAuth, handleGetPlayback)
	syncGroup.Delete("/playback/:media_type/:media_id", requireAuth, handleDeletePlayback)
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

type scrobbleRequest struct {
	Movie    *scrobbleItem `json:"movie,omitempty"`
	Episode  *scrobbleItem `json:"episode,omitempty"`
	Show     *scrobbleItem `json:"show,omitempty"`
	Progress float64       `json:"progress"`
}

type scrobbleItem struct {
	IDs    mediaItemIDs `json:"ids"`
	Season int          `json:"season,omitempty"`
	Number int          `json:"number,omitempty"`
}

type mediaItemIDs struct {
	TMDB int `json:"tmdb"`
}

type scrobbleResponse struct {
	ID        int64   `json:"id"`
	Action    string  `json:"action"`
	Progress  float64 `json:"progress"`
	WatchedAt string  `json:"watched_at,omitempty"`
}

type duplicateScrobbleResponse struct {
	WatchedAt string `json:"watched_at"`
	ExpiresAt string `json:"expires_at"`
}

type playbackResponse struct {
	MediaType string  `json:"media_type"`
	MediaID   int64   `json:"media_id"`
	Progress  float64 `json:"progress"`
	PausedAt  string  `json:"paused_at"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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

// resolveScrobbleItem resolves the media type/id from a scrobble request body.
// Returns mediaType, mediaID, seasonID, episodeID, error.
func resolveScrobbleItem(c *fiber.Ctx, req *scrobbleRequest) (string, int64, *int64, *int64, error) {
	if req == nil {
		return "", 0, nil, nil, fmt.Errorf("empty request")
	}

	if req.Episode != nil {
		// Episode scrobble — resolve the show first from episode IDs
		// The episode IDs contain the TMDB show ID, season number, and episode number
		mt, id, err := media.ResolveID(c, fmt.Sprintf("%d", req.Episode.IDs.TMDB))
		if err != nil {
			return "", 0, nil, nil, err
		}
		if mt != "show" && mt != "episode" {
			return "", 0, nil, nil, fmt.Errorf("id not a show")
		}

		// Look up the season and episode IDs from our DB
		db, dbErr := database.GetDB()
		if dbErr != nil {
			return "", 0, nil, nil, fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
		}

		var season database.Season
		if err := db.Where("show_id = ? AND season_number = ?", id, req.Episode.Season).
			First(&season).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return "", 0, nil, nil, fiber.NewError(fiber.StatusNotFound, "season not found")
			}
			return "", 0, nil, nil, fiber.NewError(fiber.StatusInternalServerError, "database query error")
		}

		var episode database.Episode
		if err := db.Where("season_id = ? AND episode_number = ?", season.ID, req.Episode.Number).
			First(&episode).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return "", 0, nil, nil, fiber.NewError(fiber.StatusNotFound, "episode not found")
			}
			return "", 0, nil, nil, fiber.NewError(fiber.StatusInternalServerError, "database query error")
		}

		seasonID := season.ID
		episodeID := episode.ID
		return "episode", episode.ID, &seasonID, &episodeID, nil
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

	return "", 0, nil, nil, fmt.Errorf("no media item specified in request")
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleScrobbleStart handles POST /v1/scrobble/start
// queueScrobbleInvalidation bumps the user's version counter, the media
// entity's version counter, and — for episode scrobbles — the show's episode
// slot, so cached scrobble/playback/activity and show/episode responses are
// invalidated in O(1).
func queueScrobbleInvalidation(tx *gorm.DB, cacheTx *cache.CacheTransaction, userID, mediaType string, mediaID int64) {
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

	if mediaType == "episode" {
		queueEpisodeShowSlotInvalidation(tx, cacheTx, mediaID)
	}
}

// queueEpisodeShowSlotInvalidation bumps the show's version slot for the given
// episode so the cached show response (which embeds that episode's watch state)
// is invalidated without blowing away the whole show. Best-effort: a lookup
// miss just skips the slot bump.
func queueEpisodeShowSlotInvalidation(tx *gorm.DB, cacheTx *cache.CacheTransaction, episodeID int64) {
	var ep database.Episode
	if err := tx.Select("id", "show_id", "season_id", "episode_number").First(&ep, episodeID).Error; err != nil {
		return
	}
	seasonNum := 0
	var season database.Season
	if err := tx.Select("season_number").Where("id = ?", ep.SeasonID).First(&season).Error; err == nil {
		seasonNum = season.SeasonNumber
	}
	vk := redis.BuildMediaVersionKey("show", ep.ShowID, &seasonNum, &ep.EpisodeNumber)
	cacheTx.AddInvalidation(vk, map[string]interface{}{
		"media_id":     ep.ShowID,
		"media_type":   "show",
		"version_key":  vk,
		"version_only": true,
	})
}

// handleScrobbleStart handles POST /v1/scrobble/start
func handleScrobbleStart(c *fiber.Ctx) error {
	var req scrobbleRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return writeAPIError(c, fiber.StatusBadRequest, "invalid_request", "invalid JSON body")
	}

	userID := getUserID(c)
	if userID == "" {
		return writeAPIError(c, fiber.StatusUnauthorized, "unauthorized", "user not authenticated")
	}

	mediaType, mediaID, seasonID, episodeID, err := resolveScrobbleItem(c, &req)
	if err != nil {
		return writeAPIError(c, fiber.StatusBadRequest, "invalid_request", err.Error())
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		// Advisory lock to prevent races
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?), ?)", userID, mediaID).Error; err != nil {
			return nil, fmt.Errorf("advisory lock: %w", err)
		}

		// Clean up expired sessions for this user+media (5h timeout).
		// If a session has been sitting in "start" or "pause" for >5h with no
		// update, it's abandoned — delete it so the player can start fresh.
		tx.Where("user_id = ? AND media_type = ? AND media_id = ? AND expires_at < ?",
			userID, mediaType, mediaID, time.Now()).
			Delete(&database.ScrobbleSession{})

		// Remove prior playback for this user+media (if any)
		tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, mediaType, mediaID).
			Delete(&database.UserPlayback{})

		// Write start session with 5h expiry
		session := &database.ScrobbleSession{
			UserID:    userID,
			MediaType: mediaType,
			MediaID:   mediaID,
			SeasonID:  seasonID,
			EpisodeID: episodeID,
			Action:    "start",
			Progress:  req.Progress,
			ExpiresAt: time.Now().Add(scrobbleSessionTimeout),
		}
		if err := tx.Create(session).Error; err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}

		queueScrobbleInvalidation(tx, cacheTx, userID, mediaType, mediaID)
		if err := outbox.PublishEvent(tx, outbox.EventScrobbleStart, userID, map[string]any{
			"action":     "start",
			"media_type": mediaType,
			"media_id":   mediaID,
			"progress":   req.Progress,
		}); err != nil {
			return nil, err
		}
		return cacheTx, nil
	})

	if err != nil {
		return writeAPIError(c, fiber.StatusInternalServerError, "internal", err.Error())
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"id":       0, // session ID tracked internally
		"action":   "start",
		"progress": req.Progress,
	})
}

// handleScrobblePause handles POST /v1/scrobble/pause
func handleScrobblePause(c *fiber.Ctx) error {
	var req scrobbleRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return writeAPIError(c, fiber.StatusBadRequest, "invalid_request", "invalid JSON body")
	}

	userID := getUserID(c)
	if userID == "" {
		return writeAPIError(c, fiber.StatusUnauthorized, "unauthorized", "user not authenticated")
	}

	mediaType, mediaID, seasonID, episodeID, err := resolveScrobbleItem(c, &req)
	if err != nil {
		return writeAPIError(c, fiber.StatusBadRequest, "invalid_request", err.Error())
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?), ?)", userID, mediaID).Error; err != nil {
			return nil, fmt.Errorf("advisory lock: %w", err)
		}

		// Write pause session with the same 5h expiry as start so a later
		// stop can distinguish a live session from an expired one.
		session := &database.ScrobbleSession{
			UserID:    userID,
			MediaType: mediaType,
			MediaID:   mediaID,
			SeasonID:  seasonID,
			EpisodeID: episodeID,
			Action:    "pause",
			Progress:  req.Progress,
			ExpiresAt: time.Now().Add(scrobbleSessionTimeout),
		}
		if err := tx.Create(session).Error; err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}

		// Save playback position for resume
		playback := &database.UserPlayback{
			UserID:    userID,
			MediaType: mediaType,
			MediaID:   mediaID,
			Progress:  req.Progress,
			PausedAt:  time.Now(),
		}
		if err := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, mediaType, mediaID).
			Assign(playback).
			FirstOrCreate(playback).Error; err != nil {
			return nil, fmt.Errorf("save playback: %w", err)
		}

		queueScrobbleInvalidation(tx, cacheTx, userID, mediaType, mediaID)
		if err := outbox.PublishEvent(tx, outbox.EventScrobblePause, userID, map[string]any{
			"action":     "pause",
			"media_type": mediaType,
			"media_id":   mediaID,
			"progress":   req.Progress,
		}); err != nil {
			return nil, err
		}
		return cacheTx, nil
	})

	if err != nil {
		return writeAPIError(c, fiber.StatusInternalServerError, "internal", err.Error())
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"action":   "pause",
		"progress": req.Progress,
	})
}

// handleScrobbleStop handles POST /v1/scrobble/stop
func handleScrobbleStop(c *fiber.Ctx) error {
	var req scrobbleRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return writeAPIError(c, fiber.StatusBadRequest, "invalid_request", "invalid JSON body")
	}

	userID := getUserID(c)
	if userID == "" {
		return writeAPIError(c, fiber.StatusUnauthorized, "unauthorized", "user not authenticated")
	}

	mediaType, mediaID, seasonID, episodeID, err := resolveScrobbleItem(c, &req)
	if err != nil {
		return writeAPIError(c, fiber.StatusBadRequest, "invalid_request", err.Error())
	}

	// <1% → 422 error
	if req.Progress < 1.0 {
		return writeAPIError(c, fiber.StatusUnprocessableEntity, "unprocessable", "progress too low — must be ≥1%")
	}

	// ≥80% → scrobble, 1–79% → pause
	if req.Progress >= 80.0 {
		// Scrobble path
		var scrobbleResp *scrobbleResponse
		err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
			cacheTx := cache.NewCacheTransaction()

			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?), ?)", userID, mediaID).Error; err != nil {
				return nil, fmt.Errorf("advisory lock: %w", err)
			}

			// Check for expired session (5h timeout with no activity).
			// If the last start/pause session expired, the watching session
			// is dead — player should re-start instead.
			var lastSession database.ScrobbleSession
			if result := tx.Where("user_id = ? AND media_type = ? AND media_id = ? AND action IN ?",
				userID, mediaType, mediaID, []string{"start", "pause"}).
				Order("created_at DESC").First(&lastSession); result.Error == nil {
				if time.Now().After(lastSession.ExpiresAt) {
					return nil, &scrobbleSessionExpiredError{}
				}
			}

			// Check for recent duplicate scrobble (within cooldown period)
			cooldown := 24 * time.Hour
			cooldownThreshold := time.Now().Add(-cooldown)
			var existing database.ScrobbleSession
			if result := tx.Where("user_id = ? AND media_type = ? AND media_id = ? AND action = ? AND created_at > ?",
				userID, mediaType, mediaID, "scrobble", cooldownThreshold).
				Order("created_at DESC").
				First(&existing); result.Error == nil {
				// Duplicate scrobble found
				return nil, &duplicateScrobbleError{
					WatchedAt: existing.CreatedAt.Format(time.RFC3339),
					ExpiresAt: existing.CreatedAt.Add(cooldown).Format(time.RFC3339),
				}
			}

			// Write scrobble session
			session := &database.ScrobbleSession{
				UserID:    userID,
				MediaType: mediaType,
				MediaID:   mediaID,
				SeasonID:  seasonID,
				EpisodeID: episodeID,
				Action:    "scrobble",
				Progress:  req.Progress,
			}
			if err := tx.Create(session).Error; err != nil {
				return nil, fmt.Errorf("create session: %w", err)
			}

			// Remove playback position (fully watched)
			tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, mediaType, mediaID).
				Delete(&database.UserPlayback{})

			scrobbleResp = &scrobbleResponse{
				ID:        session.ID,
				Action:    "scrobble",
				Progress:  req.Progress,
				WatchedAt: session.CreatedAt.Format(time.RFC3339),
			}

			queueScrobbleInvalidation(tx, cacheTx, userID, mediaType, mediaID)
			if err := outbox.PublishEvent(tx, outbox.EventScrobbleStop, userID, map[string]any{
				"action":     "scrobble",
				"media_type": mediaType,
				"media_id":   mediaID,
				"progress":   req.Progress,
				"watched_at": session.CreatedAt.Format(time.RFC3339),
			}); err != nil {
				return nil, err
			}
			return cacheTx, nil
		})

		if err != nil {
			if dupErr, ok := err.(*duplicateScrobbleError); ok {
				return c.Status(fiber.StatusConflict).JSON(duplicateScrobbleResponse{
					WatchedAt: dupErr.WatchedAt,
					ExpiresAt: dupErr.ExpiresAt,
				})
			}
			if _, ok := err.(*scrobbleSessionExpiredError); ok {
				return writeAPIError(c, fiber.StatusGone, "session_expired",
					"scrobble session expired after 5 hours of inactivity — restart watching to begin a new session")
			}
			return writeAPIError(c, fiber.StatusInternalServerError, "internal", err.Error())
		}

		return c.JSON(scrobbleResp)
	}

	// 1–79% → pause behavior
	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?), ?)", userID, mediaID).Error; err != nil {
			return nil, fmt.Errorf("advisory lock: %w", err)
		}

		// Write stop session
		session := &database.ScrobbleSession{
			UserID:    userID,
			MediaType: mediaType,
			MediaID:   mediaID,
			SeasonID:  seasonID,
			EpisodeID: episodeID,
			Action:    "stop",
			Progress:  req.Progress,
		}
		if err := tx.Create(session).Error; err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}

		// Save playback position
		playback := &database.UserPlayback{
			UserID:    userID,
			MediaType: mediaType,
			MediaID:   mediaID,
			Progress:  req.Progress,
			PausedAt:  time.Now(),
		}
		if err := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, mediaType, mediaID).
			Assign(playback).
			FirstOrCreate(playback).Error; err != nil {
			return nil, fmt.Errorf("save playback: %w", err)
		}

		queueScrobbleInvalidation(tx, cacheTx, userID, mediaType, mediaID)
		if err := outbox.PublishEvent(tx, outbox.EventScrobbleStop, userID, map[string]any{
			"action":     "stop",
			"media_type": mediaType,
			"media_id":   mediaID,
			"progress":   req.Progress,
		}); err != nil {
			return nil, err
		}
		return cacheTx, nil
	})

	if err != nil {
		return writeAPIError(c, fiber.StatusInternalServerError, "internal", err.Error())
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"action":   "pause",
		"progress": req.Progress,
	})
}

type duplicateScrobbleError struct {
	WatchedAt string
	ExpiresAt string
}

func (e *duplicateScrobbleError) Error() string {
	return "duplicate scrobble"
}

// scrobbleSessionExpiredError is returned when a stop arrives for a session
// that has been inactive for >5 hours. The client should restart watching.
type scrobbleSessionExpiredError struct{}

func (e *scrobbleSessionExpiredError) Error() string {
	return "scrobble session expired — restart watching to begin a new session"
}

// ---------------------------------------------------------------------------
// Playback handlers (T3.3)
// ---------------------------------------------------------------------------

// validMediaTypes lists the media types accepted by playback routes (and the
// values written by resolveScrobbleItem).
var validMediaTypes = map[string]bool{"movie": true, "show": true, "episode": true}

// parsePlaybackMediaID strictly parses a playback media_id. Unlike fmt.Sscanf
// it rejects partial parses ("12abc") and non-positive values.
func parsePlaybackMediaID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid media_id %q", s)
	}
	return id, nil
}

// handleGetPlayback handles GET /v1/sync/playback
func handleGetPlayback(c *fiber.Ctx) error {
	userID := getUserID(c)
	if userID == "" {
		return writeAPIError(c, fiber.StatusUnauthorized, "unauthorized", "user not authenticated")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeAPIError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	mediaTypeFilter := c.Query("type") // optional filter: movie, show, episode

	if mediaTypeFilter != "" && !validMediaTypes[mediaTypeFilter] {
		return writeAPIError(c, fiber.StatusBadRequest, "invalid_request", "invalid media_type")
	}

	var playback []database.UserPlayback
	query := db.Where("user_id = ?", userID)
	if mediaTypeFilter != "" {
		query = query.Where("media_type = ?", mediaTypeFilter)
	}
	if err := query.Order("paused_at DESC").Find(&playback).Error; err != nil {
		return writeAPIError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	result := make([]playbackResponse, 0, len(playback))
	for _, p := range playback {
		result = append(result, playbackResponse{
			MediaType: p.MediaType,
			MediaID:   p.MediaID,
			Progress:  p.Progress,
			PausedAt:  p.PausedAt.Format(time.RFC3339),
		})
	}

	return c.JSON(result)
}

// handleDeletePlayback handles DELETE /v1/sync/playback/:media_type/:media_id
func handleDeletePlayback(c *fiber.Ctx) error {
	userID := getUserID(c)
	if userID == "" {
		return writeAPIError(c, fiber.StatusUnauthorized, "unauthorized", "user not authenticated")
	}

	mediaType := c.Params("media_type")
	if !validMediaTypes[mediaType] {
		return writeAPIError(c, fiber.StatusBadRequest, "invalid_request", "invalid media_type")
	}

	mediaID, err := parsePlaybackMediaID(c.Params("media_id"))
	if err != nil {
		return writeAPIError(c, fiber.StatusBadRequest, "invalid_request", "invalid media_id")
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, mediaType, mediaID).
			Delete(&database.UserPlayback{})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, errPlaybackNotFound
		}

		uvk := redis.BuildUserVersionKey(userID)
		cacheTx.AddInvalidation(uvk, map[string]interface{}{
			"user_id":      userID,
			"version_key":  uvk,
			"version_only": true,
		})
		return cacheTx, nil
	})
	if err != nil {
		if err == errPlaybackNotFound {
			return writeAPIError(c, fiber.StatusNotFound, "not_found", "playback position not found")
		}
		return writeAPIError(c, fiber.StatusInternalServerError, "internal", "database error")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

var errPlaybackNotFound = fmt.Errorf("playback position not found")
