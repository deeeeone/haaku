package users

import (
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// This file is the contract-shaped half of the public-profile read surface
// (issue #103): the paths Trakt publishes for a profile header, per-type
// history/ratings/watched, favorites, watchlist, activities, settings, saved
// filters, comment reactions, blocked users and the report write.
//
// Every target-profile read runs the same auth.PrivacyFilter/PrivacyFilterSection
// the already-landed /users/{id} reads use, so a stranger on a private profile
// gets 403 rather than a 200 with an empty body. The caller-owned endpoints
// (/users/blocked, /users/settings, /users/saved_filters, /users/reactions)
// require authentication: there is no target to filter.
//
// Nothing here invents a payload. Each handler reads the tables the API already
// writes (user_history, user_favorites, user_watchlist, user_ratings, comments,
// comment_reactions, user_blocks, saved_filters, user_reports, user_follows).

// contractExtendedValues are the documented Trakt extended groups. The vendored
// contract types the parameter loosely; accepting exactly these and rejecting
// anything else keeps a client typo from being silently ignored.
var contractExtendedValues = map[string]bool{
	"full": true, "images": true, "metadata": true,
	"guest_ratings": true, "comments": true, "min": true,
}

// validateExtended rejects an unknown extended group.
func validateExtended(c *fiber.Ctx) error {
	raw := strings.TrimSpace(c.Query("extended"))
	if raw == "" {
		return nil
	}
	for _, part := range strings.Split(raw, ",") {
		if !contractExtendedValues[strings.TrimSpace(part)] {
			return response.BadRequest(c, "invalid extended value")
		}
	}
	return nil
}

// contractPage parses page/limit with Trakt's defaults (page 1, limit 10). The
// contract caps a supplied limit at the endpoint maximum ("often 250") and
// clamps rather than rejects, so values above 250 are clamped to 250.
func contractPage(c *fiber.Ctx) (page, limit int) {
	page, limit = 1, 10
	if p := c.Query("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = response.ClampLimit(parsed, 1, 250)
		}
	}
	return page, limit
}

// writeContractPage writes a bare contract array with the X-Pagination-* envelope.
func writeContractPage(c *fiber.Ctx, items any, page, limit int, total int64, count int) error {
	response.SetPaginationHeaders(c, page, limit, total, count)
	return c.JSON(items)
}

// contractMediaRow renders a media row in the contract's object shape. ok is
// false when the row a user row points at no longer exists; the caller omits the
// dangling item rather than fabricating a placeholder.
func contractMediaRow(db *gorm.DB, mediaType string, mediaID int64) (map[string]any, bool) {
	switch mediaType {
	case "movie":
		var m database.Movie
		if db.First(&m, mediaID).Error != nil {
			return nil, false
		}
		return contractshape.Movie(&m), true
	case "show":
		var s database.Show
		if db.First(&s, mediaID).Error != nil {
			return nil, false
		}
		return contractshape.Show(&s), true
	case "episode":
		var e database.Episode
		if db.First(&e, mediaID).Error != nil {
			return nil, false
		}
		return contractshape.Episode(&e, seasonNumber(&e)), true
	case "season":
		var s database.Season
		if db.First(&s, mediaID).Error != nil {
			return nil, false
		}
		return contractshape.Season(&s), true
	default:
		return nil, false
	}
}

// seasonNumber returns the parent season's number for an episode, or 0 when the
// season row is gone (contractshape.Episode emits 0 rather than guessing).
func seasonNumber(e *database.Episode) int {
	db, err := database.GetDB()
	if err != nil {
		return 0
	}
	var season database.Season
	if db.First(&season, e.SeasonID).Error != nil {
		return 0
	}
	return season.SeasonNumber
}

// contractHistoryRow renders one user_history row in the contract's history
// shape: id, watched_at, action, type, plus the type's media object.
func contractHistoryRow(db *gorm.DB, h *database.UserHistory) (map[string]any, bool) {
	mediaType := h.MediaType
	mediaID := h.MediaID
	if mediaType == "episode" && h.EpisodeID != nil {
		mediaID = *h.EpisodeID
	}
	row := map[string]any{
		"id":         h.ID,
		"watched_at": contractshape.Timestamp(h.WatchedAt),
		"action":     syncHistoryAction(h.Source),
		"type":       mediaType,
	}
	media, ok := contractMediaRow(db, mediaType, mediaID)
	if !ok {
		return nil, false
	}
	row[mediaType] = media
	return row, true
}

// syncHistoryAction mirrors sync.HistoryAction without importing the sync
// handler package into this file's callers twice.
func syncHistoryAction(source string) string {
	switch source {
	case "scrobble":
		return "scrobble"
	case "checkin":
		return "checkin"
	default:
		return "watch"
	}
}

