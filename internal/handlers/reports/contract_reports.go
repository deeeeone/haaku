package reports

import (
	"errors"
	"strconv"
	"strings"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped report submissions (issue #68).
//
// Trakt nests reports under the show/season/episode and has a person report;
// our flat routes are /v1/{movies,shows,seasons,episodes}/{id}/report. These
// routes resolve the contract's path segments to our row ids and write the
// report the same way the flat handlers do — same ContentReport row, same
// per-reporter uniqueness (409 on a duplicate, which is the contract's status
// for it), same auth requirement.
//
// The body is the contract's {reason, message}: reason is validated against the
// contract's own enum (which differs from our flat routes'), and message maps
// to the stored description.

func init() {
	openapi.Register(openapi.RouteDef{Method: "POST", Path: "/v1/people/{id}/report", Summary: "Report a person (contract path)", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 400: "Invalid request", 401: "Unauthorized", 404: "Person not found", 409: "Already reported", 429: "Rate limited"}})
	openapi.Register(openapi.RouteDef{Method: "POST", Path: "/v1/shows/{id}/seasons/{season}/report", Summary: "Report a season (contract path)", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 400: "Invalid request", 401: "Unauthorized", 404: "Show or season not found", 409: "Already reported", 429: "Rate limited"}})
	openapi.Register(openapi.RouteDef{Method: "POST", Path: "/v1/shows/{id}/seasons/{season}/episodes/{episode}/report", Summary: "Report an episode (contract path)", Tags: []string{"Reports"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 400: "Invalid request", 401: "Unauthorized", 404: "Show, season or episode not found", 409: "Already reported", 429: "Rate limited"}})
}

// registerContractReportRoutes mounts the contract-shaped report submissions.
func registerContractReportRoutes(api fiber.Router) {
	api.Post("/people/:id/report", auth.AuthRequired(), handleContractPersonReport)
	api.Post("/shows/:id/seasons/:season/report", auth.AuthRequired(), handleContractSeasonReport)
	api.Post("/shows/:id/seasons/:season/episodes/:episode/report", auth.AuthRequired(), handleContractEpisodeReport)
}

// contractReportRequest is the contract's report body.
type contractReportRequest struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

var contractPersonReasons = map[string]bool{
	"duplicate": true, "remove": true, "data_refresh": true, "metadata": true,
	"adult": true, "language": true, "spam": true, "tmdb": true, "other": true,
}

var contractSeasonEpisodeReasons = map[string]bool{
	"duplicate": true, "remove": true, "data_refresh": true, "metadata": true,
	"adult": true, "runtime": true, "language": true, "spam": true, "tmdb": true, "other": true,
}

func handleContractPersonReport(c *fiber.Ctx) error {
	_, personID, err := media.ResolveIDAs(c, c.Params("id"), "person")
	if err != nil {
		return err
	}
	return submitContractReport(c, "person", personID, contractPersonReasons)
}

func handleContractSeasonReport(c *fiber.Ctx) error {
	db, season, err := resolveContractSeason(c)
	if err != nil {
		return err
	}
	_ = db
	return submitContractReport(c, "season", season.ID, contractSeasonEpisodeReasons)
}

func handleContractEpisodeReport(c *fiber.Ctx) error {
	db, episode, err := resolveContractEpisode(c)
	if err != nil {
		return err
	}
	_ = db
	return submitContractReport(c, "episode", episode.ID, contractSeasonEpisodeReasons)
}

// submitContractReport validates the contract body and writes one ContentReport
// row, answering 201 {} on create, 400 on a bad reason, and 409 when this
// reporter already reported this item.
func submitContractReport(c *fiber.Ctx, mediaType string, mediaID int64, reasons map[string]bool) error {
	userID, _ := c.Locals("user_id").(string)

	var req contractReportRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}
	reason := strings.ToLower(strings.TrimSpace(req.Reason))
	if !reasons[reason] {
		return response.BadRequest(c, "invalid reason")
	}
	message := strings.TrimSpace(req.Message)
	if len(message) > maxReportDescription {
		return response.BadRequest(c, "message exceeds maximum length")
	}

	// The report-specific per-user window applies here exactly as it does on the
	// legacy flat routes; without it a caller could file unlimited contract
	// reports against distinct targets and flood the moderation queue.
	if err := reportRateCheck(c, userID); err != nil {
		return err
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	exists, verr := reportTargetExists(db, mediaType, mediaID)
	if verr != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}
	if !exists {
		return response.NotFound(c, mediaType+" not found")
	}

	report := database.ContentReport{
		ReporterID:  userID,
		MediaType:   mediaType,
		MediaID:     mediaID,
		Reason:      reason,
		Description: message,
		Status:      reportStatusOpen,
	}
	// Pre-check for an existing report: the production unique index
	// (reporter_id, media_type, media_id) is not present in the sqlite test
	// schema, so the duplicate case is decided here and the insert's unique
	// violation is the (racy) backstop below.
	var existing database.ContentReport
	if err := db.Where("reporter_id = ? AND media_type = ? AND media_id = ?", userID, mediaType, mediaID).
		First(&existing).Error; err == nil {
		return response.WriteError(c, fiber.StatusConflict, "conflict", "you have already reported this content")
	}
	if err := db.Create(&report).Error; err != nil {
		// The unique index (reporter_id, media_type, media_id) is the
		// duplicate signal; the contract answers 409 for it.
		var dup database.ContentReport
		if db.Where("reporter_id = ? AND media_type = ? AND media_id = ?", userID, mediaType, mediaID).
			First(&dup).Error == nil {
			return response.WriteError(c, fiber.StatusConflict, "conflict", "you have already reported this content")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to create report")
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{})
}

// resolveContractSeason resolves /shows/{id}/seasons/{season}/report to our
// season row.
func resolveContractSeason(c *fiber.Ctx) (*gorm.DB, *database.Season, error) {
	_, showID, err := media.ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return nil, nil, err
	}
	seasonNum, aerr := strconv.Atoi(c.Params("season"))
	if aerr != nil || seasonNum < 0 {
		_ = response.BadRequest(c, "invalid season number")
		return nil, nil, response.ErrHandled
	}
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return nil, nil, response.InternalError(c)
	}
	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", showID, seasonNum).First(&season).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, response.NotFound(c, "season not found")
		}
		return nil, nil, response.InternalError(c)
	}
	return db, &season, nil
}

// resolveContractEpisode resolves .../episodes/{episode}/report to our episode
// row.
func resolveContractEpisode(c *fiber.Ctx) (*gorm.DB, *database.Episode, error) {
	db, season, err := resolveContractSeason(c)
	if err != nil {
		return nil, nil, err
	}
	epNum, aerr := strconv.Atoi(c.Params("episode"))
	if aerr != nil || epNum < 0 {
		_ = response.BadRequest(c, "invalid episode number")
		return nil, nil, response.ErrHandled
	}
	var episode database.Episode
	if err := db.Where("season_id = ? AND episode_number = ?", season.ID, epNum).First(&episode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, response.NotFound(c, "episode not found")
		}
		return nil, nil, response.InternalError(c)
	}
	return db, &episode, nil
}
