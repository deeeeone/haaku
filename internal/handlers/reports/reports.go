package reports

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	projredis "github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/gorm"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "POST", Path: "/v1/movies/{id}/report", Summary: "Report a movie", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 200: "Previously dismissed report reopened", 400: "Invalid request or duplicate", 401: "Unauthorized", 404: "Movie not found"}},
		{Method: "POST", Path: "/v1/shows/{id}/report", Summary: "Report a show", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 200: "Previously dismissed report reopened", 400: "Invalid request or duplicate", 401: "Unauthorized", 404: "Show not found"}},
		{Method: "POST", Path: "/v1/seasons/{id}/report", Summary: "Report a season", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 200: "Previously dismissed report reopened", 400: "Invalid request or duplicate", 401: "Unauthorized", 404: "Season not found"}},
		{Method: "POST", Path: "/v1/episodes/{id}/report", Summary: "Report an episode", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 200: "Previously dismissed report reopened", 400: "Invalid request or duplicate", 401: "Unauthorized", 404: "Episode not found"}},
		{Method: "GET", Path: "/v1/reports", Summary: "List content reports for moderation (moderator/admin only)", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{200: "Reports", 400: "Invalid filter", 401: "Unauthorized", 403: "Moderator access required"}},
		{Method: "POST", Path: "/v1/reports/{id}/resolve", Summary: "Resolve a content report as actioned (moderator/admin only)", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{200: "Resolved", 400: "Invalid request", 401: "Unauthorized", 403: "Moderator access required", 404: "Report not found", 409: "Report already actioned"}},
		{Method: "POST", Path: "/v1/reports/{id}/dismiss", Summary: "Dismiss a content report as needing no action (moderator/admin only)", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{200: "Dismissed", 400: "Invalid request", 401: "Unauthorized", 403: "Moderator access required", 404: "Report not found", 409: "Report already actioned"}},
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

	// Moderation queue (issue #10 §2). Behind AuthRequired + RequireModerator.
	// Static "/reports" is registered at depth 2 where no parameterised route
	// exists, so there is nothing for it to shadow.
	api.Get("/reports", auth.AuthRequired(), auth.RequireModerator(), handleListReports)
	api.Post("/reports/:id/resolve", auth.AuthRequired(), auth.RequireModerator(), handleResolveReport)
	api.Post("/reports/:id/dismiss", auth.AuthRequired(), auth.RequireModerator(), handleDismissReport)
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

// Report lifecycle states (issue #10 §2). A report is "open" until a moderator
// moves it to a terminal state: "resolved" (action taken) or "dismissed" (no
// action needed). Only "open" reports await moderation.
const (
	reportStatusOpen      = "open"
	reportStatusResolved  = "resolved"
	reportStatusDismissed = "dismissed"
)

var validReportStatuses = map[string]bool{
	reportStatusOpen:      true,
	reportStatusResolved:  true,
	reportStatusDismissed: true,
}

// Target media types a report may point at — the same set the submit routes
// accept. Serves as the allowlist for the ?target_type= list filter.
var validReportTargetTypes = map[string]bool{
	"movie":   true,
	"show":    true,
	"season":  true,
	"episode": true,
}

// maxReportDescription caps free-form report detail. The schema column is
// unbounded text; this cap keeps moderation notes reasonable and bounded.
const maxReportDescription = 1000

// maxResolutionNote caps the free-form note a moderator may attach when
// resolving or dismissing a report.
const maxResolutionNote = 1000

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

// resolveRequest is the optional body of POST /v1/reports/:id/{resolve,dismiss}.
type resolveRequest struct {
	Note string `json:"note,omitempty"`
}

