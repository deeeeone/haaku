package syncengine

import (
	"os"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const engineTestUser = "user-sync-engine-test"

// setupEngineDB mirrors the per-module DB harness used across the repo: the
// real PostgreSQL when DATABASE_URL is set (scripts/dev-pg.sh), an in-memory
// sqlite otherwise, so `make test-go` runs with no services. The engine leans
// on jsonb, partial-index-free ON CONFLICT and a pg advisory lock, so the
// real-cluster run is the meaningful one; sqlite keeps the default path green
// (the lock degrades gracefully — see database.WithAdvisoryLockStr).
func setupEngineDB(t *testing.T) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	var err error
	models := append([]interface{}{
		&database.Movie{}, &database.Show{}, &database.Season{}, &database.Episode{},
		&database.ExternalID{}, &database.UserHistory{}, &database.UserRating{},
	}, database.SyncEngineModels()...)

	if url := os.Getenv("DATABASE_URL"); url != "" {
		db, err = gorm.Open(postgres.Open(url), &gorm.Config{})
		if err != nil {
			t.Fatalf("open PostgreSQL: %v", err)
		}
		// The shared throwaway cluster may carry leftovers from an earlier run.
		if err := db.Migrator().DropTable(models...); err != nil {
			t.Fatalf("drop stale PG tables: %v", err)
		}
	} else {
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatalf("open in-memory sqlite: %v", err)
		}
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	// Reproduce the production dedupe index from 0006_create_user_history.sql.
	// GORM's struct tag can only express a plain column unique index, not the
	// COALESCE(source_id, '') expression the real schema uses, and a plain
	// index over a nullable source_id never conflicts (NULLs are distinct in
	// both engines) — so without this the harness would not detect duplicates
	// and the engine's ON CONFLICT DO NOTHING would look broken when it is not.
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_history_dedupe_expr
		ON user_history (user_id, media_type, media_id, source, COALESCE(source_id, ''))`).Error; err != nil {
		t.Fatalf("create production dedupe index: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// seedEngineMedia creates the media rows an item resolves against: one movie
// (tmdb 550 -> movies.id 10) and one show with one season and one episode
// (tmdb 1396 -> shows.id 20), each reachable through external_ids — the same
// lookup ResolveTMDBBatch performs in production.
func seedEngineMedia(t *testing.T, db *gorm.DB) {
	t.Helper()
	mustDo(t, db.Create(&database.Movie{ID: 10, TmdbID: 550, Title: "Fight Club"}).Error)
	mustDo(t, db.Create(&database.ExternalID{MediaType: "movie", MediaID: 10, Source: "tmdb", ExternalID: "550"}).Error)

	mustDo(t, db.Create(&database.Show{ID: 20, TmdbID: 1396, Title: "Breaking Bad"}).Error)
	mustDo(t, db.Create(&database.ExternalID{MediaType: "show", MediaID: 20, Source: "tmdb", ExternalID: "1396"}).Error)

	mustDo(t, db.Create(&database.Season{ID: 30, ShowID: 20, TmdbShowID: 1396, SeasonNumber: 1}).Error)
	aired := &database.Date{Time: time.Date(2008, 1, 20, 0, 0, 0, 0, time.UTC)}
	mustDo(t, db.Create(&database.Episode{
		ID: 40, SeasonID: 30, ShowID: 20, EpisodeNumber: 1, Title: "Pilot", AiredAt: aired,
	}).Error)
}

// newSessionWithItems seeds a pending session plus its persisted items, exactly
// as POST /v1/syncs does in one transaction.
func newSessionWithItems(t *testing.T, db *gorm.DB, kind string, items []database.JSONMap) *database.SyncSession {
	t.Helper()
	s := &database.SyncSession{
		UserID: engineTestUser, Kind: kind,
		Status: database.SyncStatusPending, TotalItems: len(items),
	}
	mustDo(t, db.Create(s).Error)
	rows := make([]database.SyncSessionItem, 0, len(items))
	for i, p := range items {
		rows = append(rows, database.SyncSessionItem{
			SessionID: s.ID, Seq: i + 1, Payload: p, State: "pending",
		})
	}
	mustDo(t, db.Create(&rows).Error)
	return s
}

func itemsFor(t *testing.T, db *gorm.DB, sessionID int64) []database.SyncSessionItem {
	t.Helper()
	var items []database.SyncSessionItem
	mustDo(t, db.Where("session_id = ?", sessionID).Order("seq ASC").Find(&items).Error)
	return items
}

func outcomeOf(it database.SyncSessionItem) string {
	if it.Outcome == nil {
		return ""
	}
	return *it.Outcome
}

// TestProcessSessionRecordsPerItemOutcomes is the acceptance path the issue
// asks for: create -> process -> read back, including a not_found item. One
// session carries an added movie, an unresolvable id, a duplicate, and a
// malformed item, so every terminal outcome is exercised in one pass.
func TestProcessSessionRecordsPerItemOutcomes(t *testing.T) {
	db := setupEngineDB(t)
	seedEngineMedia(t, db)

	session := newSessionWithItems(t, db, "history", []database.JSONMap{
		{"ids": database.JSONMap{"tmdb": 550}, "media_type": "movie", "watched_at": "2024-01-15T20:30:00Z"}, // added
		{"ids": database.JSONMap{"tmdb": 999}, "media_type": "movie"},                                       // not_found
		{"ids": database.JSONMap{"tmdb": 550}, "media_type": "movie"},                                       // duplicate
		{"media_type": "movie"}, // error: no tmdb id
	})

	res, err := ProcessSession(db, session.ID)
	if err != nil {
		t.Fatalf("ProcessSession: %v", err)
	}
	if res.Status != database.SyncStatusCompleted {
		t.Errorf("status = %q, want completed", res.Status)
	}
	if res.Total != 4 || res.Processed != 4 {
		t.Errorf("total/processed = %d/%d, want 4/4", res.Total, res.Processed)
	}
	want := database.SyncErrorCounts{Added: 1, NotFound: 1, Duplicate: 1, Error: 1}
	if res.Counts != want {
		t.Errorf("counts = %+v, want %+v", res.Counts, want)
	}

	items := itemsFor(t, db, session.ID)
	if len(items) != 4 {
		t.Fatalf("persisted items = %d, want 4", len(items))
	}
	wantOutcomes := []string{
		database.SyncOutcomeAdded, database.SyncOutcomeNotFound,
		database.SyncOutcomeDuplicate, database.SyncOutcomeError,
	}
	for i, want := range wantOutcomes {
		if got := outcomeOf(items[i]); got != want {
			t.Errorf("item seq %d outcome = %q, want %q", items[i].Seq, got, want)
		}
		if items[i].State != "processed" || items[i].ProcessedAt == nil {
			t.Errorf("item seq %d not moved to processed/processed_at", items[i].Seq)
		}
	}
	// The not_found item is resolvable data: the /paused read needs its code.
	if items[1].ErrorCode == nil || *items[1].ErrorCode != codeNotFound {
		t.Errorf("item seq 2 error_code = %v, want not_found", items[1].ErrorCode)
	}

	// The added movie reached user_history with the submitted watched_at.
	var h database.UserHistory
	mustDo(t, db.Where("user_id = ? AND media_type = ? AND media_id = ?", engineTestUser, "movie", 10).First(&h).Error)
	wantWatched := time.Date(2024, 1, 15, 20, 30, 0, 0, time.UTC)
	if !h.WatchedAt.UTC().Equal(wantWatched) {
		t.Errorf("history watched_at = %v, want %v", h.WatchedAt.UTC(), wantWatched)
	}

	// The typed session.Errors report is persisted on the session: counts for
	// every outcome, Items naming each item that did not land.
	var loaded database.SyncSession
	mustDo(t, db.First(&loaded, session.ID).Error)
	if loaded.Status != database.SyncStatusCompleted {
		t.Errorf("persisted status = %q, want completed", loaded.Status)
	}
	if loaded.Errors.Counts != want {
		t.Errorf("persisted errors.counts = %+v, want %+v", loaded.Errors.Counts, want)
	}
	if len(loaded.Errors.Items) != 2 {
		t.Fatalf("persisted errors.items = %d, want 2 (not_found + error)", len(loaded.Errors.Items))
	}
	if loaded.Errors.Items[0].Seq != 2 || loaded.Errors.Items[0].Code != codeNotFound {
		t.Errorf("errors.items[0] = %+v, want seq 2 not_found", loaded.Errors.Items[0])
	}
	if got := loaded.Errors.Items[0].IDs["tmdb"]; got != float64(999) {
		t.Errorf("errors.items[0].ids.tmdb = %v, want 999", got)
	}
	if loaded.Errors.Items[1].Seq != 4 || loaded.Errors.Items[1].Code != codeInvalidPayload {
		t.Errorf("errors.items[1] = %+v, want seq 4 invalid_payload", loaded.Errors.Items[1])
	}

	// A second pass is a no-op: the session is terminal, so nothing is claimed
	// and no collection row is written twice.
	if _, err := ProcessSession(db, session.ID); err != nil {
		t.Fatalf("second ProcessSession: %v", err)
	}
	var historyRows int64
	mustDo(t, db.Model(&database.UserHistory{}).Where("user_id = ?", engineTestUser).Count(&historyRows).Error)
	if historyRows != 1 {
		t.Errorf("history rows after re-run = %d, want 1 (idempotent)", historyRows)
	}
}

// TestProcessSessionResolvesEpisodeAndRating covers the episode path (series
// id -> season -> number) and the ratings upsert, so the engine is exercised on
// both kinds it supports.
func TestProcessSessionResolvesEpisodeAndRating(t *testing.T) {
	db := setupEngineDB(t)
	seedEngineMedia(t, db)

	history := newSessionWithItems(t, db, "history", []database.JSONMap{
		{"ids": database.JSONMap{"tmdb": 1396}, "media_type": "episode", "season": 1, "number": 1, "watched_at": "released"},
	})
	if _, err := ProcessSession(db, history.ID); err != nil {
		t.Fatalf("history session: %v", err)
	}
	items := itemsFor(t, db, history.ID)
	if got := outcomeOf(items[0]); got != database.SyncOutcomeAdded {
		t.Fatalf("episode outcome = %q, want added (%v)", got, items[0].Error)
	}
	if items[0].MediaType == nil || *items[0].MediaType != "episode" || items[0].MediaID == nil || *items[0].MediaID != 40 {
		t.Errorf("episode item media ref = %v/%v, want episode/40", items[0].MediaType, items[0].MediaID)
	}
	// watched_at "released" uses the episode's air date.
	var h database.UserHistory
	mustDo(t, db.Where("user_id = ? AND media_type = ?", engineTestUser, "episode").First(&h).Error)
	if !h.WatchedAt.UTC().Equal(time.Date(2008, 1, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("episode watched_at = %v, want the air date", h.WatchedAt.UTC())
	}

	ratings := newSessionWithItems(t, db, "ratings", []database.JSONMap{
		{"ids": database.JSONMap{"tmdb": 550}, "media_type": "movie", "rating": 8},
		{"ids": database.JSONMap{"tmdb": 550}, "media_type": "movie", "rating": 8},  // upsert, not a duplicate
		{"ids": database.JSONMap{"tmdb": 550}, "media_type": "movie", "rating": 42}, // out of range -> error
	})
	res, err := ProcessSession(db, ratings.ID)
	if err != nil {
		t.Fatalf("ratings session: %v", err)
	}
	if res.Counts.Added != 2 || res.Counts.Error != 1 {
		t.Errorf("ratings counts = %+v, want added 2 error 1", res.Counts)
	}
	var rating database.UserRating
	mustDo(t, db.Where("user_id = ? AND media_type = ? AND media_id = ?", engineTestUser, "movie", 10).First(&rating).Error)
	if rating.Rating != 8 {
		t.Errorf("rating = %d, want 8", rating.Rating)
	}
}

// TestProcessPendingSessionsWalksTheQueue pins the worker entrypoint: it picks
// up the pending sessions oldest first and reports each outcome.
func TestProcessPendingSessionsWalksTheQueue(t *testing.T) {
	db := setupEngineDB(t)
	seedEngineMedia(t, db)

	first := newSessionWithItems(t, db, "history", []database.JSONMap{
		{"ids": database.JSONMap{"tmdb": 550}, "media_type": "movie"},
	})
	second := newSessionWithItems(t, db, "history", []database.JSONMap{
		{"ids": database.JSONMap{"tmdb": 999}, "media_type": "movie"},
	})

	results, err := ProcessPendingSessions(db, 10)
	if err != nil {
		t.Fatalf("ProcessPendingSessions: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].SessionID != first.ID || results[1].SessionID != second.ID {
		t.Errorf("order = %d,%d want %d,%d", results[0].SessionID, results[1].SessionID, first.ID, second.ID)
	}
	if results[0].Counts.Added != 1 || results[1].Counts.NotFound != 1 {
		t.Errorf("counts = %+v / %+v", results[0].Counts, results[1].Counts)
	}

	// Nothing is left pending, so a second sweep is empty.
	again, err := ProcessPendingSessions(db, 10)
	if err != nil {
		t.Fatalf("second ProcessPendingSessions: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second sweep = %d results, want 0", len(again))
	}
}

// TestParsePayloadRejectsNonObjects guards the engine's poison-tolerance: a
// payload that is not a JSON object is a handled outcome, never a panic.
func TestParsePayloadRejectsNonObjects(t *testing.T) {
	if _, ok := parsePayload(nil); ok {
		t.Error("nil payload should not parse")
	}
	if p, ok := parsePayload(database.JSONMap{"ids": database.JSONMap{"tmdb": 550}}); !ok || p.IDs.TMDB != 550 {
		t.Errorf("valid payload parsed to %+v ok=%v", p, ok)
	}
}
