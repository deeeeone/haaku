package reports

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
	projredis "trakt-api/redis"

	"github.com/gofiber/fiber/v2"
	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"trakt-api/internal/auth"
	"trakt-api/internal/response"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "POST", Path: "/v1/movies/{id}/report", Summary: "Report a movie", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 400: "Invalid request or duplicate", 401: "Unauthorized", 404: "Movie not found"}},
		{Method: "POST", Path: "/v1/shows/{id}/report", Summary: "Report a show", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 400: "Invalid request or duplicate", 401: "Unauthorized", 404: "Show not found"}},
		{Method: "POST", Path: "/v1/seasons/{id}/report", Summary: "Report a season", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 400: "Invalid request or duplicate", 401: "Unauthorized", 404: "Season not found"}},
		{Method: "POST", Path: "/v1/episodes/{id}/report", Summary: "Report an episode", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 400: "Invalid request or duplicate", 401: "Unauthorized", 404: "Episode not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires content report routes.
func RegisterRoutes(api fiber.Router) {
	api.Post("/movies/:id/report", auth.AuthRequired(), handleReportMovie)
	api.Post("/shows/:id/report", auth.AuthRequired(), handleReportShow)
	api.Post("/seasons/:id/report", auth.AuthRequired(), handleReportSeason)
	api.Post("/episodes/:id/report", auth.AuthRequired(), handleReportEpisode)
}

// ---------------------------------------------------------------------------
// Auth middleware
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Valid reasons
// ---------------------------------------------------------------------------

var validReportReasons = map[string]bool{
	"spam":      true,
	"spoiler":   true,
	"offensive": true,
	"incorrect": true,
	"duplicate": true,
	"other":     true,
}

// maxReportDescription caps free-form report detail. The schema column is
// unbounded text; this cap keeps moderation notes reasonable and bounded.
const maxReportDescription = 1000

// Per-user report submission rate limit (sliding window, stored in Redis).
// Each user may submit at most reportRateMax reports per reportRateWindow.
const (
	reportRateWindow = time.Hour
	reportRateMax    = 10
)

// ---------------------------------------------------------------------------
// Request/Response types
// ---------------------------------------------------------------------------

type reportRequest struct {
	Reason      string `json:"reason"`
	Description string `json:"description,omitempty"`
}

type reportResponse struct {
	ID          int64  `json:"id"`
	ReporterID  string `json:"reporter_id"`
	MediaType   string `json:"media_type"`
	MediaID     int64  `json:"media_id"`
	Reason      string `json:"reason"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

func toReportResponse(r *database.ContentReport) reportResponse {
	return reportResponse{
		ID:          r.ID,
		ReporterID:  r.ReporterID,
		MediaType:   r.MediaType,
		MediaID:     r.MediaID,
		Reason:      r.Reason,
		Description: r.Description,
		CreatedAt:   response.FormatTime(r.CreatedAt),
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// POST /v1/movies/:id/report
func handleReportMovie(c *fiber.Ctx) error {
	return handleReportContent(c, "movie")
}

// POST /v1/shows/:id/report
func handleReportShow(c *fiber.Ctx) error {
	return handleReportContent(c, "show")
}

// POST /v1/seasons/:id/report
func handleReportSeason(c *fiber.Ctx) error {
	return handleReportContent(c, "season")
}

// POST /v1/episodes/:id/report
func handleReportEpisode(c *fiber.Ctx) error {
	return handleReportContent(c, "episode")
}

func handleReportContent(c *fiber.Ctx, mediaType string) error {
	userID := c.Locals("user_id").(string)
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return response.BadRequest(c, "invalid media id")
	}

	var req reportRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid request body")
	}

	reason := strings.ToLower(strings.TrimSpace(req.Reason))
	if !validReportReasons[reason] {
		return response.BadRequest(c, "invalid reason: must be one of spam, spoiler, offensive, incorrect, duplicate, other")
	}

	description := strings.TrimSpace(req.Description)
	if len(description) > maxReportDescription {
		return response.BadRequest(c, fmt.Sprintf("description exceeds maximum length of %d characters", maxReportDescription))
	}

	// Rate-limit report submissions per user before doing any work, so abuse
	// is throttled regardless of the target media.
	if err := reportRateAllowed(c, userID); err != nil {
		return err
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// The OpenAPI contract advertises a 404 for a missing target media item, so
	// verify the media row exists before inserting a report against it.
	exists, verr := reportTargetExists(db, mediaType, id)
	if verr != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}
	if !exists {
		return response.NotFound(c, mediaType+" not found")
	}

	// Check for duplicate report.
	var existing database.ContentReport
	result := db.Where("reporter_id = ? AND media_type = ? AND media_id = ?", userID, mediaType, id).
		First(&existing)
	if result.Error == nil {
		return response.BadRequest(c, "you have already reported this content")
	}
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	report := database.ContentReport{
		ReporterID:  userID,
		MediaType:   mediaType,
		MediaID:     id,
		Reason:      reason,
		Description: description,
	}

	if err := db.Create(&report).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to create report")
	}

	return c.Status(fiber.StatusCreated).JSON(toReportResponse(&report))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// reportRateAllowed enforces a per-user sliding-window limit on report
// submissions via Redis. It returns a 429 rate_limited response (with a
// Retry-After header) when the user exceeds reportRateMax submissions within
// reportRateWindow, and nil otherwise. Redis failures fail OPEN: the request
// is allowed through so an unavailable cache never blocks legitimate use.
func reportRateAllowed(c *fiber.Ctx, userID string) error {
	rdb := projredis.GetClient()
	if rdb == nil {
		return nil
	}

	key := "ratelimit:report:" + userID
	now := time.Now()
	nowMs := float64(now.UnixMilli())
	windowStart := float64(now.Add(-reportRateWindow).UnixMilli())
	member := fmt.Sprintf("%d:%d", now.UnixNano(), now.UnixNano()%100000)

	ctx, cancel := context.WithTimeout(c.Context(), 100*time.Millisecond)
	defer cancel()

	pipe := rdb.Pipeline()
	pipe.ZAdd(ctx, key, goredis.Z{Score: nowMs, Member: member})
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%.0f", windowStart))
	countCmd := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, reportRateWindow)

	if _, err := pipe.Exec(ctx); err != nil {
		return nil
	}
	count, err := countCmd.Result()
	if err != nil {
		return nil
	}
	if count > reportRateMax {
		c.Set("Retry-After", fmt.Sprintf("%d", int(reportRateWindow.Seconds())))
		return response.WriteError(c, fiber.StatusTooManyRequests, "rate_limited", "too many reports — slow down")
	}
	return nil
}

// reportTargetExists reports whether a media row of the given type/id exists.
// This backs the 404 advertised by the report OpenAPI routes.
func reportTargetExists(db *gorm.DB, mediaType string, mediaID int64) (bool, error) {
	var model interface{}
	switch mediaType {
	case "movie":
		model = &database.Movie{}
	case "show":
		model = &database.Show{}
	case "season":
		model = &database.Season{}
	case "episode":
		model = &database.Episode{}
	default:
		return false, nil
	}
	var count int64
	if err := db.Model(model).Where("id = ?", mediaID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
