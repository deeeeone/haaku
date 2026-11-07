package database

import (
	"encoding/json"
	"time"
)

// Per-item outcomes the sync engine records for a submitted item. The set is
// closed: an item that reaches a terminal state is exactly one of these, so a
// client can rely on it (see docs in internal/handlers/syncs/syncs.md).
const (
	// SyncOutcomeAdded: the item resolved and its collection row was written.
	SyncOutcomeAdded = "added"
	// SyncOutcomeNotFound: the item did not resolve to a media row we hold.
	// It is not a failure of the session — the item is reported back so a
	// client can fix and re-submit it. /v1/syncs/{id}/paused serves these.
	SyncOutcomeNotFound = "not_found"
	// SyncOutcomeDuplicate: the item resolved but the collection already had
	// it; nothing was written. /v1/syncs/{id}/skipped serves these.
	SyncOutcomeDuplicate = "duplicate"
	// SyncOutcomeError: a single poisoned item (unparseable payload, a value
	// the write rejected). The engine records it and keeps going; it never
	// fails the whole session.
	SyncOutcomeError = "error"
)

// Session statuses (mirrors the CHECK in 0016_sync_sessions.sql).
const (
	SyncStatusPending    = "pending"
	SyncStatusProcessing = "processing"
	SyncStatusCompleted  = "completed"
	SyncStatusFailed     = "failed"
)

// SyncSessionItem is one item submitted to a batch sync session, persisted so a
// session is resumable and auditable (issue #6). See
// migrations/sql/20261107073214_sync_session_items.sql for the column rationale.
//
// Payload is the submitted item itself — its ids and attributes — kept in the
// submitted form so an item that never resolved can still be re-submitted.
// MediaType/MediaID are filled in once the payload resolves. State is the queue
// state (pending -> processed); Outcome/Error are the terminal resolution.
type SyncSessionItem struct {
	ID          int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID   int64      `gorm:"not null;uniqueIndex:uq_sync_session_items_seq,priority:1" json:"session_id"`
	Seq         int        `gorm:"not null;uniqueIndex:uq_sync_session_items_seq,priority:2" json:"seq"`
	Payload     JSONMap    `gorm:"type:jsonb;not null;default:'{}'" json:"payload"`
	MediaType   *string    `gorm:"type:text" json:"media_type,omitempty"`
	MediaID     *int64     `json:"media_id,omitempty"`
	State       string     `gorm:"type:text;not null;default:pending" json:"state"`
	Outcome     *string    `gorm:"type:text" json:"outcome,omitempty"`
	ErrorCode   *string    `gorm:"type:text" json:"error_code,omitempty"`
	Error       *string    `gorm:"type:text" json:"error,omitempty"`
	WatchedAt   *time.Time `json:"watched_at,omitempty"`
	Rating      *int       `json:"rating,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ProcessedAt *time.Time `json:"processed_at,omitempty"`
}

func (SyncSessionItem) TableName() string { return "sync_session_items" }

// SyncItemError is one item in the typed session.Errors report: the item, the
// machine-readable code, and what went wrong. Code is from a small closed set
// the engine owns (not_found, invalid_payload, write_error) so a worker's
// report is not free text.
type SyncItemError struct {
	ItemID  int64   `json:"item_id"`
	Seq     int     `json:"seq"`
	Code    string  `json:"code"`
	Message string  `json:"message"`
	IDs     JSONMap `json:"ids,omitempty"`
}

// SyncErrorCounts tallies the outcomes of a session's items.
type SyncErrorCounts struct {
	Added     int `json:"added"`
	NotFound  int `json:"not_found"`
	Duplicate int `json:"duplicate"`
	Error     int `json:"error"`
}

// SyncErrors is the typed shape of sync_sessions.errors (issue #6): the object
// the engine — a worker, not the API process — writes when a session finishes.
// It replaced an untyped map that nothing ever wrote. Counts is the summary a
// client renders; Items names every item that did not land (not_found + error,
// capped by the engine — duplicates are not failures and are served by
// /v1/syncs/{id}/skipped).
type SyncErrors struct {
	Counts SyncErrorCounts `json:"counts"`
	Items  []SyncItemError `json:"items"`
}

// MarshalJSON keeps the wire shape stable: Items is always an array (never
// null), so a client can iterate it without a nil check.
func (e SyncErrors) MarshalJSON() ([]byte, error) {
	items := e.Items
	if items == nil {
		items = []SyncItemError{}
	}
	type alias SyncErrors
	return json.Marshal(alias{Counts: e.Counts, Items: items})
}

// SyncEngineModels returns the models this engine owns, for AutoMigrate in
// tests. Production schema is owned by the SQL migrations, never by GORM.
func SyncEngineModels() []interface{} {
	return []interface{}{&SyncSession{}, &SyncSessionItem{}}
}
