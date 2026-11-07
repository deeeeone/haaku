// Package syncengine processes batch sync sessions (issue #6).
//
// It is the consumer the POST /v1/syncs handler has always pointed at with a
// TODO. A session is created pending by the API process, which never resolves
// or writes a single item itself; this package claims a pending session, walks
// its persisted items, resolves each against our media data, writes the target
// collection, records a per-item outcome, and drives the session to a terminal
// state.
//
// It is deliberately its own package with no HTTP dependency so a worker (a
// sibling lane owns worker topology) can call it from anywhere — see
// ProcessPendingSessions. Nothing here starts a goroutine or a ticker: the
// caller owns the schedule, so the API request path can never accidentally run
// a session.
//
// Guarantees the issue asks for, and where each lives:
//   - idempotent: only state='pending' items are ever worked, so re-running a
//     session does not double-write; the collection writes are additionally
//     ON CONFLICT DO NOTHING / upsert against the existing unique indexes.
//   - concurrency-safe: a session is claimed with a PostgreSQL advisory lock
//     (database.WithAdvisoryLockStr) around a compare-and-swap status update,
//     so two processors racing on one session cannot both own it.
//   - poison-tolerant: one bad item is recorded as an outcome='error' and the
//     session continues; a panic in one item is recovered and recorded too.
package syncengine

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// itemBatch bounds how many items are resolved and written per round-trip, so a
// large session is processed in bounded chunks and a resume continues from the
// lowest unprocessed seq.
const itemBatch = 200

// errorItemLimit caps the per-session error detail written into
// sync_sessions.errors, so one pathological session cannot bloat the row. The
// full per-item record always stays in sync_session_items.
const errorItemLimit = 100

// Error codes recorded per item (closed set, see syncs.md).
const (
	// codeNotFound: the item's id did not resolve to a media row we hold. It
	// is the code on every not_found item in the typed session.Errors report
	// and the reason /v1/syncs/{id}/paused serves it.
	codeNotFound = "not_found"
	// codeInvalidPayload: the submitted item is malformed (not an object, no
	// tmdb id, an unsupported media type or kind, an out-of-range rating).
	codeInvalidPayload = "invalid_payload"
	// codeWriteError: the collection write itself failed.
	codeWriteError = "write_error"
)

// Result summarises one processed session.
type Result struct {
	SessionID int64
	Status    string
	Total     int
	Processed int
	Counts    database.SyncErrorCounts
}

// ProcessSession claims and processes one session if it is pending.
//
// It is a no-op for a session that is already terminal or already claimed by
// another processor (that is what makes a re-run idempotent at the session
// level). The returned Result reflects the session's state either way.
func ProcessSession(db *gorm.DB, sessionID int64) (Result, error) {
	claimed, err := claimSession(db, sessionID)
	if err != nil {
		return Result{SessionID: sessionID}, fmt.Errorf("claim session %d: %w", sessionID, err)
	}

	session, err := loadSession(db, sessionID)
	if err != nil {
		return Result{SessionID: sessionID}, err
	}
	if !claimed {
		return Result{
			SessionID: session.ID, Status: session.Status,
			Total: session.TotalItems, Processed: session.ProcessedItems,
			Counts: session.Errors.Counts,
		}, nil
	}

	if err := processItems(db, session.ID, session.UserID, session.Kind); err != nil {
		markFailed(db, session.ID)
		return Result{SessionID: session.ID, Status: database.SyncStatusFailed},
			fmt.Errorf("process session %d items: %w", session.ID, err)
	}

	report, processed, total, err := finalize(db, session.ID)
	if err != nil {
		markFailed(db, session.ID)
		return Result{SessionID: session.ID, Status: database.SyncStatusFailed},
			fmt.Errorf("finalize session %d: %w", session.ID, err)
	}

	return Result{
		SessionID: session.ID, Status: database.SyncStatusCompleted,
		Total: total, Processed: processed, Counts: report.Counts,
	}, nil
}

// ProcessPendingSessions processes up to limit pending sessions, oldest first.
// This is the worker entrypoint: it is the only call a worker loop needs.
func ProcessPendingSessions(db *gorm.DB, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 50
	}
	var ids []int64
	if err := db.Model(&database.SyncSession{}).
		Where("status = ?", database.SyncStatusPending).
		Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("list pending sessions: %w", err)
	}

	results := make([]Result, 0, len(ids))
	for _, id := range ids {
		res, err := ProcessSession(db, id)
		if err != nil {
			// One session failing must not stop the others; the session is
			// left in a terminal (failed) state by ProcessSession.
			log.Printf("[sync-engine] session %d: %v", id, err)
			continue
		}
		results = append(results, res)
	}
	return results, nil
}

