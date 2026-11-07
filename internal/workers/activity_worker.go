package workers

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/obs"

	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// activityWorkerMetrics instruments the activity worker for the metrics listener.
// The name matches obs.WorkerActivity, which is also the HAAKU_WORKER name.
var activityWorkerMetrics = obs.NewWorker(obs.WorkerActivity)

// FanOutActivity runs one pass: it reads new events from the Redis Stream
// "events" and turns each into activity_feed rows for the actor's followers.
//
// It is a single pass, not a loop: the worker runtime (internal/workers) owns
// the loop, the heartbeat and shutdown, so the same code can run in-process in
// the API or in a dedicated worker process. A pass that finds nothing returns
// nil.
//
// The read is a plain XRead from "$", so it observes only events published
// after the call; a message that arrives between passes is not fanned out. That
// is the fan-out lane's own bug to fix (it is not the topology's), and it is
// why the topology must keep this worker owned by exactly one process: two
// readers on "$" would both see — and both apply — every new event.
func FanOutActivity(ctx context.Context, db *gorm.DB, rdb *goredis.Client) error {
	if rdb == nil {
		return nil // Redis is unavailable; the next pass retries.
	}
	if db == nil {
		return nil // Database is unavailable; the next pass retries.
	}

	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	streams, err := rdb.XRead(readCtx, &goredis.XReadArgs{
		Streams: []string{"events", "$"},
		Count:   100,
		Block:   1 * time.Second,
	}).Result()

	if err != nil {
		// A blocking XRead that times out is the steady state, not a failure.
		if err != goredis.Nil && readCtx.Err() == nil {
			return err
		}
		return nil
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			processActivityMessage(db, msg)
		}
	}
	return nil
}

// Event types we care about for activity fan-out.
var activityEventTypes = map[string]bool{
	"history.added":      true,
	"collection.updated": true,
	"watchlist.added":    true,
	"watchlist.removed":  true,
	"rating.added":       true,
	"favorite.added":     true,
	"scrobble.start":     true,
	"scrobble.pause":     true,
	"scrobble.stop":      true,
	"checkin.created":    true,
	"checkin.deleted":    true,
	"list.created":       true,
	"list.updated":       true,
	"list.deleted":       true,
	"list.items.added":   true,
	"list.items.removed": true,
}

// processActivityMessage handles a single Redis Stream message and fans out activity.
func processActivityMessage(db *gorm.DB, msg goredis.XMessage) {
	eventType, _ := msg.Values["event_type"].(string)
	if !activityEventTypes[eventType] {
		// Not an activity-generating event
		return
	}

	actorID, _ := msg.Values["user_id"].(string)
	if actorID == "" {
		return
	}

	// Parse payload
	payloadStr, _ := msg.Values["payload"].(string)
	var payload map[string]interface{}
	if payloadStr != "" {
		if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
			// A malformed payload is dropped rather than half-applied; the
			// caller falls back to an empty map below.
			log.Printf("[activity-worker] discarding unparseable outbox payload: %v", err)
			payload = nil
		}
	}
	if payload == nil {
		payload = make(map[string]interface{})
	}

	// Extract media info from payload
	var mediaType string
	var mediaID *int64
	if mt, ok := payload["media_type"].(string); ok {
		mediaType = mt
	}
	if mid, ok := payload["media_id"]; ok {
		switch v := mid.(type) {
		case float64:
			id := int64(v)
			mediaID = &id
		case string:
			id, err := strconv.ParseInt(v, 10, 64)
			if err == nil {
				mediaID = &id
			}
		}
	}

	// Fan out to followers: find all users who follow the actor
	followers, err := getFollowers(db, actorID)
	if err != nil {
		log.Printf("[activity-worker] failed to get followers for %s: %v", actorID, err)
		activityWorkerMetrics.Failed(1)
		return
	}

	// Create activity entries for each follower
	for _, followerID := range followers {
		entry := database.ActivityFeed{
			ActorID:   actorID,
			EventType: eventType,
			MediaType: mediaType,
			MediaID:   mediaID,
			Payload:   database.JSONMap(payload),
			CreatedAt: time.Now(),
		}

		if err := db.Create(&entry).Error; err != nil {
			log.Printf("[activity-worker] failed to create activity entry for follower %s: %v", followerID, err)
		}
	}

	if len(followers) > 0 {
		log.Printf("[activity-worker] fanned out %s from %s to %d followers", eventType, actorID, len(followers))
	}
	activityWorkerMetrics.Processed(1)
}

// getFollowers returns the list of user IDs who follow the given actor.
func getFollowers(db *gorm.DB, actorID string) ([]string, error) {
	var follows []database.UserFollow
	if err := db.Where("following_id = ?", actorID).Find(&follows).Error; err != nil {
		return nil, err
	}
	followers := make([]string, len(follows))
	for i, f := range follows {
		followers[i] = f.FollowerID
	}
	return followers, nil
}
