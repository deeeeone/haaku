package reports

import (
	"errors"
	"strconv"
	"strings"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
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
