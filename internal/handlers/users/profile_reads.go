package users

import (
	"errors"
	"time"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/comments"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/handlers/notes"
	"github.com/haaku-tv/haaku/internal/handlers/sync"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// This file is the public-profile READ surface (issue #4, Users section): the
// "view another user's profile" endpoints that expose data the schema already
// stores. Everything here is read-only.
//
// Every route keys off the :id path param and runs the shared
// auth.PrivacyFilter("id"), so a block in either direction and an unknown user
// are 404, a private profile is 403 to a stranger and 200 to self/admins/
// followers — identical to GET /v1/users/:id. On top of that every handler
// re-checks the account status (deleted / pending_deletion → 404), which
// PrivacyFilter does not look at, by reusing profileTarget() — the same gate
// handleGetUser uses.
//
// Response shapes are the ones the caller's own /v1/sync/... and
// /v1/comments/... routes already return (exported aliases in those packages),
// so a client that knows /v1/sync/history knows /v1/users/{id}/history.

func init() {
	registerProfileReadOpenAPIRoutes()
}

func registerProfileReadOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		// Own likes (static "me" paths — they must precede /users/:id/likes/*).
		{Method: "GET", Path: "/v1/users/me/likes/comments", Summary: "Comments the caller has liked", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{200: "Liked comments", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/users/me/likes/lists", Summary: "Lists the caller has liked", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{200: "Liked lists", 401: "Unauthorized"}},

		// History
		{Method: "GET", Path: "/v1/users/{id}/history", Summary: "A user's watch history (all media types)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "History items", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/history/{media_type}", Summary: "A user's watch history filtered by media type (movies|shows|seasons|episodes|all)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "History items", 400: "Invalid media type", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/history/{media_type}/{media_id}", Summary: "A user's watch history for one item (?id_type= resolves an external id)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "History items", 400: "Invalid media type", 403: "Private profile", 404: "Not found"}},

		// Ratings
		{Method: "GET", Path: "/v1/users/{id}/ratings", Summary: "A user's ratings (all media types)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Rating items", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/ratings/{media_type}", Summary: "A user's ratings filtered by media type", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Rating items", 400: "Invalid media type", 403: "Private profile", 404: "Not found"}},

		// Watchlist
		{Method: "GET", Path: "/v1/users/{id}/watchlist", Summary: "A user's watchlist (?sort_by=rank|listed|added|title, ?sort_how=asc|desc)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Watchlist items", 400: "Invalid sort", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/watchlist/{media_type}", Summary: "A user's watchlist filtered by media type", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Watchlist items", 400: "Invalid media type", 403: "Private profile", 404: "Not found"}},

		// Collection
		{Method: "GET", Path: "/v1/users/{id}/collection", Summary: "A user's collection (all media types)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Collection items", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/collection/{media_type}", Summary: "A user's collection filtered by media type", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Collection items", 400: "Invalid media type", 403: "Private profile", 404: "Not found"}},

		// Watching
		{Method: "GET", Path: "/v1/users/{id}/watching", Summary: "What a user is watching right now (live scrobble session; {} when not watching)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Current playback or empty object", 403: "Private profile", 404: "Not found"}},

		// Comments / notes / likes
		{Method: "GET", Path: "/v1/users/{id}/comments/{comment_type}/{media_type}", Summary: "A user's comments (comment_type: all|reviews|shouts; media_type: all|movies|shows|seasons|episodes|lists)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Comments", 400: "Invalid comment type or media type", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/notes/{media_type}", Summary: "A user's notes filtered by media type", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Notes", 400: "Invalid media type", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/likes/comments", Summary: "Comments a user has liked", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Liked comments", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/likes/lists", Summary: "Lists a user has liked (lists not visible to the caller are omitted)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Liked lists", 403: "Private profile", 404: "Not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterProfileReadRoutes mounts the public-profile read surface.
//
// Call this LAST, after every static /users/me/* route has been registered
// (notes.RegisterRoutes, lists.RegisterRoutes, social.RegisterRoutes, stats,
// hidden, filters, auth's api-key routes, …). Fiber matches in registration
// order, so a /users/:id/<segment> path registered before /users/me/<segment>
// would swallow "me" as an id. internal/httpapi's TestNoStaticRouteIsShadowed
// guards this.
func RegisterProfileReadRoutes(api fiber.Router) {
	// OptionalAuth runs on the whole surface so PrivacyFilter can see WHO is
	// asking. Without it every caller looked anonymous, and a signed-in owner
	// was refused their own private sections. It also gives "me" something to
	// resolve to in the :id routes below.
	users := api.Group("/users", auth.OptionalAuth())

	// Caller's own likes: static "me" paths, registered before :id/likes below.
	users.Get("/me/likes/comments", auth.AuthRequired(), handleMyLikedComments)
	users.Get("/me/likes/lists", auth.AuthRequired(), handleMyLikedLists)

	// History — gated by users.settings.privacy.history.
	users.Get("/:id/history", auth.PrivacyFilterSection("id", "history"), handleUserHistory)
	users.Get("/:id/history/:media_type", auth.PrivacyFilterSection("id", "history"), handleUserHistory)
	users.Get("/:id/history/:media_type/:media_id", auth.PrivacyFilterSection("id", "history"), handleUserHistoryItem)

	// Ratings — gated by users.settings.privacy.ratings.
	users.Get("/:id/ratings", auth.PrivacyFilterSection("id", "ratings"), handleUserRatings)
	users.Get("/:id/ratings/:media_type", auth.PrivacyFilterSection("id", "ratings"), handleUserRatings)

	// Watchlist — no per-section flag names this collection; is_private/block only.
	users.Get("/:id/watchlist", auth.PrivacyFilter("id"), handleUserWatchlist)
	users.Get("/:id/watchlist/:media_type", auth.PrivacyFilter("id"), handleUserWatchlist)

	// Collection — no per-section flag names this collection; is_private/block only.
	users.Get("/:id/collection", auth.PrivacyFilter("id"), handleUserCollection)
	users.Get("/:id/collection/:media_type", auth.PrivacyFilter("id"), handleUserCollection)

	// Watching — the target's live activity; gated by users.settings.privacy.activity.
	users.Get("/:id/watching", auth.PrivacyFilterSection("id", "activity"), handleUserWatching)

	// Comments, notes, likes — no per-section flag names these; is_private/block only.
	users.Get("/:id/comments/:comment_type/:media_type", auth.PrivacyFilter("id"), handleUserComments)
	users.Get("/:id/notes/:media_type", auth.PrivacyFilter("id"), handleUserNotes)
	users.Get("/:id/likes/comments", auth.PrivacyFilter("id"), handleUserLikedComments)
	users.Get("/:id/likes/lists", auth.PrivacyFilter("id"), handleUserLikedLists)
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// profilePage is the pagination envelope of this surface. It is the same
// {items,page,limit,row_count} envelope the /v1/sync/* read routes return.
type profilePage struct {
	Items    interface{} `json:"items"`
	Page     int         `json:"page"`
	Limit    int         `json:"limit"`
	RowCount int64       `json:"row_count"`
}

// profileTarget loads the :id user and applies the account-status gate that
// PrivacyFilter does not: deleted / pending_deletion accounts are 404, exactly
// like GET /v1/users/:id. Unknown user → 404. Blocked / private handling has
// already happened in PrivacyFilter.
//
// ok is false when a response has already been written; the caller then
// returns nil (never the response.WriteError result — that is not an error).
func profileTarget(c *fiber.Ctx) (user *database.User, db *gorm.DB, ok bool) {
	handle, err := database.GetDB()
	if err != nil {
		_ = response.InternalError(c)
		return nil, nil, false
	}

	// "me" means the caller: /v1/users/me/watchlist is the same page as the
	// caller's own id. The privacy middleware resolves it the same way.
	targetID := auth.ResolveTargetID(c, "id")
	if targetID == "" {
		_ = response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
		return nil, nil, false
	}

	var target database.User
	if err := handle.First(&target, "id = ?", targetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = response.WriteError(c, fiber.StatusNotFound, "not_found", "user not found")
			return nil, nil, false
		}
		_ = response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
		return nil, nil, false
	}

	if target.Status == "deleted" || target.Status == "pending_deletion" {
		_ = response.WriteError(c, fiber.StatusNotFound, "not_found", "user not found")
		return nil, nil, false
	}

	return &target, handle, true
}

// profileMediaType maps a Trakt-style path segment (plural) or the internal
// singular value onto the media_type stored in the database. "" and "all" mean
// "no filter". ok is false for anything unknown.
func profileMediaType(raw string) (mt string, ok bool) {
	switch raw {
	case "", "all":
		return "", true
	case "movie", "movies":
		return "movie", true
	case "show", "shows":
		return "show", true
	case "season", "seasons":
		return "season", true
	case "episode", "episodes":
		return "episode", true
	default:
		return "", false
	}
}

// pageItems clamps a page/limit pair and returns the SQL offset.
func pageOffset(page, limit int) int { return (page - 1) * limit }

// ---------------------------------------------------------------------------
// History
// ---------------------------------------------------------------------------

// handleUserHistory serves /users/:id/history and /users/:id/history/:media_type.
func handleUserHistory(c *fiber.Ctx) error {
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}

	mt, ok := profileMediaType(c.Params("media_type"))
	if !ok {
		return response.BadRequest(c, "invalid media type")
	}

	page, limit := response.ParsePagination(c, 25)

	query := db.Where("user_id = ?", target.ID)
	if mt != "" {
		query = query.Where("media_type = ?", mt)
	}

	var total int64
	query.Model(&database.UserHistory{}).Count(&total)

	var rows []database.UserHistory
	if err := query.Order("watched_at DESC, id DESC").
		Offset(pageOffset(page, limit)).Limit(limit).
		Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	items := make([]sync.HistoryResponse, 0, len(rows))
	for i := range rows {
		items = append(items, sync.BuildHistoryResponse(&rows[i]))
	}

	response.SetPaginationHeaders(c, page, limit, total, len(items))
	return c.JSON(profilePage{Items: items, Page: page, Limit: limit, RowCount: total})
}

// handleUserHistoryItem serves /users/:id/history/:media_type/:media_id.
// media_id may be a local id, a slug, or an external id via ?id_type=.
func handleUserHistoryItem(c *fiber.Ctx) error {
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}

	mt, ok := profileMediaType(c.Params("media_type"))
	if !ok || mt == "" {
		return response.BadRequest(c, "invalid media type")
	}

	resolvedType, mediaID, resolveErr := media.ResolveID(c, c.Params("media_id"))
	if resolveErr != nil {
		return resolveErr
	}
	if resolvedType == "" {
		// media.ResolveID writes its own error envelope and returns a nil
		// error after doing so (e.g. unknown external id → 404). Nothing
		// left to send.
		return nil
	}
	if resolvedType != mt {
		return response.BadRequest(c, "media_type does not match the resolved media id")
	}

	var rows []database.UserHistory
	if err := db.Where("user_id = ? AND media_type = ? AND media_id = ?", target.ID, mt, mediaID).
		Order("watched_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	items := make([]sync.HistoryResponse, 0, len(rows))
	for i := range rows {
		items = append(items, sync.BuildHistoryResponse(&rows[i]))
	}

	return c.JSON(profilePage{Items: items, RowCount: int64(len(items))})
}

// ---------------------------------------------------------------------------
// Ratings
// ---------------------------------------------------------------------------

// handleUserRatings serves /users/:id/ratings and /users/:id/ratings/:media_type.
func handleUserRatings(c *fiber.Ctx) error {
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}

	mt, ok := profileMediaType(c.Params("media_type"))
	if !ok {
		return response.BadRequest(c, "invalid media type")
	}

	page, limit := response.ParsePagination(c, 25)

	query := db.Where("user_id = ?", target.ID)
	if mt != "" {
		query = query.Where("media_type = ?", mt)
	}

	var total int64
	query.Model(&database.UserRating{}).Count(&total)

	var rows []database.UserRating
	if err := query.Order("rated_at DESC, id DESC").
		Offset(pageOffset(page, limit)).Limit(limit).
		Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	items := sync.BuildRatingResponses(rows)

	response.SetPaginationHeaders(c, page, limit, total, len(items))
	return c.JSON(profilePage{Items: items, Page: page, Limit: limit, RowCount: total})
}

// ---------------------------------------------------------------------------
// Watchlist
// ---------------------------------------------------------------------------

// handleUserWatchlist serves /users/:id/watchlist and
// /users/:id/watchlist/:media_type. The sort behaviour (rank|listed|added|
// title) mirrors /v1/sync/watchlist.
func handleUserWatchlist(c *fiber.Ctx) error {
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}

	mt, ok := profileMediaType(c.Params("media_type"))
	if !ok {
		return response.BadRequest(c, "invalid media type")
	}

	sortBy := c.Query("sort_by", "rank")
	sortHow := c.Query("sort_how", "asc")
	if sortHow != "asc" && sortHow != "desc" {
		sortHow = "asc"
	}

	var order string
	switch sortBy {
	case "rank":
		order = "rank " + sortHow + ", id " + sortHow
	case "listed", "added":
		order = "listed_at " + sortHow + ", id " + sortHow
	case "title":
		order = `COALESCE(
			(SELECT m.title FROM movies m WHERE m.id = user_watchlist.media_id AND user_watchlist.media_type = 'movie'),
			(SELECT s.title FROM shows s WHERE s.id = user_watchlist.media_id AND user_watchlist.media_type = 'show')
		) ` + sortHow + `, media_type ` + sortHow + `, media_id ` + sortHow
	default:
		return response.BadRequest(c, "invalid sort_by (rank|listed|added|title)")
	}

	page, limit := response.ParsePagination(c, 25)

	query := db.Where("user_id = ?", target.ID)
	if mt != "" {
		query = query.Where("media_type = ?", mt)
	}

	var total int64
	query.Model(&database.UserWatchlist{}).Count(&total)

	var rows []database.UserWatchlist
	if err := query.Order(order).Offset(pageOffset(page, limit)).Limit(limit).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	response.SetPaginationHeaders(c, page, limit, total, len(rows))
	return c.JSON(profilePage{
		Items:    sync.BuildWatchlistResponses(rows),
		Page:     page,
		Limit:    limit,
		RowCount: total,
	})
}

// ---------------------------------------------------------------------------
// Collection
// ---------------------------------------------------------------------------

// handleUserCollection serves /users/:id/collection and
// /users/:id/collection/:media_type.
func handleUserCollection(c *fiber.Ctx) error {
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}

	mt, ok := profileMediaType(c.Params("media_type"))
	if !ok {
		return response.BadRequest(c, "invalid media type")
	}

	page, limit := response.ParsePagination(c, 25)

	query := db.Where("user_id = ?", target.ID)
	if mt != "" {
		query = query.Where("media_type = ?", mt)
	}

	var total int64
	query.Model(&database.UserCollection{}).Count(&total)

	var rows []database.UserCollection
	if err := query.Order("collected_at DESC, id DESC").
		Offset(pageOffset(page, limit)).Limit(limit).
		Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	response.SetPaginationHeaders(c, page, limit, total, len(rows))
	return c.JSON(profilePage{
		Items:    sync.BuildCollectionResponses(rows),
		Page:     page,
		Limit:    limit,
		RowCount: total,
	})
}

// ---------------------------------------------------------------------------
// Watching
// ---------------------------------------------------------------------------

// handleUserWatching serves /users/:id/watching.
//
// The predicate is real, not a guess: the newest non-expired scrobble session
// for the user is a live "start"/"scrobble" (a "pause" or "stop" session means
// they are NOT watching). user_playback.paused_at is deliberately NOT used —
// a saved resume position is not evidence of current playback. When there is
// no live session the response is an empty object, matching Trakt.
func handleUserWatching(c *fiber.Ctx) error {
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}

	var session database.ScrobbleSession
	err := db.Where("user_id = ? AND expires_at > ?", target.ID, time.Now()).
		Order("created_at DESC, id DESC").
		First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.JSON(fiber.Map{})
	}
	if err != nil {
		return response.InternalError(c)
	}

	if session.Action == "pause" || session.Action == "stop" {
		return c.JSON(fiber.Map{})
	}

	return c.JSON(fiber.Map{
		"media_type": session.MediaType,
		"media_id":   session.MediaID,
		"season_id":  session.SeasonID,
		"episode_id": session.EpisodeID,
		"progress":   session.Progress,
		"action":     session.Action,
		"started_at": response.FormatTime(session.CreatedAt),
		"expires_at": response.FormatTime(session.ExpiresAt),
	})
}

// ---------------------------------------------------------------------------
// Comments
// ---------------------------------------------------------------------------

// handleUserComments serves /users/:id/comments/:comment_type/:media_type.
// comment_type: all|reviews|shouts. media_type: all|movies|shows|seasons|
// episodes|lists.
func handleUserComments(c *fiber.Ctx) error {
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}

	var commentType string
	switch c.Params("comment_type") {
	case "all":
		commentType = ""
	case "shout", "shouts":
		commentType = "shout"
	case "review", "reviews":
		commentType = "review"
	default:
		return response.BadRequest(c, "invalid comment_type (all|reviews|shouts)")
	}

	page, limit := response.ParsePagination(c, 25)
	query := db.Where("user_id = ?", target.ID)
	if commentType != "" {
		query = query.Where("comment_type = ?", commentType)
	}

	switch c.Params("media_type") {
	case "", "all":
		// no media filter
	case "list", "lists":
		query = query.Where("list_id IS NOT NULL")
	default:
		mt, ok := profileMediaType(c.Params("media_type"))
		if !ok || mt == "" {
			return response.BadRequest(c, "invalid media type")
		}
		query = query.Where("media_type = ?", mt)
	}

	var total int64
	query.Model(&database.Comment{}).Count(&total)

	var rows []database.Comment
	if err := query.Order("created_at DESC, id DESC").
		Offset(pageOffset(page, limit)).Limit(limit).
		Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	items := make([]comments.CommentResponse, 0, len(rows))
	for i := range rows {
		items = append(items, comments.ToCommentResponse(&rows[i]))
	}

	response.SetPaginationHeaders(c, page, limit, total, len(items))
	return c.JSON(fiber.Map{
		"data":        items,
		"page":        page,
		"limit":       limit,
		"total_count": total,
	})
}