// --- claim -------------------------------------------------------------------

// claimSession takes the advisory lock for the session and flips it
// pending -> processing in one transaction. The compare-and-swap is the durable
// guard: whichever processor committed the update owns the session, and every
// other processor (and every re-run) sees a non-pending status and stops.
func claimSession(db *gorm.DB, sessionID int64) (bool, error) {
	claimed := false
	err := database.WithAdvisoryLockStr(db, func(tx *gorm.DB) error {
		res := tx.Exec(
			`UPDATE sync_sessions SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status = ?`,
			database.SyncStatusProcessing, sessionID, database.SyncStatusPending)
		if res.Error != nil {
			return res.Error
		}
		claimed = res.RowsAffected > 0
		return nil
	}, fmt.Sprintf("sync-engine:session:%d", sessionID))
	return claimed, err
}

func loadSession(db *gorm.DB, sessionID int64) (*database.SyncSession, error) {
	var session database.SyncSession
	if err := db.First(&session, "id = ?", sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("sync session %d not found", sessionID)
		}
		return nil, fmt.Errorf("load session %d: %w", sessionID, err)
	}
	return &session, nil
}

// --- per-item processing -----------------------------------------------------

// processItems walks the session's pending items in seq order until none are
// left, resolving each batch's ids in one query and writing one item at a time.
func processItems(db *gorm.DB, sessionID int64, userID, kind string) error {
	for {
		var items []database.SyncSessionItem
		if err := db.Where("session_id = ? AND state = ?", sessionID, "pending").
			Order("seq ASC").Limit(itemBatch).Find(&items).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		refs := resolveBatch(db, items)
		for i := range items {
			processOne(db, userID, kind, &items[i], refs)
		}
	}
}

// resolveBatch resolves the TMDB ids of a batch in one query. A payload that
// will not parse contributes no id — applyItem records it as an error later.
func resolveBatch(db *gorm.DB, items []database.SyncSessionItem) map[int]media.ResolvedMedia {
	var tmdbIDs []int
	for _, it := range items {
		if p, ok := parsePayload(it.Payload); ok && p.IDs.TMDB != 0 {
			tmdbIDs = append(tmdbIDs, p.IDs.TMDB)
		}
	}
	refs, err := media.ResolveTMDBBatch(db, tmdbIDs)
	if err != nil {
		// A resolution failure is not fatal to the session: every item simply
		// resolves to nothing this pass.
		log.Printf("[sync-engine] batch resolve failed: %v", err)
		return map[int]media.ResolvedMedia{}
	}
	return refs
}

// processOne applies one item inside its own transaction and records the
// outcome. Content problems are outcomes, not transaction failures; only a
// genuine database error aborts the transaction, and then the item is recorded
// as an error so the session is never stuck on it.
func processOne(db *gorm.DB, userID, kind string, it *database.SyncSessionItem, refs map[int]media.ResolvedMedia) {
	var res applied
	txErr := runItemTx(db, userID, kind, it, refs, &res)
	if txErr == nil {
		return
	}
	res = applied{outcome: database.SyncOutcomeError, code: codeWriteError, message: txErr.Error()}
	if err := recordOutcome(db, it.ID, res); err != nil {
		log.Printf("[sync-engine] item %d: recording error failed: %v", it.ID, err)
	}
}

// runItemTx runs applyItem + recordOutcome atomically, converting a panic in an
// item's handling into an error instead of a process crash.
func runItemTx(db *gorm.DB, userID, kind string, it *database.SyncSessionItem, refs map[int]media.ResolvedMedia, res *applied) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic processing item %d: %v", it.ID, r)
		}
	}()
	return db.Transaction(func(tx *gorm.DB) error {
		a, aerr := applyItem(tx, userID, kind, it.Payload, refs)
		if aerr != nil {
			return aerr
		}
		*res = a
		return recordOutcome(tx, it.ID, a)
	})
}