type reportResponse struct {
	ID             int64  `json:"id"`
	ReporterID     string `json:"reporter_id"`
	MediaType      string `json:"media_type"`
	MediaID        int64  `json:"media_id"`
	Reason         string `json:"reason"`
	Description    string `json:"description"`
	Status         string `json:"status"`
	ResolutionNote string `json:"resolution_note,omitempty"`
	ResolvedBy     string `json:"resolved_by,omitempty"`
	ResolvedAt     string `json:"resolved_at,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func toReportResponse(r *database.ContentReport) reportResponse {
	resp := reportResponse{
		ID:             r.ID,
		ReporterID:     r.ReporterID,
		MediaType:      r.MediaType,
		MediaID:        r.MediaID,
		Reason:         r.Reason,
		Description:    r.Description,
		Status:         r.Status,
		ResolutionNote: r.ResolutionNote,
		ResolvedBy:     r.ResolvedBy,
		CreatedAt:      response.FormatTime(r.CreatedAt),
		UpdatedAt:      response.FormatTime(r.UpdatedAt),
	}
	if r.Status == "" {
		resp.Status = reportStatusOpen
	}
	if r.ResolvedAt != nil {
		resp.ResolvedAt = response.FormatTime(*r.ResolvedAt)
	}
	return resp
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
	if err := response.ParseBody(c, &req); err != nil {
		return err
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

	// One report per (reporter, media) is enforced by a unique index (0029).
	// Re-reporting behaves thus:
	//   - open or resolved report → 400: already on file / already handled;
	//   - dismissed report → reopen (200): a dismissal means "no action needed",
	//     not "this user may never flag the content again".
	var existing database.ContentReport
	result := db.Where("reporter_id = ? AND media_type = ? AND media_id = ?", userID, mediaType, id).
		First(&existing)
	switch {
	case result.Error == nil && existing.Status == reportStatusDismissed:
		now := time.Now().UTC()
		updates := map[string]interface{}{
			"reason":          reason,
			"description":     description,
			"status":          reportStatusOpen,
			"resolution_note": "",
			"resolved_by":     "",
			"resolved_at":     nil,
			"updated_at":      now,
		}
		if err := db.Model(&existing).Updates(updates).Error; err != nil {
			return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to reopen report")
		}
		if err := db.First(&existing, existing.ID).Error; err != nil {
			return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
		}
		return c.Status(fiber.StatusOK).JSON(toReportResponse(&existing))
	case result.Error == nil:
		return response.BadRequest(c, "you have already reported this content")
	case !errors.Is(result.Error, gorm.ErrRecordNotFound):
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	report := database.ContentReport{
		ReporterID:  userID,
		MediaType:   mediaType,
		MediaID:     id,
		Reason:      reason,
		Description: description,
		Status:      reportStatusOpen,
	}

	if err := db.Create(&report).Error; err != nil {
		// The check above is check-then-insert and racy; a concurrent identical
		// report trips the 0029 unique index. Re-check so that race answers 400
		// ("already reported") rather than a misleading 500.
		var dup database.ContentReport
		if db.Where("reporter_id = ? AND media_type = ? AND media_id = ?", userID, mediaType, id).First(&dup).Error == nil {
			return response.BadRequest(c, "you have already reported this content")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to create report")
	}

	return c.Status(fiber.StatusCreated).JSON(toReportResponse(&report))
}

// ---------------------------------------------------------------------------
// Moderation handlers (issue #10 §2)
// ---------------------------------------------------------------------------

// GET /v1/reports — the moderation queue. Filter by ?status= (open|resolved|
// dismissed) and ?target_type= (movie|show|season|episode); paginated.
func handleListReports(c *fiber.Ctx) error {
	status := strings.ToLower(strings.TrimSpace(c.Query("status")))
	if status != "" && !validReportStatuses[status] {
		return response.BadRequest(c, "invalid status: must be one of open, resolved, dismissed")
	}
	targetType := strings.ToLower(strings.TrimSpace(c.Query("target_type")))
	if targetType != "" && !validReportTargetTypes[targetType] {
		return response.BadRequest(c, "invalid target_type: must be one of movie, show, season, episode")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	page, limit := response.ParsePagination(c, 25)

	query := db.Model(&database.ContentReport{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if targetType != "" {
		query = query.Where("media_type = ?", targetType)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	var rows []database.ContentReport
	if err := query.Order("created_at DESC, id DESC").
		Limit(limit).Offset((page - 1) * limit).
		Find(&rows).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	out := make([]reportResponse, 0, len(rows))
	for i := range rows {
		out = append(out, toReportResponse(&rows[i]))
	}
	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}

// POST /v1/reports/:id/resolve
func handleResolveReport(c *fiber.Ctx) error {
	return handleActOnReport(c, reportStatusResolved)
}

// POST /v1/reports/:id/dismiss
func handleDismissReport(c *fiber.Ctx) error {
	return handleActOnReport(c, reportStatusDismissed)
}

// handleActOnReport moves an open report to a terminal state, recording the
// acting operator, an optional note, and the time. All three are the audit
// trail the issue asks for. Only "open" reports can be actioned — a second
// action on a terminal report is a 409, not a silent no-op.
func handleActOnReport(c *fiber.Ctx, newStatus string) error {
	operatorID := c.Locals("user_id").(string)

	id, err := response.ParseIDParam(c, "report")
	if err != nil {
		return err
	}

	var req resolveRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&req); err != nil {
			return response.BadRequest(c, "invalid request body")
		}
	}
	note := strings.TrimSpace(req.Note)
	if len(note) > maxResolutionNote {
		return response.BadRequest(c, fmt.Sprintf("note exceeds maximum length of %d characters", maxResolutionNote))
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var report database.ContentReport
	if err := db.First(&report, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "report not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}
	if !report.IsOpen() {
		return response.WriteError(c, fiber.StatusConflict, "conflict", "report has already been "+report.Status)
	}

	now := time.Now().UTC()
	if err := db.Model(&report).Updates(map[string]interface{}{
		"status":          newStatus,
		"resolution_note": note,
		"resolved_by":     operatorID,
		"resolved_at":     now,
		"updated_at":      now,
	}).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to update report")
	}

	if err := db.First(&report, id).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}
	return c.JSON(toReportResponse(&report))
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
	window, err := projredis.SlidingWindow(
		projredis.RateLimitKey("report", userID),
		reportRateWindow,
		reportRateMax,
		time.Now(),
	)
	// err != nil means Redis is unavailable. Fail OPEN with no further branch:
	// an unavailable limiter must never be what blocks a legitimate report, and
	// a nil return from this helper is what says "allowed through"
	// (internal/ratelimit/ratelimit.md documents the same rule for the request
	// limiter).
	if err == nil && window.Count > reportRateMax {
		c.Set("Retry-After", fmt.Sprintf("%d", int(window.Reset.Seconds())))
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
