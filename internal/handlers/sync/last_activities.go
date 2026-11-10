package sync

import (
	"database/sql"
	"errors"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// init registers this route in the code-first OpenAPI registry. It lives here
// rather than in sync.go so the size-ratchet's over-limit sync.go does not
// grow; the registry is order-independent, so a second init is harmless.
func init() {
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/sync/last_activities",
		Summary: "Get the latest activity timestamps per collection (Trakt last_activities delta)",
		Tags:    []string{"Sync"},
		Auth:    "required",
		StatusCodes: map[int]string{
			200: "Per-collection activity timestamps",
			401: "Unauthorized",
		},
	})
}

// ---------------------------------------------------------------------------
// GET /v1/sync/last_activities
//
// Trakt's "what changed since" endpoint: every Trakt client polls it before a
// sync and compares the per-collection timestamps with its local cache, so it
// only downloads what actually moved. The shape below mirrors Trakt's
// last_activities payload (see docs in the package audit doc, sync.md): an
// envelope of per-section objects, each holding Trakt's timestamp field names.
//
// Honesty rules this endpoint follows (issue #6, "the first honest slice"):
//
//   - Every leaf is a real aggregate over the rows we actually store (the
//     newest watched_at / collected_at / created_at / updated_at row, scoped to
//     the caller). No fabricated constants, no placeholder "now".
//   - A section the caller has no rows for is null, not an object of nulls.
//   - A Trakt field we do not store is an explicit null, never an invented
//     value. Today that is `episodes.watchlisted_at` and
//     `seasons.watchlisted_at`: our watchlist table only carries movies and
//     shows (documented gap in sync.md).
// ---------------------------------------------------------------------------

// LastActivitiesResponse is the payload for GET /v1/sync/last_activities.
type LastActivitiesResponse struct {
	All        *string            `json:"all"`
	Movies     *MovieActivities   `json:"movies"`
	Episodes   *EpisodeActivities `json:"episodes"`
	Shows      *ShowActivities    `json:"shows"`
	Seasons    *SeasonActivities  `json:"seasons"`
	Ratings    *UpdatedActivity   `json:"ratings"`
	Watchlist  *UpdatedActivity   `json:"watchlist"`
	Favorites  *UpdatedActivity   `json:"favorites"`
	Collection *UpdatedActivity   `json:"collection"`
	History    *UpdatedActivity   `json:"history"`
	Comments   *CommentActivities `json:"comments"`
	Notes      *UpdatedActivity   `json:"notes"`
	Lists      *ListActivities    `json:"lists"`
	Follows    *UpdatedActivity   `json:"follows"`
	Checkins   *UpdatedActivity   `json:"checkins"`
	Playback   *UpdatedActivity   `json:"playback"`
}

// MovieActivities mirrors Trakt's `movies` section.
type MovieActivities struct {
	WatchedAt     *string `json:"watched_at"`
	CollectedAt   *string `json:"collected_at"`
	RatedAt       *string `json:"rated_at"`
	WatchlistedAt *string `json:"watchlisted_at"`
	FavoritedAt   *string `json:"favorited_at"`
	CommentedAt   *string `json:"commented_at"`
	PausedAt      *string `json:"paused_at"`
	HiddenAt      *string `json:"hidden_at"`
}

// EpisodeActivities mirrors Trakt's `episodes` section. WatchlistedAt is
// always null: we do not store episode watchlist rows.
type EpisodeActivities struct {
	WatchedAt     *string `json:"watched_at"`
	CollectedAt   *string `json:"collected_at"`
	RatedAt       *string `json:"rated_at"`
	WatchlistedAt *string `json:"watchlisted_at"`
	CommentedAt   *string `json:"commented_at"`
	PausedAt      *string `json:"paused_at"`
}

// ShowActivities mirrors Trakt's `shows` section.
type ShowActivities struct {
	RatedAt       *string `json:"rated_at"`
	WatchlistedAt *string `json:"watchlisted_at"`
	FavoritedAt   *string `json:"favorited_at"`
	CommentedAt   *string `json:"commented_at"`
	HiddenAt      *string `json:"hidden_at"`
}