// recordOutcome writes the terminal state of one item.
func recordOutcome(tx *gorm.DB, itemID int64, a applied) error {
	return tx.Model(&database.SyncSessionItem{}).
		Where("id = ? AND state = ?", itemID, "pending").
		Updates(map[string]any{
			"state":        "processed",
			"outcome":      a.outcome,
			"error_code":   nullable(a.code),
			"error":        nullable(a.message),
			"media_type":   a.mediaType,
			"media_id":     a.mediaID,
			"watched_at":   a.watchedAt,
			"rating":       a.rating,
			"processed_at": time.Now().UTC(),
		}).Error
}

// --- finalize ----------------------------------------------------------------

// finalize tallies the session's items, writes the typed errors report, and
// moves the session to 'completed'.
func finalize(db *gorm.DB, sessionID int64) (database.SyncErrors, int, int, error) {
	var total int64
	if err := db.Model(&database.SyncSessionItem{}).
		Where("session_id = ?", sessionID).Count(&total).Error; err != nil {
		return database.SyncErrors{}, 0, 0, err
	}

	var counts []struct {
		Outcome string
		N       int
	}
	if err := db.Model(&database.SyncSessionItem{}).
		Select("outcome, count(*) AS n").
		Where("session_id = ? AND state = ?", sessionID, "processed").
		Group("outcome").Scan(&counts).Error; err != nil {
		return database.SyncErrors{}, 0, 0, err
	}

	var report database.SyncErrors
	processed := 0
	for _, c := range counts {
		processed += c.N
		switch c.Outcome {
		case database.SyncOutcomeAdded:
			report.Counts.Added = c.N
		case database.SyncOutcomeNotFound:
			report.Counts.NotFound = c.N
		case database.SyncOutcomeDuplicate:
			report.Counts.Duplicate = c.N
		case database.SyncOutcomeError:
			report.Counts.Error = c.N
		}
	}

	// Items names every item that did not land: not_found (its id did not
	// resolve) and error (a poisoned item). Duplicates are not failures — they
	// are served by /v1/syncs/{id}/skipped — so they stay in Counts only.
	var landed []database.SyncSessionItem
	if err := db.Where("session_id = ? AND outcome IN ?", sessionID,
		[]string{database.SyncOutcomeNotFound, database.SyncOutcomeError}).
		Order("seq ASC").Limit(errorItemLimit).Find(&landed).Error; err != nil {
		return database.SyncErrors{}, 0, 0, err
	}
	report.Items = make([]database.SyncItemError, 0, len(landed))
	for _, it := range landed {
		item := database.SyncItemError{
			ItemID: it.ID, Seq: it.Seq, IDs: payloadIDs(it.Payload),
		}
		if it.ErrorCode != nil {
			item.Code = *it.ErrorCode
		}
		if it.Error != nil {
			item.Message = *it.Error
		}
		report.Items = append(report.Items, item)
	}

	updates := database.SyncSession{
		Status:         database.SyncStatusCompleted,
		TotalItems:     int(total),
		ProcessedItems: processed,
		Errors:         report,
		UpdatedAt:      time.Now().UTC(),
	}
	if err := db.Model(&database.SyncSession{}).Where("id = ?", sessionID).
		Select("status", "total_items", "processed_items", "errors", "updated_at").
		Updates(&updates).Error; err != nil {
		return database.SyncErrors{}, 0, 0, err
	}

	return report, processed, int(total), nil
}

func markFailed(db *gorm.DB, sessionID int64) {
	if err := db.Model(&database.SyncSession{}).Where("id = ?", sessionID).
		Updates(map[string]any{
			"status":     database.SyncStatusFailed,
			"updated_at": time.Now().UTC(),
		}).Error; err != nil {
		log.Printf("[sync-engine] marking session %d failed: %v", sessionID, err)
	}
}

// --- payload + write ---------------------------------------------------------

// itemIDs is the identity a submitted item carries; tmdb is the only id the
// resolver needs today.
type itemIDs struct {
	TMDB int    `json:"tmdb"`
	TVDB int    `json:"tvdb"`
	IMDB string `json:"imdb"`
}

// itemPayload is the client-supplied item shape, mirroring the synchronous
// /v1/sync endpoints: an id bag, a media type, and the per-kind attributes.
type itemPayload struct {
	IDs       itemIDs `json:"ids"`
	MediaType string  `json:"media_type"`
	WatchedAt string  `json:"watched_at"`
	Rating    int     `json:"rating"`
	Season    *int    `json:"season"`
	Number    *int    `json:"number"`
}

