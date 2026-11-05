package users

// User data export — GET /v1/users/me/export (issue #10 §1).
//
// The archive is streamed, not assembled: a power user's history is unbounded
// by the project charter (no caps on personal data), so the handler must never
// hold the whole archive — or even one whole section — in memory. We walk each
// table with a database/sql cursor and hand every row straight to a
// json.Encoder writing into the response body stream.
//
// The archive carries a version field so the format can evolve; the importer in
// issue #7 is meant to consume the same shape (a Trakt-compatible variant is
// tracked as a follow-up, not invented here).
//
// Secrets are never exported. We deliberately do not read webhook signing
// secrets, integration tokens, API keys or OAuth credentials at all; the
// webhook section exposes the fact that an endpoint exists, nothing more.

import (
	"bufio"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ExportFormatVersion is the schema version of the archive produced by
// GET /v1/users/me/export. Bump it on any backward-incompatible change so the
// importer can branch on it.
//
//	v1: { version, exported_at, user, settings, history, ratings, watchlist,
//	      collection, favorites, lists[{...list, items:[...]}], comments,
//	      notes, hidden, follows{following,followers}, blocks, checkins,
//	      playback, webhook_endpoints }
const ExportFormatVersion = 1

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// handleExport streams the caller's complete data archive as JSON.
func handleExport(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}

	// Resolve the DB and the user *before* the first byte is written: once the
	// body stream starts we can no longer change the status code, so every
	// failure we can report with an envelope must happen up front.
	db, err := database.GetDB()
	if err != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	var user database.User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "user not found")
		}
		slog.Error("user export: load user failed", "user_id", userID, "error", err)
		return response.InternalError(c, "database query error")
	}

	now := time.Now().UTC()
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="tkiller-export.json"`)

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		if err := writeArchive(w, db, &user, now); err != nil {
			// The response has already begun, so there is no envelope left to
			// send; record the failure and let the truncated body signal it.
			slog.Error("user export: stream failed", "user_id", userID, "error", err)
		}
	})
	return nil
}

// ---------------------------------------------------------------------------
// Streaming JSON writer
//
// A tiny nesting-aware writer: it tracks, per open container, whether a comma
// is due, so callers can push an object/array, write keys/values, and pop —
// which is what lets the lists section stream nested item arrays without
// buffering them.
// ---------------------------------------------------------------------------

type streamWriter struct {
	w   *bufio.Writer
	enc *json.Encoder
	// needComma[i] reports whether the i-th open container already holds an
	// element and therefore needs a separator before the next one.
	needComma []bool
	// pendingKey is set right after a key is written so the value that follows
	// it does not emit its own separator.
	pendingKey bool
}

func newStreamWriter(w *bufio.Writer) *streamWriter {
	return &streamWriter{w: w, enc: json.NewEncoder(w)}
}

// open writes the container delimiter and pushes comma state.
func (s *streamWriter) open(delim byte) error {
	if err := s.beforeElem(); err != nil {
		return err
	}
	if err := s.w.WriteByte(delim); err != nil {
		return err
	}
	s.needComma = append(s.needComma, false)
	return nil
}

// close pops comma state and writes the container delimiter.
func (s *streamWriter) close(delim byte) error {
	if len(s.needComma) > 0 {
		s.needComma = s.needComma[:len(s.needComma)-1]
	}
	return s.w.WriteByte(delim)
}

// beforeElem writes a separator unless the previous token was an object key
// (whose value must follow the colon directly).
func (s *streamWriter) beforeElem() error {
	if s.pendingKey {
		s.pendingKey = false
		return nil
	}
	return s.sep()
}

// sep emits a separating comma when the current container already has content.
func (s *streamWriter) sep() error {
	if len(s.needComma) == 0 {
		return nil
	}
	top := len(s.needComma) - 1
	if s.needComma[top] {
		return s.w.WriteByte(',')
	}
	s.needComma[top] = true
	return nil
}

// key writes a JSON object key (with any needed separator).
func (s *streamWriter) key(name string) error {
	if err := s.sep(); err != nil {
		return err
	}
	b, err := json.Marshal(name)
	if err != nil {
		return err
	}
	if _, err := s.w.Write(b); err != nil {
		return err
	}
	if err := s.w.WriteByte(':'); err != nil {
		return err
	}
	s.pendingKey = true
	return nil
}

// value writes a JSON value (array element or object field value).
func (s *streamWriter) value(v any) error {
	if err := s.beforeElem(); err != nil {
		return err
	}
	return s.enc.Encode(v)
}

// ---------------------------------------------------------------------------
// Cursor helper — one row in memory at a time
// ---------------------------------------------------------------------------

// streamModel walks every row of model matching where/args and calls emit per
// row, holding only a single row in memory. GORM's Find materialises the whole
// result set into a slice before returning, and FindInBatches needs a primary
// key that user_hidden and user_playback do not have, so we drive database/sql
// directly through GORM's Rows() cursor and scan each row ourselves.
//
// The SELECT list is taken from the parsed model schema so it matches the
// struct exactly (a hand-created table can carry extra columns — e.g. the
// production-only `id` on user_playback — that must not desync the scan).
func streamModel[T any](db *gorm.DB, where string, args []any, emit func(*T) error) error {
	var zero T
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&zero); err != nil {
		return err
	}

	fields := stmt.Schema.Fields
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = f.DBName
	}

	rows, err := db.Model(&zero).Select(cols).Where(where, args...).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()

	rt := reflect.TypeOf(zero)
	for rows.Next() {
		dests := make([]any, len(fields))
		for i, f := range fields {
			dests[i] = reflect.New(f.FieldType).Interface()
		}
		if err := rows.Scan(dests...); err != nil {
			return err
		}
		v := reflect.New(rt)
		for i, f := range fields {
			v.Elem().FieldByIndex(f.StructField.Index).Set(reflect.ValueOf(dests[i]).Elem())
		}
		if err := emit(v.Interface().(*T)); err != nil {
			return err
		}
	}
	return rows.Err()
}

// arraySection writes "name":[ ...one element per streamed row... ].
func arraySection[T any](s *streamWriter, db *gorm.DB, name, where string, args []any, to func(*T) any) error {
	if err := s.key(name); err != nil {
		return err
	}
	if err := s.open('['); err != nil {
		return err
	}
	if err := streamModel(db, where, args, func(row *T) error {
		return s.value(to(row))
	}); err != nil {
		return err
	}
	return s.close(']')
}

// ---------------------------------------------------------------------------
// Archive assembly
// ---------------------------------------------------------------------------

func writeArchive(w *bufio.Writer, db *gorm.DB, user *database.User, now time.Time) error {
	s := newStreamWriter(w)

	if err := s.open('{'); err != nil {
		return err
	}

	if err := s.key("version"); err != nil {
		return err
	}
	if err := s.value(ExportFormatVersion); err != nil {
		return err
	}

	if err := s.key("exported_at"); err != nil {
		return err
	}
	if err := s.value(response.FormatTime(now)); err != nil {
		return err
	}

	if err := s.key("user"); err != nil {
		return err
	}
	if err := s.value(toExportUser(user)); err != nil {
		return err
	}

	if err := s.key("settings"); err != nil {
		return err
	}
	if err := s.value(parseSettings(user.Settings)); err != nil {
		return err
	}

	// Library sections — streamed row by row.
	if err := arraySection(s, db, "history", "user_id = ?", []any{user.ID}, toExportHistory); err != nil {
		return err
	}
	if err := arraySection(s, db, "ratings", "user_id = ?", []any{user.ID}, toExportRating); err != nil {
		return err
	}
	if err := arraySection(s, db, "watchlist", "user_id = ?", []any{user.ID}, toExportWatchlist); err != nil {
		return err
	}
	if err := arraySection(s, db, "collection", "user_id = ?", []any{user.ID}, toExportCollection); err != nil {
		return err
	}
	if err := arraySection(s, db, "favorites", "user_id = ?", []any{user.ID}, toExportFavorite); err != nil {
		return err
	}

	// Lists, each with its streamed item array. Lists are paged with keyset
	// pagination rather than held in one open cursor: a nested cursor would
	// require a second connection mid-stream (which an in-memory sqlite test DB
	// cannot provide), and paging keeps memory bounded regardless of list count.
	if err := s.writeLists(db, user.ID); err != nil {
		return err
	}

	if err := arraySection(s, db, "comments", "user_id = ?", []any{user.ID}, toExportComment); err != nil {
		return err
	}
	if err := arraySection(s, db, "notes", "user_id = ?", []any{user.ID}, toExportNote); err != nil {
		return err
	}
	if err := arraySection(s, db, "hidden", "user_id = ?", []any{user.ID}, toExportHidden); err != nil {
		return err
	}

	// Follows (both directions) and blocks.
	if err := s.key("follows"); err != nil {
		return err
	}
	if err := s.open('{'); err != nil {
		return err
	}
	if err := s.outgoingFollows(db, user.ID); err != nil {
		return err
	}
	if err := s.incomingFollows(db, user.ID); err != nil {
		return err
	}
	if err := s.close('}'); err != nil {
		return err
	}

	if err := arraySection(s, db, "blocks", "blocker_id = ?", []any{user.ID}, toExportBlock); err != nil {
		return err
	}
	if err := arraySection(s, db, "checkins", "user_id = ?", []any{user.ID}, toExportCheckin); err != nil {
		return err
	}
	if err := arraySection(s, db, "playback", "user_id = ?", []any{user.ID}, toExportPlayback); err != nil {
		return err
	}

	// Webhook endpoints — deliberately not their signing secret.
	if err := arraySection(s, db, "webhook_endpoints", "user_id = ?", []any{user.ID}, toExportWebhookEndpoint); err != nil {
		return err
	}

	return s.close('}')
}

// exportListPageSize bounds how many list rows are materialised at once while
// paging the lists section.
const exportListPageSize = 100

// writeLists streams the "lists" array, paging by id so the whole set of lists
// is never held in memory at once.
func (s *streamWriter) writeLists(db *gorm.DB, userID string) error {
	if err := s.key("lists"); err != nil {
		return err
	}
	if err := s.open('['); err != nil {
		return err
	}
	var lastID int64
	for {
		var page []database.List
		if err := db.Where("user_id = ?", userID).Where("id > ?", lastID).
			Order("id ASC").Limit(exportListPageSize).Find(&page).Error; err != nil {
			return err
		}
		for i := range page {
			if err := s.writeList(db, &page[i]); err != nil {
				return err
			}
		}
		if len(page) < exportListPageSize {
			break
		}
		lastID = page[len(page)-1].ID
	}
	return s.close(']')
}

// writeList emits one list object with a nested, streamed items array.
func (s *streamWriter) writeList(db *gorm.DB, l *database.List) error {
	if err := s.open('{'); err != nil {
		return err
	}
	for _, f := range []struct {
		key string
		val any
	}{
		{"id", l.ID},
		{"name", l.Name},
		{"slug", l.Slug},
		{"description", l.Description},
		{"privacy", l.Privacy},
		{"sort_by", l.SortBy},
		{"sort_how", l.SortHow},
		{"is_ranked", l.IsRanked},
		{"cover_url", l.CoverURL},
		{"allow_comments", l.AllowComments},
		{"created_at", response.FormatTime(l.CreatedAt)},
		{"updated_at", response.FormatTime(l.UpdatedAt)},
	} {
		if err := s.key(f.key); err != nil {
			return err
		}
		if err := s.value(f.val); err != nil {
			return err
		}
	}
	if err := s.key("items"); err != nil {
		return err
	}
	if err := s.open('['); err != nil {
		return err
	}
	if err := streamModel(db, "list_id = ?", []any{l.ID}, func(it *database.ListItem) error {
		return s.value(toExportListItem(it))
	}); err != nil {
		return err
	}
	if err := s.close(']'); err != nil {
		return err
	}
	return s.close('}')
}

func (s *streamWriter) outgoingFollows(db *gorm.DB, userID string) error {
	if err := s.key("following"); err != nil {
		return err
	}
	if err := s.open('['); err != nil {
		return err
	}
	if err := streamModel(db, "follower_id = ?", []any{userID}, func(f *database.UserFollow) error {
		return s.value(exportFollow{
			UserID:     f.FollowingID,
			ApprovedAt: exportTimePtr(f.ApprovedAt),
			CreatedAt:  response.FormatTime(f.CreatedAt),
		})
	}); err != nil {
		return err
	}
	return s.close(']')
}

func (s *streamWriter) incomingFollows(db *gorm.DB, userID string) error {
	if err := s.key("followers"); err != nil {
		return err
	}
	if err := s.open('['); err != nil {
		return err
	}
	if err := streamModel(db, "following_id = ?", []any{userID}, func(f *database.UserFollow) error {
		return s.value(exportFollow{
			UserID:     f.FollowerID,
			ApprovedAt: exportTimePtr(f.ApprovedAt),
			CreatedAt:  response.FormatTime(f.CreatedAt),
		})
	}); err != nil {
		return err
	}
	return s.close(']')
}

// ---------------------------------------------------------------------------
// Archive value types (documented export shape)
// ---------------------------------------------------------------------------

type exportUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email,omitempty"`
	Name        string `json:"name"`
	Bio         string `json:"bio,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Location    string `json:"location,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	Language    string `json:"language,omitempty"`
	IsPrivate   bool   `json:"is_private"`
	IsAdmin     bool   `json:"is_admin"`
	IsModerator bool   `json:"is_moderator"`
	Status      string `json:"status"`
	Plan        string `json:"plan"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type exportHistory struct {
	ID        int64   `json:"id"`
	MediaType string  `json:"media_type"`
	MediaID   int64   `json:"media_id"`
	SeasonID  *int64  `json:"season_id,omitempty"`
	EpisodeID *int64  `json:"episode_id,omitempty"`
	WatchedAt string  `json:"watched_at"`
	Source    string  `json:"source"`
	SourceID  *string `json:"source_id,omitempty"`
}

type exportRating struct {
	ID        int64  `json:"id"`
	MediaType string `json:"media_type"`
	MediaID   int64  `json:"media_id"`
	Rating    int    `json:"rating"`
	RatedAt   string `json:"rated_at"`
}

type exportWatchlist struct {
	ID        int64   `json:"id"`
	MediaType string  `json:"media_type"`
	MediaID   int64   `json:"media_id"`
	Rank      int     `json:"rank"`
	Notes     *string `json:"notes,omitempty"`
	ListedAt  string  `json:"listed_at"`
}

type exportCollection struct {
	ID          int64  `json:"id"`
	MediaType   string `json:"media_type"`
	MediaID     int64  `json:"media_id"`
	CollectedAt string `json:"collected_at"`
}

type exportFavorite struct {
	ID          int64  `json:"id"`
	MediaType   string `json:"media_type"`
	MediaID     int64  `json:"media_id"`
	Rank        int    `json:"rank"`
	FavoritedAt string `json:"favorited_at"`
}

type exportListItem struct {
	ID        int64  `json:"id"`
	MediaType string `json:"media_type"`
	MediaID   int64  `json:"media_id"`
	Rank      int    `json:"rank"`
	Notes     string `json:"notes,omitempty"`
	AddedBy   string `json:"added_by"`
	AddedAt   string `json:"added_at"`
}

type exportComment struct {
	ID          int64   `json:"id"`
	ParentID    *int64  `json:"parent_id,omitempty"`
	MediaType   string  `json:"media_type,omitempty"`
	MediaID     *int64  `json:"media_id,omitempty"`
	ListID      *int64  `json:"list_id,omitempty"`
	CommentType string  `json:"comment_type"`
	Body        string  `json:"body"`
	Spoiler     bool    `json:"spoiler"`
	LikeCount   int     `json:"like_count"`
	ReplyCount  int     `json:"reply_count"`
	EditedAt    *string `json:"edited_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type exportNote struct {
	ID        int64  `json:"id"`
	MediaType string `json:"media_type"`
	MediaID   int64  `json:"media_id"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type exportHidden struct {
	Section   string `json:"section"`
	MediaType string `json:"media_type"`
	MediaID   int64  `json:"media_id"`
	HiddenAt  string `json:"hidden_at"`
}

type exportFollow struct {
	UserID     string  `json:"user_id"`
	ApprovedAt *string `json:"approved_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