// SeasonActivities mirrors Trakt's `seasons` section. WatchlistedAt is always
// null: we do not store season watchlist entries.
type SeasonActivities struct {
	RatedAt       *string `json:"rated_at"`
	WatchlistedAt *string `json:"watchlisted_at"`
	CommentedAt   *string `json:"commented_at"`
	HiddenAt      *string `json:"hidden_at"`
}

// CommentActivities mirrors Trakt's `comments` section. We do not track
// comment blocks, so blocked_at is not part of our shape.
type CommentActivities struct {
	LikedAt *string `json:"liked_at"`
}

// ListActivities mirrors Trakt's `lists` section.
type ListActivities struct {
	LikedAt     *string `json:"liked_at"`
	UpdatedAt   *string `json:"updated_at"`
	CommentedAt *string `json:"commented_at"`
}

// UpdatedActivity is a section whose single activity marker is a server-side
// update time (the `{updated_at}` sections in Trakt's payload).
type UpdatedActivity struct {
	UpdatedAt *string `json:"updated_at"`
}

// handleGetLastActivities returns the caller's latest activity per collection.
func handleGetLastActivities(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return response.Unauthorized(c)
	}

	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	resp, err := buildLastActivities(db, userID)
	if err != nil {
		return response.InternalError(c)
	}
	return c.JSON(resp)
}

// buildLastActivities runs the per-collection aggregates and assembles the
// Trakt-shaped envelope.
func buildLastActivities(db *gorm.DB, userID string) (*LastActivitiesResponse, error) {
	const (
		mtMovie   = "movie"
		mtEpisode = "episode"
		mtShow    = "show"
		mtSeason  = "season"
	)

	// get accumulates the first query error so the derivation below reads as a
	// flat list of aggregates rather than twenty error checks.
	var qerr error
	get := func(table, tsColumn, userColumn string, filters ...activityFilter) *string {
		if qerr != nil {
			return nil
		}
		v, err := maxActivity(db, table, tsColumn, userColumn, userID, filters...)
		if err != nil {
			qerr = err
			return nil
		}
		return v
	}
	mt := func(t string) activityFilter { return eq("media_type", t) }

	movies := &MovieActivities{
		WatchedAt:     get("user_history", "watched_at", "user_id", mt(mtMovie)),
		CollectedAt:   get("user_collection", "collected_at", "user_id", mt(mtMovie)),
		RatedAt:       get("user_ratings", "rated_at", "user_id", mt(mtMovie)),
		WatchlistedAt: get("user_watchlist", "listed_at", "user_id", mt(mtMovie)),
		FavoritedAt:   get("user_favorites", "favorited_at", "user_id", mt(mtMovie)),
		CommentedAt:   get("comments", "created_at", "user_id", mt(mtMovie)),
		PausedAt:      get("user_playback", "paused_at", "user_id", mt(mtMovie)),
		HiddenAt:      get("user_hidden", "hidden_at", "user_id", mt(mtMovie)),
	}

	episodes := &EpisodeActivities{
		WatchedAt:   get("user_history", "watched_at", "user_id", mt(mtEpisode)),
		CollectedAt: get("user_collection", "collected_at", "user_id", mt(mtEpisode)),
		RatedAt:     get("user_ratings", "rated_at", "user_id", mt(mtEpisode)),
		CommentedAt: get("comments", "created_at", "user_id", mt(mtEpisode)),
		PausedAt:    get("user_playback", "paused_at", "user_id", mt(mtEpisode)),
		// WatchlistedAt: not stored (null, not invented).
	}

	shows := &ShowActivities{
		RatedAt:       get("user_ratings", "rated_at", "user_id", mt(mtShow)),
		WatchlistedAt: get("user_watchlist", "listed_at", "user_id", mt(mtShow)),
		FavoritedAt:   get("user_favorites", "favorited_at", "user_id", mt(mtShow)),
		CommentedAt:   get("comments", "created_at", "user_id", mt(mtShow)),
		HiddenAt:      get("user_hidden", "hidden_at", "user_id", mt(mtShow)),
	}

	seasons := &SeasonActivities{
		RatedAt:     get("user_ratings", "rated_at", "user_id", mt(mtSeason)),
		CommentedAt: get("comments", "created_at", "user_id", mt(mtSeason)),
		HiddenAt:    get("user_hidden", "hidden_at", "user_id", mt(mtSeason)),
		// WatchlistedAt: not stored (null, not invented).
	}

	comments := &CommentActivities{
		LikedAt: get("comment_likes", "created_at", "user_id"),
	}

	lists := &ListActivities{
		LikedAt:     get("list_likes", "created_at", "user_id"),
		UpdatedAt:   get("lists", "updated_at", "user_id"),
		CommentedAt: get("comments", "created_at", "user_id", notNull("list_id")),
	}

	// Server-side write markers. These use the row's own created_at/updated_at
	// (the moment we recorded the change), not client-supplied dates, so a
	// brand-new write always advances them — exactly what a polling client
	// compares against.
	ratings := &UpdatedActivity{UpdatedAt: get("user_ratings", "updated_at", "user_id")}
	watchlist := &UpdatedActivity{UpdatedAt: get("user_watchlist", "created_at", "user_id")}
	favorites := &UpdatedActivity{UpdatedAt: get("user_favorites", "created_at", "user_id")}
	collection := &UpdatedActivity{UpdatedAt: get("user_collection", "created_at", "user_id")}
	history := &UpdatedActivity{UpdatedAt: get("user_history", "created_at", "user_id")}
	notes := &UpdatedActivity{UpdatedAt: get("user_notes", "updated_at", "user_id")}
	follows := &UpdatedActivity{UpdatedAt: get("user_follows", "created_at", "follower_id")}
	checkins := &UpdatedActivity{UpdatedAt: get("user_checkins", "created_at", "user_id")}
	// user_playback has no created_at/updated_at; paused_at is its write time.
	playback := &UpdatedActivity{UpdatedAt: get("user_playback", "paused_at", "user_id")}

	if qerr != nil {
		return nil, qerr
	}

	resp := &LastActivitiesResponse{
		Movies:     movieOrNil(movies),
		Episodes:   episodeOrNil(episodes),
		Shows:      showOrNil(shows),
		Seasons:    seasonOrNil(seasons),
		Ratings:    updatedOrNil(ratings),
		Watchlist:  updatedOrNil(watchlist),
		Favorites:  updatedOrNil(favorites),
		Collection: updatedOrNil(collection),
		History:    updatedOrNil(history),
		Comments:   commentOrNil(comments),
		Notes:      updatedOrNil(notes),
		Lists:      listOrNil(lists),
		Follows:    updatedOrNil(follows),
		Checkins:   updatedOrNil(checkins),
		Playback:   updatedOrNil(playback),
	}

	// `all` is the greatest activity across every section, or null when the
	// account has no tracked activity at all.
	resp.All = maxStamp(allStamps(resp)...)
	return resp, nil
}

