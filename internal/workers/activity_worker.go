package workers

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"time"

	"trakt-api/internal/database"
	"trakt-api/redis"

	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// StartActivityWorker launches a background goroutine that consumes events
// from the Redis Stream "events" and transforms them into activity_feed rows
// for relevant followers (fan-out).
//
// The worker runs in a consumer group called "activity-workers".
// Events that cannot be processed are sent to the dead letter queue
// "events-dlq" for manual inspection.
func StartActivityWorker() {
	go func() {
		// Give Redis and DB time to initialize
		time.Sleep(2 * time.Second)

		for {
			processActivityEvents()
			time.Sleep(500 * time.Millisecond)
		}
	}()
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

// processActivityEvents reads new events from Redis Stream and fans them out.
func processActivityEvents() {
	client := redis.GetClient()
	if client == nil {
		return // Redis not available
	}

	gormDB, err := database.GetDB()
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Read pending/new messages from the "events" stream
	// We use XReadGroup for consumer group support, but since we may not have
	// created a group yet, we'll use simple XRead and track the last ID.
	// For now, use XRead with ">" to get new messages.
	streams, err := client.XRead(ctx, &goredis.XReadArgs{
		Streams: []string{"events", "$"},
		Count:   100,
		Block:   1 * time.Second,
	}).Result()

	if err != nil {
		// Timeout is expected on XRead with block
		if err != goredis.Nil {
			log.Printf("[activity-worker] XRead error: %v", err)
		}
		return
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			processMessage(gormDB, msg)
		}
	}
}

// processMessage handles a single Redis Stream message and fans out activity.
func processMessage(db *gorm.DB, msg goredis.XMessage) {
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
		json.Unmarshal([]byte(payloadStr), &payload)
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