// ---------------------------------------------------------------------------
// Notes
// ---------------------------------------------------------------------------

// handleUserNotes serves /users/:id/notes/:media_type.
func handleUserNotes(c *fiber.Ctx) error {
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}

	mt, ok := profileMediaType(c.Params("media_type"))
	if !ok {
		return response.BadRequest(c, "invalid media type")
	}

	page, limit := response.ParsePagination(c, 25)
	query := db.Where("user_id = ?", target.ID)
	if mt != "" {
		query = query.Where("media_type = ?", mt)
	}

	var total int64
	query.Model(&database.UserNote{}).Count(&total)

	var rows []database.UserNote
	if err := query.Order("created_at DESC, id DESC").
		Offset(pageOffset(page, limit)).Limit(limit).
		Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	items := make([]notes.NoteResponse, 0, len(rows))
	for i := range rows {
		items = append(items, notes.ToNoteResponse(&rows[i]))
	}

	response.SetPaginationHeaders(c, page, limit, total, len(items))
	return c.JSON(fiber.Map{
		"data":        items,
		"page":        page,
		"limit":       limit,
		"total_count": total,
	})
}

// ---------------------------------------------------------------------------
// Likes
// ---------------------------------------------------------------------------

// likedCommentItem / likedListItem follow Trakt's liked-item shape:
// {liked_at, type, comment|list}.
type likedCommentItem struct {
	LikedAt string                   `json:"liked_at"`
	Type    string                   `json:"type"`
	Comment comments.CommentResponse `json:"comment"`
}