// applied is the outcome of handling one item.
type applied struct {
	outcome   string
	code      string
	message   string
	mediaType *string
	mediaID   *int64
	watchedAt *time.Time
	rating    *int
}

func appliedError(code, format string, args ...any) applied {
	return applied{outcome: database.SyncOutcomeError, code: code, message: fmt.Sprintf(format, args...)}
}

func appliedNotFound(format string, args ...any) applied {
	return applied{outcome: database.SyncOutcomeNotFound, code: codeNotFound, message: fmt.Sprintf(format, args...)}
}

func appliedDuplicate() applied { return applied{outcome: database.SyncOutcomeDuplicate} }

func appliedAdded(mt string, id int64) applied {
	return applied{outcome: database.SyncOutcomeAdded, mediaType: &mt, mediaID: &id}
}

// applyItem resolves one item and writes it to the collection for its kind.
// It never returns an error for a content problem — those are outcomes.
func applyItem(tx *gorm.DB, userID, kind string, payload database.JSONMap, refs map[int]media.ResolvedMedia) (applied, error) {
	p, ok := parsePayload(payload)
	if !ok {
		return appliedError(codeInvalidPayload, "item payload is not a valid item object"), nil
	}
	if p.IDs.TMDB == 0 {
		return appliedError(codeInvalidPayload, "item has no tmdb id"), nil
	}

	switch kind {
	case "history":
		return applyHistory(tx, userID, p, refs)
	case "ratings":
		return applyRating(tx, userID, p, refs)
	default:
		return appliedError(codeInvalidPayload, "session kind %q is not supported", kind), nil
	}
}

func applyHistory(tx *gorm.DB, userID string, p itemPayload, refs map[int]media.ResolvedMedia) (applied, error) {
	ref, ok := refs[p.IDs.TMDB]
	if !ok {
		return appliedNotFound("tmdb id %d did not resolve to a movie or episode", p.IDs.TMDB), nil
	}

	switch p.MediaType {
	case "movie":
		if ref.MediaType != "movie" {
			return appliedNotFound("tmdb id %d resolved to %s, not a movie", p.IDs.TMDB, ref.MediaType), nil
		}
		watchedAt, a := watchedAtFor(tx, p, "movie", ref.MediaID, nil)
		if a != nil {
			return *a, nil
		}
		added, dup := insertHistory(tx, userID, "movie", ref.MediaID, nil, nil, watchedAt)
		return historyOutcome(added, dup, "movie", ref.MediaID, watchedAt), nil

	case "episode":
		ep, a := resolveEpisode(tx, ref, p)
		if a != nil {
			return *a, nil
		}
		watchedAt, a := watchedAtFor(tx, p, "episode", ep.ID, ep.AiredAt)
		if a != nil {
			return *a, nil
		}
		added, dup := insertHistory(tx, userID, "episode", ep.ID, &ep.SeasonID, &ep.ID, watchedAt)
		return historyOutcome(added, dup, "episode", ep.ID, watchedAt), nil

	default:
		return appliedError(codeInvalidPayload, "history items must be movie or episode, got %q", p.MediaType), nil
	}
}

func historyOutcome(added, dup bool, mediaType string, mediaID int64, watchedAt time.Time) applied {
	if !added || dup {
		a := appliedDuplicate()
		a.mediaType = &mediaType
		a.mediaID = &mediaID
		return a
	}
	a := appliedAdded(mediaType, mediaID)
	w := watchedAt
	a.watchedAt = &w
	return a
}