type exportBlock struct {
	BlockedID string `json:"blocked_id"`
	CreatedAt string `json:"created_at"`
}

type exportCheckin struct {
	ID        int64  `json:"id"`
	MediaType string `json:"media_type"`
	MediaID   int64  `json:"media_id"`
	SeasonID  *int64 `json:"season_id,omitempty"`
	EpisodeID *int64 `json:"episode_id,omitempty"`
	Message   string `json:"message,omitempty"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}

type exportPlayback struct {
	MediaType string  `json:"media_type"`
	MediaID   int64   `json:"media_id"`
	Progress  float64 `json:"progress"`
	PausedAt  string  `json:"paused_at"`
}

// exportWebhookEndpoint is the webhook export shape: the endpoint's existence,
// target and subscriptions — never its signing secret (SecretHash is not read).
type exportWebhookEndpoint struct {
	ID        int64    `json:"id"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Active    bool     `json:"active"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// Model -> export mapping
// ---------------------------------------------------------------------------

func toExportUser(u *database.User) exportUser {
	return exportUser{
		ID:          u.ID,
		Username:    u.Username,
		Email:       u.Email,
		Name:        u.Name,
		Bio:         u.Bio,
		AvatarURL:   u.AvatarURL,
		Location:    u.Location,
		Timezone:    u.Timezone,
		Language:    u.Language,
		IsPrivate:   u.IsPrivate,
		IsAdmin:     u.IsAdmin,
		IsModerator: u.IsModerator,
		Status:      u.Status,
		Plan:        u.Plan,
		CreatedAt:   response.FormatTime(u.CreatedAt),
		UpdatedAt:   response.FormatTime(u.UpdatedAt),
	}
}

func toExportHistory(h *database.UserHistory) any {
	return exportHistory{
		ID:        h.ID,
		MediaType: h.MediaType,
		MediaID:   h.MediaID,
		SeasonID:  h.SeasonID,
		EpisodeID: h.EpisodeID,
		WatchedAt: response.FormatTime(h.WatchedAt),
		Source:    h.Source,
		SourceID:  h.SourceID,
	}
}

func toExportRating(r *database.UserRating) any {
	return exportRating{
		ID:        r.ID,
		MediaType: r.MediaType,
		MediaID:   r.MediaID,
		Rating:    r.Rating,
		RatedAt:   response.FormatTime(r.RatedAt),
	}
}

func toExportWatchlist(w *database.UserWatchlist) any {
	return exportWatchlist{
		ID:        w.ID,
		MediaType: w.MediaType,
		MediaID:   w.MediaID,
		Rank:      w.Rank,
		Notes:     w.Notes,
		ListedAt:  response.FormatTime(w.ListedAt),
	}
}

func toExportCollection(c *database.UserCollection) any {
	return exportCollection{
		ID:          c.ID,
		MediaType:   c.MediaType,
		MediaID:     c.MediaID,
		CollectedAt: response.FormatTime(c.CollectedAt),
	}
}

func toExportFavorite(f *database.UserFavorite) any {
	return exportFavorite{
		ID:          f.ID,
		MediaType:   f.MediaType,
		MediaID:     f.MediaID,
		Rank:        f.Rank,
		FavoritedAt: response.FormatTime(f.FavoritedAt),
	}
}

func toExportListItem(it *database.ListItem) any {
	return exportListItem{
		ID:        it.ID,
		MediaType: it.MediaType,
		MediaID:   it.MediaID,
		Rank:      it.Rank,
		Notes:     it.Notes,
		AddedBy:   it.AddedBy,
		AddedAt:   response.FormatTime(it.AddedAt),
	}
}

func toExportComment(cm *database.Comment) any {
	return exportComment{
		ID:          cm.ID,
		ParentID:    cm.ParentID,
		MediaType:   cm.MediaType,
		MediaID:     cm.MediaID,
		ListID:      cm.ListID,
		CommentType: cm.CommentType,
		Body:        cm.Body,
		Spoiler:     cm.Spoiler,
		LikeCount:   cm.LikeCount,
		ReplyCount:  cm.ReplyCount,
		EditedAt:    exportTimePtr(cm.EditedAt),
		CreatedAt:   response.FormatTime(cm.CreatedAt),
		UpdatedAt:   response.FormatTime(cm.UpdatedAt),
	}
}

func toExportNote(n *database.UserNote) any {
	return exportNote{
		ID:        n.ID,
		MediaType: n.MediaType,
		MediaID:   n.MediaID,
		Note:      n.Note,
		CreatedAt: response.FormatTime(n.CreatedAt),
		UpdatedAt: response.FormatTime(n.UpdatedAt),
	}
}

func toExportHidden(h *database.UserHidden) any {
	return exportHidden{
		Section:   h.Section,
		MediaType: h.MediaType,
		MediaID:   h.MediaID,
		HiddenAt:  response.FormatTime(h.HiddenAt),
	}
}

func toExportBlock(b *database.UserBlock) any {
	return exportBlock{
		BlockedID: b.BlockedID,
		CreatedAt: response.FormatTime(b.CreatedAt),
	}
}

func toExportCheckin(ck *database.UserCheckin) any {
	return exportCheckin{
		ID:        ck.ID,
		MediaType: ck.MediaType,
		MediaID:   ck.MediaID,
		SeasonID:  ck.SeasonID,
		EpisodeID: ck.EpisodeID,
		Message:   ck.Message,
		ExpiresAt: response.FormatTime(ck.ExpiresAt),
		CreatedAt: response.FormatTime(ck.CreatedAt),
	}
}

func toExportPlayback(p *database.UserPlayback) any {
	return exportPlayback{
		MediaType: p.MediaType,
		MediaID:   p.MediaID,
		Progress:  p.Progress,
		PausedAt:  response.FormatTime(p.PausedAt),
	}
}

// toExportWebhookEndpoint maps the endpoint without ever touching SecretHash.
func toExportWebhookEndpoint(wh *database.WebhookEndpoint) any {
	events := []string(wh.Events)
	if events == nil {
		events = []string{}
	}
	return exportWebhookEndpoint{
		ID:        wh.ID,
		URL:       wh.URL,
		Events:    events,
		Active:    wh.Active,
		CreatedAt: response.FormatTime(wh.CreatedAt),
		UpdatedAt: response.FormatTime(wh.UpdatedAt),
	}
}

func exportTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := response.FormatTime(*t)
	return &s
}
