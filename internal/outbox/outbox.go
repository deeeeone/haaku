package outbox

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/obs"
	"trakt-api/redis"

	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// outboxWorkerMetrics instruments the outbox publisher for the metrics listener;
// its heartbeat is what makes a stalled publisher detectable.
var outboxWorkerMetrics = obs.NewWorker("outbox_publisher")

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

// StartPublisher launches a background goroutine that polls the events_outbox
// table for unpublished events and publishes them to the Redis Stream "events".
//
// Polling interval: 1 second.
// Events are marked as published after successful delivery.
//
// Returns immediately; the goroutine runs until the process exits.
func StartPublisher(rdb interface{}) {
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			outboxWorkerMetrics.Heartbeat()
			publishBatch()
		}
	}()
}

// publishBatch fetches the next batch of unpublished events, publishes them
// to Redis Streams, and marks them as published.
func publishBatch() {
	gormDB, err := database.GetDB()
	if err != nil {
		return
	}

	var events []database.EventsOutbox
	if err := gormDB.
		Where("published = ?", false).
		Order("created_at ASC").
		Limit(100).
		Find(&events).Error; err != nil {
		log.Printf("[outbox] failed to fetch events: %v", err)
		return
	}

	if len(events) == 0 {
		return
	}

	client := redis.GetClient()
	if client == nil {
		return // Redis not available — wait for next tick
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

		if err := client.XAdd(ctx, &goredis.XAddArgs{
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
		if err := gormDB.Model(&database.EventsOutbox{}).
			Where("id IN ?", publishedIDs).
			Update("published", true).Error; err != nil {
			log.Printf("[outbox] failed to mark events as published: %v", err)
		} else {
			log.Printf("[outbox] published %d events to Redis Stream", len(publishedIDs))
			outboxWorkerMetrics.Processed(len(publishedIDs))
		}
	}
}