func applyRating(tx *gorm.DB, userID string, p itemPayload, refs map[int]media.ResolvedMedia) (applied, error) {
	if p.Rating < 1 || p.Rating > 10 {
		return appliedError(codeInvalidPayload, "rating %d is outside 1..10", p.Rating), nil
	}
	ref, ok := refs[p.IDs.TMDB]
	if !ok {
		return appliedNotFound("tmdb id %d did not resolve", p.IDs.TMDB), nil
	}

	write := func(mt string, id int64) (applied, error) {
		now := time.Now().UTC()
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "media_type"}, {Name: "media_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"rating", "rated_at", "updated_at"}),
		}).Create(&database.UserRating{
			UserID: userID, MediaType: mt, MediaID: id,
			Rating: p.Rating, RatedAt: now, UpdatedAt: now,
		})
		if result.Error != nil {
			return applied{}, result.Error
		}
		a := appliedAdded(mt, id)
		r := p.Rating
		a.rating = &r
		return a, nil
	}

	switch p.MediaType {
	case "movie", "show", "season":
		if ref.MediaType != p.MediaType {
			return appliedNotFound("tmdb id %d resolved to %s, not %s", p.IDs.TMDB, ref.MediaType, p.MediaType), nil
		}
		return write(ref.MediaType, ref.MediaID)
	case "episode":
		ep, a := resolveEpisode(tx, ref, p)
		if a != nil {
			return *a, nil
		}
		return write("episode", ep.ID)
	default:
		return appliedError(codeInvalidPayload, "rating items must be movie, show, season or episode, got %q", p.MediaType), nil
	}
}

// --- helpers -----------------------------------------------------------------

func parsePayload(payload database.JSONMap) (itemPayload, bool) {
	var p itemPayload
	if payload == nil {
		return p, false
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return p, false
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, false
	}
	return p, true
}

func payloadIDs(payload database.JSONMap) database.JSONMap {
	p, ok := parsePayload(payload)
	if !ok {
		return nil
	}
	ids := database.JSONMap{}
	if p.IDs.TMDB != 0 {
		ids["tmdb"] = p.IDs.TMDB
	}
	if p.IDs.TVDB != 0 {
		ids["tvdb"] = p.IDs.TVDB
	}
	if p.IDs.IMDB != "" {
		ids["imdb"] = p.IDs.IMDB
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

// resolveEpisode walks series ref -> season -> episode, exactly as the
// synchronous /v1/sync endpoints do, because an episode's ids.tmdb identifies
// the series.
func resolveEpisode(tx *gorm.DB, ref media.ResolvedMedia, p itemPayload) (*database.Episode, *applied) {
	if ref.MediaType != "show" {
		a := appliedNotFound("tmdb id %d resolved to %s, not a show", p.IDs.TMDB, ref.MediaType)
		return nil, &a
	}
	if p.Season == nil || p.Number == nil {
		a := appliedError(codeInvalidPayload, "episode items require season and number")
		return nil, &a
	}
	var season database.Season
	if err := tx.Where("show_id = ? AND season_number = ?", ref.MediaID, *p.Season).First(&season).Error; err != nil {
		a := appliedNotFound("season %d of show %d not found", *p.Season, ref.MediaID)
		return nil, &a
	}
	var ep database.Episode
	if err := tx.Where("season_id = ? AND episode_number = ?", season.ID, *p.Number).First(&ep).Error; err != nil {
		a := appliedNotFound("episode %d of season %d not found", *p.Number, *p.Season)
		return nil, &a
	}
	return &ep, nil
}

// watchedAtFor resolves the item's watched_at. "released" uses the title's own
// date when we have it, "unknown"/empty is now, and an explicit non-RFC3339
// value is a poisoned item — never silently rewritten to now.
func watchedAtFor(tx *gorm.DB, p itemPayload, mediaType string, mediaID int64, aired *database.Date) (time.Time, *applied) {
	now := time.Now().UTC()
	switch p.WatchedAt {
	case "", "unknown":
		return now, nil
	case "released":
		if aired != nil && !aired.IsZero() {
			return aired.Time, nil
		}
		if mediaType == "movie" {
			var movie database.Movie
			if err := tx.Select("released").First(&movie, mediaID).Error; err == nil &&
				movie.Released != nil && !movie.Released.IsZero() {
				return movie.Released.Time, nil
			}
		}
		return now, nil
	default:
		t, err := time.Parse(time.RFC3339, p.WatchedAt)
		if err != nil {
			a := appliedError(codeInvalidPayload, "watched_at %q is not RFC3339", p.WatchedAt)
			return time.Time{}, &a
		}
		return t, nil
	}
}

func insertHistory(tx *gorm.DB, userID, mediaType string, mediaID int64, seasonID, episodeID *int64, watchedAt time.Time) (added, dup bool) {
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&database.UserHistory{
		UserID: userID, MediaType: mediaType, MediaID: mediaID,
		SeasonID: seasonID, EpisodeID: episodeID, WatchedAt: watchedAt,
	})
	if result.Error != nil {
		return false, false
	}
	return result.RowsAffected > 0, result.RowsAffected == 0
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