// contractComment renders a stored comment in the contract's comment shape.
func contractComment(db *gorm.DB, cm *database.Comment) (map[string]any, bool) {
	owner, err := contractshape.UserByIDBatch(db, []string{cm.UserID})
	if err != nil {
		return nil, false
	}
	var playCount int64
	if cm.MediaType != "" && cm.MediaID != nil {
		db.Model(&database.UserHistory{}).
			Where("user_id = ? AND media_type = ? AND media_id = ?", cm.UserID, cm.MediaType, *cm.MediaID).
			Count(&playCount)
	}
	review := cm.CommentType == "review"
	return map[string]any{
		"id":         cm.ID,
		"parent_id":  cm.ParentID,
		"created_at": contractshape.Timestamp(cm.CreatedAt),
		"updated_at": contractshape.Timestamp(cm.UpdatedAt),
		"comment":    cm.Body,
		"spoiler":    cm.Spoiler,
		"review":     review,
		"replies":    cm.ReplyCount,
		"likes":      cm.LikeCount,
		"language":   nil,
		"user_stats": map[string]any{
			"rating":          nil,
			"play_count":      playCount,
			"completed_count": playCount,
		},
		"user": contractshape.User(owner[cm.UserID]),
	}, true
}

// contractProfileFields adds the contract's profile-header properties to the
// existing public profile response. Everything is derived from the user row;
// vip/director/gender have no column and are emitted as null, which the
// contract's optional nullable properties allow.
type contractProfileFields struct {
	Private    bool           `json:"private"`
	Deleted    bool           `json:"deleted"`
	IDs        map[string]any `json:"ids"`
	JoinedAt   any            `json:"joined_at"`
	About      any            `json:"about"`
	Vip        any            `json:"vip"`
	VipEp      any            `json:"vip_ep"`
	Director   any            `json:"director"`
	Gender     any            `json:"gender"`
	Age        any            `json:"age"`
	Images     map[string]any `json:"images"`
	VipOg      any            `json:"vip_og"`
	VipYears   any            `json:"vip_years"`
	VipCover   any            `json:"vip_cover_image"`
	VipVetSinc any            `json:"vip_veteran_since"`
	VipVetYrs  any            `json:"vip_veteran_years"`
	VipVetTier any            `json:"vip_veteran_tier"`
	VipVetTit  any            `json:"vip_veteran_title"`
	VipGrace   any            `json:"vip_grace_ends_at"`
}

func contractProfileFieldsFor(u *database.User) contractProfileFields {
	avatar := map[string]any{"avatar": map[string]any{"full": u.AvatarURL}}
	return contractProfileFields{
		Private:  u.IsPrivate,
		Deleted:  false,
		IDs:      map[string]any{"slug": contractshape.NullableString(u.Username), "trakt": contractshape.LocalUserNum(u.ID)},
		JoinedAt: contractshape.Timestamp(u.CreatedAt),
		About:    contractshape.NullableString(u.Bio),
		Images:   avatar,
	}
}

// ---------------------------------------------------------------------------
// Routes
// ---------------------------------------------------------------------------

// registerAccountContractRoutes mounts the caller-owned contract endpoints.
// It must run before users.Get("/:id") so /users/blocked and /users/settings
// are not captured as a user id.
func registerAccountContractRoutes(users fiber.Router) {
	users.Get("/blocked", auth.AuthRequired(), handleBlockedUsers)
	users.Get("/reactions/comments", auth.AuthRequired(), handleMyCommentReactions)
	users.Get("/saved_filters/:section", auth.AuthRequired(), handleContractSavedFilters)
	users.Put("/settings", auth.AuthRequired(), handlePutContractSettings)
	users.Post("/:id/report", auth.OptionalAuth(), auth.PrivacyFilter("id"), handleReportUser)
}