type likedListItem struct {
	LikedAt string         `json:"liked_at"`
	Type    string         `json:"type"`
	List    *database.List `json:"list"`
}

// handleUserLikedComments serves /users/:id/likes/comments.
func handleUserLikedComments(c *fiber.Ctx) error {
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}
	return writeLikedComments(c, db, target.ID)
}

// handleMyLikedComments serves /users/me/likes/comments.
func handleMyLikedComments(c *fiber.Ctx) error {
	caller, ok := c.Locals("user_id").(string)
	if !ok || caller == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}
	return writeLikedComments(c, db, caller)
}

func writeLikedComments(c *fiber.Ctx, db *gorm.DB, userID string) error {
	page, limit := response.ParsePagination(c, 25)

	query := db.Where("user_id = ?", userID)
	var total int64
	query.Model(&database.CommentLike{}).Count(&total)

	var likes []database.CommentLike
	if err := query.Order("created_at DESC, user_id DESC").
		Offset(pageOffset(page, limit)).Limit(limit).
		Find(&likes).Error; err != nil {
		return response.InternalError(c)
	}

	items := make([]likedCommentItem, 0, len(likes))
	if len(likes) > 0 {
		ids := make([]int64, 0, len(likes))
		for _, l := range likes {
			ids = append(ids, l.CommentID)
		}
		var rows []database.Comment
		if err := db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return response.InternalError(c)
		}
		byID := make(map[int64]database.Comment, len(rows))
		for i := range rows {
			byID[rows[i].ID] = rows[i]
		}
		for _, l := range likes {
			comment, found := byID[l.CommentID]
			if !found {
				continue // dangling like row — nothing real to show
			}
			items = append(items, likedCommentItem{
				LikedAt: response.FormatTime(l.CreatedAt),
				Type:    "comment",
				Comment: comments.ToCommentResponse(&comment),
			})
		}
	}

	response.SetPaginationHeaders(c, page, limit, total, len(items))
	return c.JSON(profilePage{Items: items, Page: page, Limit: limit, RowCount: total})
}