// ---------------------------------------------------------------------------
// Aggregation
// ---------------------------------------------------------------------------

// activityFilter is a WHERE predicate for maxActivity.
type activityFilter struct {
	clause string
	args   []any
}

func eq(column string, value any) activityFilter {
	return activityFilter{clause: column + " = ?", args: []any{value}}
}

func notNull(column string) activityFilter {
	return activityFilter{clause: column + " IS NOT NULL"}
}

// maxActivity returns the newest tsColumn value for the caller, rendered in the
// API's wire format, or nil when the caller has no matching rows. filters add
// equality predicates (media_type, section, list_id IS NOT NULL, ...).
//
// It reads the latest row (`ORDER BY ts DESC LIMIT 1`) rather than MAX(ts): an
// aggregate loses the column's declared type, so sqlite hands back raw text that
// cannot be decoded into a time, whereas selecting the bare column keeps the
// driver's native time decoding on both sqlite and Postgres. The index on
// (user_id, <ts>) makes it one index-ordered read; the value is the same as
// MAX(ts).
func maxActivity(db *gorm.DB, table, tsColumn, userColumn, userID string, filters ...activityFilter) (*string, error) {
	q := db.Table(table).Where(userColumn+" = ?", userID)
	for _, f := range filters {
		q = q.Where(f.clause, f.args...)
	}

	var ts sql.NullTime
	err := q.Select(tsColumn).Order(tsColumn + " DESC").Limit(1).Row().Scan(&ts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !ts.Valid {
		return nil, nil
	}
	return stampPtr(ts.Time), nil
}

func stampPtr(t time.Time) *string {
	s := response.FormatTime(t.UTC())
	return &s
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// maxStamp returns the greatest non-nil stamp. Every stamp is produced by
// response.FormatTime, a fixed-width UTC layout, so lexicographic order equals
// chronological order.
func maxStamp(stamps ...*string) *string {
	var max *string
	for _, s := range stamps {
		if s == nil {
			continue
		}
		if max == nil || *s > *max {
			max = s
		}
	}
	return max
}

func movieOrNil(a *MovieActivities) *MovieActivities {
	if !anySet(a.WatchedAt, a.CollectedAt, a.RatedAt, a.WatchlistedAt, a.FavoritedAt, a.CommentedAt, a.PausedAt, a.HiddenAt) {
		return nil
	}
	return a
}

func episodeOrNil(a *EpisodeActivities) *EpisodeActivities {
	// CollectedAt belongs here: episode collection writes create collection rows
	// with no history or rating, and without it the whole episodes section was
	// reported as null even though the account had collected episodes.
	if !anySet(a.WatchedAt, a.CollectedAt, a.RatedAt, a.CommentedAt, a.PausedAt) {
		return nil
	}
	return a
}

func showOrNil(a *ShowActivities) *ShowActivities {
	if !anySet(a.RatedAt, a.WatchlistedAt, a.FavoritedAt, a.CommentedAt, a.HiddenAt) {
		return nil
	}
	return a
}

func seasonOrNil(a *SeasonActivities) *SeasonActivities {
	if !anySet(a.RatedAt, a.CommentedAt, a.HiddenAt) {
		return nil
	}
	return a
}

func commentOrNil(a *CommentActivities) *CommentActivities {
	if a.LikedAt == nil {
		return nil
	}
	return a
}

func listOrNil(a *ListActivities) *ListActivities {
	if !anySet(a.LikedAt, a.UpdatedAt, a.CommentedAt) {
		return nil
	}
	return a
}

func updatedOrNil(a *UpdatedActivity) *UpdatedActivity {
	if a.UpdatedAt == nil {
		return nil
	}
	return a
}

func anySet(stamps ...*string) bool {
	for _, s := range stamps {
		if s != nil {
			return true
		}
	}
	return false
}

// allStamps flattens every section's leaves into one list for the `all` marker.
func allStamps(r *LastActivitiesResponse) []*string {
	var out []*string
	add := func(ss ...*string) { out = append(out, ss...) }
	if r.Movies != nil {
		add(r.Movies.WatchedAt, r.Movies.CollectedAt, r.Movies.RatedAt, r.Movies.WatchlistedAt,
			r.Movies.FavoritedAt, r.Movies.CommentedAt, r.Movies.PausedAt, r.Movies.HiddenAt)
	}
	if r.Episodes != nil {
		add(r.Episodes.WatchedAt, r.Episodes.CollectedAt, r.Episodes.RatedAt, r.Episodes.CommentedAt, r.Episodes.PausedAt)
	}
	if r.Shows != nil {
		add(r.Shows.RatedAt, r.Shows.WatchlistedAt, r.Shows.FavoritedAt, r.Shows.CommentedAt, r.Shows.HiddenAt)
	}
	if r.Seasons != nil {
		add(r.Seasons.RatedAt, r.Seasons.CommentedAt, r.Seasons.HiddenAt)
	}
	if r.Ratings != nil {
		add(r.Ratings.UpdatedAt)
	}
	if r.Watchlist != nil {
		add(r.Watchlist.UpdatedAt)
	}
	if r.Favorites != nil {
		add(r.Favorites.UpdatedAt)
	}
	if r.Collection != nil {
		add(r.Collection.UpdatedAt)
	}
	if r.History != nil {
		add(r.History.UpdatedAt)
	}
	if r.Comments != nil {
		add(r.Comments.LikedAt)
	}
	if r.Notes != nil {
		add(r.Notes.UpdatedAt)
	}
	if r.Lists != nil {
		add(r.Lists.LikedAt, r.Lists.UpdatedAt, r.Lists.CommentedAt)
	}
	if r.Follows != nil {
		add(r.Follows.UpdatedAt)
	}
	if r.Checkins != nil {
		add(r.Checkins.UpdatedAt)
	}
	if r.Playback != nil {
		add(r.Playback.UpdatedAt)
	}
	return out
}