func init() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/users/blocked", Summary: "Users the caller has blocked (contract path)", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{200: "Blocked users", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/users/reactions/comments", Summary: "The caller's comment reactions (contract path)", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{200: "Reactions", 401: "Unauthorized", 400: "Invalid extended value"}},
		{Method: "GET", Path: "/v1/users/saved_filters/{section}", Summary: "The caller's saved filters for a section (contract path)", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{200: "Saved filters", 400: "Invalid section", 401: "Unauthorized"}},
		{Method: "PUT", Path: "/v1/users/settings", Summary: "Update the caller's settings (contract path)", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{200: "Updated settings", 400: "Invalid request", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/users/{id}/report", Summary: "Report a user for moderator review (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{201: "Report filed", 400: "Invalid reason", 403: "Private profile", 404: "Not found", 401: "Unauthorized"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// ---------------------------------------------------------------------------
// Blocked users
// ---------------------------------------------------------------------------

// handleBlockedUsers serves GET /v1/users/blocked for the caller.
func handleBlockedUsers(c *fiber.Ctx) error {
	caller, _ := c.Locals("user_id").(string)
	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	page, limit := contractPage(c)
	query := db.Where("blocker_id = ?", caller)
	var total int64
	if err := query.Model(&database.UserBlock{}).Count(&total).Error; err != nil {
		return response.InternalError(c)
	}

	var blocks []database.UserBlock
	if err := query.Order("created_at DESC, blocked_id").Offset(pageOffset(page, limit)).Limit(limit).Find(&blocks).Error; err != nil {
		return response.InternalError(c)
	}

	ids := make([]string, 0, len(blocks))
	for _, b := range blocks {
		ids = append(ids, b.BlockedID)
	}
	usersByID, err := contractshape.UserByIDBatch(db, ids)
	if err != nil {
		return response.InternalError(c)
	}

	items := make([]map[string]any, 0, len(blocks))
	for _, b := range blocks {
		u, found := usersByID[b.BlockedID]
		if !found {
			continue // dangling block row — nothing real to show
		}
		items = append(items, map[string]any{
			"blocked_at": contractshape.Timestamp(b.CreatedAt),
			"user":       contractshape.User(u),
		})
	}
	return writeContractPage(c, items, page, limit, total, len(items))
}

// ---------------------------------------------------------------------------
// Comment reactions
// ---------------------------------------------------------------------------

// handleMyCommentReactions serves GET /v1/users/reactions/comments.
func handleMyCommentReactions(c *fiber.Ctx) error {
	if err := validateExtended(c); err != nil {
		return err
	}
	caller, _ := c.Locals("user_id").(string)
	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	page, limit := contractPage(c)
	query := db.Where("user_id = ?", caller)
	var total int64
	if err := query.Model(&database.CommentReaction{}).Count(&total).Error; err != nil {
		return response.InternalError(c)
	}

	var reactions []database.CommentReaction
	if err := query.Order("created_at DESC, comment_id").Offset(pageOffset(page, limit)).Limit(limit).Find(&reactions).Error; err != nil {
		return response.InternalError(c)
	}

	commentIDs := make([]int64, 0, len(reactions))
	for _, r := range reactions {
		commentIDs = append(commentIDs, r.CommentID)
	}
	commentsByID := map[int64]database.Comment{}
	if len(commentIDs) > 0 {
		var rows []database.Comment
		if err := db.Where("id IN ?", commentIDs).Find(&rows).Error; err != nil {
			return response.InternalError(c)
		}
		for i := range rows {
			commentsByID[rows[i].ID] = rows[i]
		}
	}

	items := make([]map[string]any, 0, len(reactions))
	for _, r := range reactions {
		cm, found := commentsByID[r.CommentID]
		if !found {
			continue
		}
		comment, ok := contractComment(db, &cm)
		if !ok {
			continue
		}
		items = append(items, map[string]any{
			"reacted_at": contractshape.Timestamp(r.CreatedAt),
			"reaction":   map[string]any{"type": r.ReactionType},
			"type":       "comment",
			"comment":    comment,
		})
	}
	return writeContractPage(c, items, page, limit, total, len(items))
}

// ---------------------------------------------------------------------------
// Saved filters
// ---------------------------------------------------------------------------

var contractFilterSections = map[string]bool{
	"movies": true, "shows": true, "calendars": true, "search": true,
}

// handleContractSavedFilters serves GET /v1/users/saved_filters/{section}.
//
// Our saved_filters row stores a free-form filter map. The contract's path and
// query fields are derived from that map (path when present, otherwise the
// remaining keys encoded as a query string), never invented.
func handleContractSavedFilters(c *fiber.Ctx) error {
	section := c.Params("section")
	if !contractFilterSections[section] {
		return response.BadRequest(c, "section must be one of movies, shows, calendars, search")
	}
	caller, _ := c.Locals("user_id").(string)
	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	page, limit := contractPage(c)
	query := db.Where("user_id = ? AND section = ?", caller, section)
	var total int64
	if err := query.Model(&database.SavedFilter{}).Count(&total).Error; err != nil {
		return response.InternalError(c)
	}

	var rows []database.SavedFilter
	if err := query.Order("created_at ASC, id ASC").Offset(pageOffset(page, limit)).Limit(limit).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	items := make([]map[string]any, 0, len(rows))
	for i := range rows {
		path, queryString := savedFilterPathQuery(rows[i].Filter)
		items = append(items, map[string]any{
			"rank":       i + 1,
			"id":         rows[i].ID,
			"name":       rows[i].Name,
			"path":       path,
			"query":      queryString,
			"updated_at": contractshape.Timestamp(rows[i].CreatedAt),
			"section":    rows[i].Section,
		})
	}
	return writeContractPage(c, items, page, limit, total, len(items))
}

// savedFilterPathQuery derives the contract's path/query pair from the stored
// filter map. A "path" key is used verbatim; every other key becomes an encoded
// query parameter in sorted order, so the result is reproducible.
func savedFilterPathQuery(filter database.JSONMap) (string, string) {
	path, _ := filter["path"].(string)
	values := url.Values{}
	for _, k := range sortedKeys(filter) {
		if k == "path" || k == "query" {
			continue
		}
		switch v := filter[k].(type) {
		case string:
			values.Set(k, v)
		case float64:
			values.Set(k, strconv.FormatFloat(v, 'f', -1, 64))
		case bool:
			values.Set(k, strconv.FormatBool(v))
		}
	}
	if q, ok := filter["query"].(string); ok && q != "" {
		return path, q
	}
	return path, values.Encode()
}

func sortedKeys(m database.JSONMap) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// Settings (PUT)
// ---------------------------------------------------------------------------

// contractSettingsBody is the subset of the contract's settings body haaku can
// store: the user profile block and the free-form browsing/sharing settings
// blob. Unknown top-level keys are preserved in users.settings so a client that
// round-trips the GET response does not lose them.
type contractSettingsBody struct {
	User *struct {
		Name     *string `json:"name"`
		About    *string `json:"about"`
		Location *string `json:"location"`
		Private  *bool   `json:"private"`
	} `json:"user"`
	Account  map[string]any `json:"account"`
	Browsing map[string]any `json:"browsing"`
	Sharing  map[string]any `json:"sharing"`
	Limits   map[string]any `json:"limits"`
}

// handlePutContractSettings serves PUT /v1/users/settings.
func handlePutContractSettings(c *fiber.Ctx) error {
	caller, _ := c.Locals("user_id").(string)
	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	var body contractSettingsBody
	if err := json.Unmarshal(c.Body(), &body); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	var user database.User
	if err := db.First(&user, "id = ?", caller).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "user not found")
		}
		return response.InternalError(c)
	}

	if body.User != nil {
		if body.User.Name != nil {
			user.Name = *body.User.Name
		}
		if body.User.About != nil {
			user.Bio = *body.User.About
		}
		if body.User.Location != nil {
			user.Location = *body.User.Location
		}
		if body.User.Private != nil {
			user.IsPrivate = *body.User.Private
		}
	}

	settings := map[string]any{}
	if user.Settings != "" && user.Settings != "null" {
		_ = json.Unmarshal([]byte(user.Settings), &settings)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	if body.Account != nil {
		settings["account"] = mergeMaps(settings["account"], body.Account)
	}
	if body.Browsing != nil {
		settings["browsing"] = mergeMaps(settings["browsing"], body.Browsing)
	}
	if body.Sharing != nil {
		settings["sharing"] = mergeMaps(settings["sharing"], body.Sharing)
	}
	if body.Limits != nil {
		settings["limits"] = mergeMaps(settings["limits"], body.Limits)
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return response.InternalError(c)
	}
	user.Settings = string(encoded)

	if err := db.Save(&user).Error; err != nil {
		return response.InternalError(c)
	}

	// Answer with the same envelope GET /users/settings returns, so the update
	// can be verified by the client.
	return handleContractSettings(c)
}

func mergeMaps(existing any, incoming map[string]any) map[string]any {
	out := map[string]any{}
	if m, ok := existing.(map[string]any); ok {
		for k, v := range m {
			out[k] = v
		}
	}
	for k, v := range incoming {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Report (POST)
// ---------------------------------------------------------------------------

var contractReportReasons = map[string]bool{
	"spam": true, "adult": true, "language": true, "other": true,
}

// handleReportUser serves POST /v1/users/{id}/report.
func handleReportUser(c *fiber.Ctx) error {
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}
	caller, _ := c.Locals("user_id").(string)
	if caller == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	if caller == target.ID {
		return response.BadRequest(c, "you cannot report yourself")
	}

	var body struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(c.Body(), &body); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}
	if !contractReportReasons[body.Reason] {
		return response.BadRequest(c, "reason must be one of spam, adult, language, other")
	}

	report := database.UserReport{
		ReporterID:     caller,
		ReportedUserID: target.ID,
		Reason:         body.Reason,
		Message:        body.Message,
	}
	// One row per reporter per target; re-reporting updates the reason.
	var existing database.UserReport
	err := db.Where("reporter_id = ? AND reported_user_id = ?", caller, target.ID).First(&existing).Error
	switch {
	case err == nil:
		existing.Reason = report.Reason
		existing.Message = report.Message
		if err := db.Save(&existing).Error; err != nil {
			return response.InternalError(c)
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err := db.Create(&report).Error; err != nil {
			return response.InternalError(c)
		}
	default:
		return response.InternalError(c)
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{})
}