// handleUserLikedLists serves /users/:id/likes/lists.
func handleUserLikedLists(c *fiber.Ctx) error {
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}
	viewer, _ := c.Locals("user_id").(string)
	return writeLikedLists(c, db, target.ID, viewer)
}

// handleMyLikedLists serves /users/me/likes/lists.
func handleMyLikedLists(c *fiber.Ctx) error {
	caller, ok := c.Locals("user_id").(string)
	if !ok || caller == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}
	return writeLikedLists(c, db, caller, caller)
}

// writeLikedLists returns the lists a user has liked. A like whose list is not
// visible to the viewer (private, and the viewer does not own it) is omitted
// rather than leaked; the row_count is the number of like rows, so an omission
// is visible as items < row_count.
func writeLikedLists(c *fiber.Ctx, db *gorm.DB, userID, viewerID string) error {
	page, limit := response.ParsePagination(c, 25)

	query := db.Where("user_id = ?", userID)
	var total int64
	query.Model(&database.ListLike{}).Count(&total)

	var likes []database.ListLike
	if err := query.Order("created_at DESC, list_id DESC").
		Offset(pageOffset(page, limit)).Limit(limit).
		Find(&likes).Error; err != nil {
		return response.InternalError(c)
	}

	items := make([]likedListItem, 0, len(likes))
	if len(likes) > 0 {
		ids := make([]int64, 0, len(likes))
		for _, l := range likes {
			ids = append(ids, l.ListID)
		}
		var lists []database.List
		if err := db.Where("id IN ?", ids).Find(&lists).Error; err != nil {
			return response.InternalError(c)
		}
		byID := make(map[int64]database.List, len(lists))
		for i := range lists {
			byID[lists[i].ID] = lists[i]
		}
		for _, l := range likes {
			list, found := byID[l.ListID]
			if !found {
				continue
			}
			if !listVisibleTo(list, viewerID) {
				continue
			}
			listCopy := list
			items = append(items, likedListItem{
				LikedAt: response.FormatTime(l.CreatedAt),
				Type:    "list",
				List:    &listCopy,
			})
		}
	}

	response.SetPaginationHeaders(c, page, limit, total, len(items))
	return c.JSON(profilePage{Items: items, Page: page, Limit: limit, RowCount: total})
}

// listVisibleTo matches the visibility rule /v1/users/:id/lists applies:
// public and link_only lists are visible to everyone, everything else only to
// its owner.
func listVisibleTo(list database.List, viewerID string) bool {
	if list.Privacy == "public" || list.Privacy == "link_only" {
		return true
	}
	return viewerID != "" && viewerID == list.UserID
}
