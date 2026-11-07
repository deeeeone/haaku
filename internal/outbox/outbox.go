package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/obs"

	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// outboxWorkerMetrics instruments the outbox publisher for the metrics listener;
// its heartbeat is what makes a stalled publisher detectable. The name matches
// obs.WorkerOutbox, which is also the HAAKU_WORKER name.
var outboxWorkerMetrics = obs.NewWorker(obs.WorkerOutbox)

// Event type constants — the canonical event names emitted to the outbox.
// Names match the webhook event catalog (plan 03-api/webhooks.md §Event catalog).
const (
	EventHistoryAdded     = "history.added"
	EventHistoryRemoved   = "history.removed"
	EventCollectionAdded  = "collection.updated"
	EventWatchlistAdded   = "watchlist.added"
	EventWatchlistRemoved = "watchlist.removed"
	EventRatingAdded      = "rating.added"
	EventFavoriteAdded    = "favorite.added"
	EventScrobbleStart    = "scrobble.start"
	EventScrobblePause    = "scrobble.pause"
	EventScrobbleStop     = "scrobble.stop"
	EventCheckinCreated   = "checkin.created"
	EventCheckinDeleted   = "checkin.deleted"
	EventUserUpdated      = "user.updated"
	EventFollowCreated    = "follow.created"
	EventFollowApproved   = "follow.approved"
	EventFollowRemoved    = "follow.removed"

	// List events (plan 03-api/lists-api.md §Events).
	EventListCreated      = "list.created"
	EventListUpdated      = "list.updated"
	EventListDeleted      = "list.deleted"
	EventListItemsAdded   = "list.items.added"
	EventListItemsRemoved = "list.items.removed"

	// Account events (plan 03-api/webhooks.md §Event catalog).
	EventUserDeleted = "user.deleted"
)

// PublishEvent inserts an event into the events_outbox table within the
// current transaction. The event will be asynchronously published to
// Redis Streams by the background publisher.
//
// Usage:
//
//	db.Transaction(func(tx *gorm.DB) error {
//	    // ... write domain data ...
//	    return outbox.PublishEvent(tx, outbox.EventHistoryAdded, userID, payload)
//	})
func PublishEvent(db *gorm.DB, eventType string, userID string, payload map[string]any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	event := database.EventsOutbox{
		EventType: eventType,
		UserID:    userID,
		Payload:   payload,
		CreatedAt: time.Now(),
		Published: false,
	}

	// Use raw insert via map to avoid GORM zero-value issues with JSONMap.
	return db.Exec(
		"INSERT INTO events_outbox (event_type, payload, user_id, created_at, published) VALUES (?, ?::jsonb, ?, ?, false)",
		eventType, string(payloadJSON), userID, event.CreatedAt,
	).Error
}

// PublishBatch runs one pass: it fetches the next batch of unpublished events
// from events_outbox, publishes them to the Redis Stream "events", and marks
// them as published.
//
// It is a single pass, not a loop: the worker runtime (internal/workers) owns
// the loop, the heartbeat and shutdown, so the same code can run in-process in
// the API or in a dedicated worker process. The runtime polls about once a
// second, which bounds what a crash loses.
func PublishBatch(ctx context.Context, db *gorm.DB, rdb *goredis.Client) error {
	if db == nil {
		return nil // Database is unavailable; the next pass retries.
	}
	if rdb == nil {
		return nil // Redis is unavailable; the next pass retries.
	}

	var events []database.EventsOutbox
	if err := db.
		Where("published = ?", false).
		Order("created_at ASC").
		Limit(100).
		Find(&events).Error; err != nil {
		return fmt.Errorf("outbox: fetch unpublished events: %w", err)
	}

	if len(events) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var publishedIDs []int64

	for _, event := range events {
		payloadBytes, err := json.Marshal(event.Payload)
		if err != nil {
			log.Printf("[outbox] failed to marshal payload for event %d: %v", event.ID, err)
			continue
		}

		values := map[string]interface{}{
			"event_type": event.EventType,
			"user_id":    event.UserID,
			"payload":    string(payloadBytes),
			"event_id":   event.ID,
			"created_at": event.CreatedAt.Format(time.RFC3339Nano),
		}

		if err := rdb.XAdd(ctx, &goredis.XAddArgs{
			Stream: "events",
			Values: values,
		}).Err(); err != nil {
			log.Printf("[outbox] failed to publish event %d to Redis Stream: %v", event.ID, err)
			outboxWorkerMetrics.Failed(1)
			continue
		}

		publishedIDs = append(publishedIDs, event.ID)
	}

	// Mark published events.
	if len(publishedIDs) > 0 {
		if err := db.Model(&database.EventsOutbox{}).
			Where("id IN ?", publishedIDs).
			Update("published", true).Error; err != nil {
			return fmt.Errorf("outbox: mark %d events published: %w", len(publishedIDs), err)
		}
		log.Printf("[outbox] published %d events to Redis Stream", len(publishedIDs))
		outboxWorkerMetrics.Processed(len(publishedIDs))
	}

	return nil
}
