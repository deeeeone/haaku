package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/handlers/webhooks"
	"trakt-api/redis"

	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	maxAttempts            = 5
	connectTimeout         = 10 * time.Second
	totalTimeout           = 30 * time.Second
	backoffBase            = 1 * time.Minute
	maxConsecutiveFailures = 5
	consumerGroup          = "webhooks"
	streamName             = "events"
	consumerName           = "webhook-worker-1"
)

// StartWebhookWorker launches a background goroutine that consumes events
// from the Redis Stream "events" and delivers them to matching webhook endpoints.
//
// Uses a separate consumer group from the activity worker (group "webhooks").
func StartWebhookWorker() {
	go func() {
		time.Sleep(2 * time.Second)

		setupConsumerGroup()

		for {
			processWebhookEvents()
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

// setupConsumerGroup creates the consumer group for webhooks if it doesn't exist.
func setupConsumerGroup() {
	client := redis.GetClient()
	if client == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.XGroupCreateMkStream(ctx, streamName, consumerGroup, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		log.Printf("[webhook-worker] failed to create consumer group: %v", err)
	}
}

// processWebhookEvents reads pending/new events from Redis Stream and delivers webhooks.
func processWebhookEvents() {
	client := redis.GetClient()
	if client == nil {
		return
	}

	gormDB, err := database.GetDB()
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	streams, err := client.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group:    consumerGroup,
		Consumer: consumerName,
		Streams:  []string{streamName, ">"},
		Count:    50,
		Block:    1 * time.Second,
	}).Result()

	if err != nil {
		if err != goredis.Nil {
			log.Printf("[webhook-worker] XReadGroup error: %v", err)
		}
		return
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			handleWebhookEvent(gormDB, msg)
			client.XAck(ctx, streamName, consumerGroup, msg.ID)
		}
	}
}

// handleWebhookEvent processes a single Redis Stream message, finds matching
// webhook endpoints, and delivers the event.
func handleWebhookEvent(db *gorm.DB, msg goredis.XMessage) {
	eventType, _ := msg.Values["event_type"].(string)
	if eventType == "" {
		return
	}

	// Extract event ID from message or generate one.
	eventID := ""
	if id, ok := msg.Values["event_id"].(string); ok {
		eventID = id
	}
	if idNum, ok := msg.Values["event_id"].(int64); ok {
		eventID = fmt.Sprintf("%d", idNum)
	}

	payloadStr, _ := msg.Values["payload"].(string)

	var payload map[string]interface{}
	if payloadStr != "" {
		json.Unmarshal([]byte(payloadStr), &payload)
	}
	if payload == nil {
		payload = make(map[string]interface{})
	}

	// Canonicalize event type for delivery.
	canonical, ok := webhooks.EventCatalogV1[eventType]
	if !ok {
		canonical = eventType
	}

	// Find all active webhook endpoints subscribed to this event type.
	var endpoints []database.WebhookEndpoint
	if err := db.Where("active = ?", true).
		Find(&endpoints).Error; err != nil {
		log.Printf("[webhook-worker] failed to query endpoints: %v", err)
		return
	}

	for _, ep := range endpoints {
		if !stringSliceContains(ep.Events, canonical) && !stringSliceContains(ep.Events, eventType) {
			continue
		}

		// Build the payload.
		whPayload := webhooks.NewWebhookPayload(canonical, payload)

		bodyBytes, err := json.Marshal(whPayload)
		if err != nil {
			log.Printf("[webhook-worker] failed to marshal payload: %v", err)
			continue
		}
		body := string(bodyBytes)

		// Deliver with retries.
		deliverWithRetries(db, &ep, eventID, canonical, body)
	}
}

// deliverWithRetries attempts to POST the webhook payload with exponential backoff.
func deliverWithRetries(db *gorm.DB, ep *database.WebhookEndpoint, eventID, eventType, body string) {
	var lastCode int
	var lastBody string
	var lastDuration int

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		code, respBody, durMs, err := doWebhookPost(ep, body)

		lastCode = code
		lastBody = respBody
		lastDuration = durMs

		if err == nil && code >= 200 && code < 300 {
			recordDelivery(db, ep.ID, eventID, eventType, "success", attempt, &code, nil, &durMs)
			return
		}

		// 429: slow down but don't count as failure.
		if code == 429 {
			recordDelivery(db, ep.ID, eventID, eventType, "retrying", attempt, &code, &respBody, &durMs)
			time.Sleep(10 * time.Second)
			continue
		}

		// Non-2xx or network error — backoff.
		if attempt < maxAttempts {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * backoffBase
			log.Printf("[webhook-worker] delivery attempt %d for endpoint %d failed with %d, retrying in %v",
				attempt, ep.ID, code, backoff)
			time.Sleep(backoff)
		}
	}

	// All attempts exhausted — mark as failed.
	recordDelivery(db, ep.ID, eventID, eventType, "failed", maxAttempts, &lastCode, &lastBody, &lastDuration)

	// Check consecutive failures.
	consecFailures := countConsecutiveFailures(db, ep.ID)
	if consecFailures >= maxConsecutiveFailures {
		log.Printf("[webhook-worker] endpoint %d has %d consecutive failures — disabling", ep.ID, consecFailures)
		db.Model(ep).Update("active", false)
	}
}

// doWebhookPost sends the payload via HTTP POST to the webhook endpoint.
func doWebhookPost(ep *database.WebhookEndpoint, body string) (int, string, int, error) {
	// SSRF guard: resolve to a verified public IP and PIN it at dial time.
	// Re-resolving in the transport would let a DNS-rebinding attacker swap in
	// an internal address after the check.
	parsed, err := url.Parse(ep.URL)
	if err != nil {
		return 0, "", 0, fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return 0, "", 0, fmt.Errorf("URL must use HTTPS")
	}
	if parsed.User != nil {
		return 0, "", 0, fmt.Errorf("URL must not include userinfo")
	}

	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	pinnedIP, err := webhooks.ResolvePublicIP(host)
	if err != nil {
		return 0, "", 0, fmt.Errorf("SSRF blocked: %w", err)
	}
	dialAddr := net.JoinHostPort(pinnedIP.String(), port)

	// HMAC signing with the recoverable (decrypted) secret, so consumers can
	// verify with the plaintext shown once at creation/rotation.
	secret, err := webhooks.DecryptSecret(ep.SecretHash)
	if err != nil {
		log.Printf("[webhook-worker] endpoint %d: %v", ep.ID, err)
		return 0, "", 0, fmt.Errorf("cannot decrypt webhook secret: %w", err)
	}
	timestamp := time.Now().Unix()
	sig := webhooks.GenerateHMAC(secret, timestamp, body)
	sigHeader := fmt.Sprintf("t=%d,v1=%s", timestamp, sig)

	start := time.Now()

	req, err := http.NewRequest("POST", ep.URL, bytes.NewBufferString(body))
	if err != nil {
		return 0, "", 0, fmt.Errorf("request creation failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TK-Signature", sigHeader)
	req.Header.Set("User-Agent", "Trakt-Killer-Webhooks/1.0")

	client := &http.Client{
		Timeout: totalTimeout,
		// Never follow redirects: a redirect to an internal URL would be
		// dialed through the pinned transport to the wrong (public) host, and
		// following it reopens the SSRF surface. 3xx is treated as a delivery
		// failure like any non-2xx.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{
					Timeout: connectTimeout,
				}).DialContext(ctx, network, dialAddr)
			},
		},
	}

	resp, err := client.Do(req)
	durMs := int(time.Since(start).Milliseconds())

	if err != nil {
		return 0, err.Error(), durMs, err
	}
	defer resp.Body.Close()

	respBuf := new(bytes.Buffer)
	respBuf.ReadFrom(resp.Body)
	respStr := respBuf.String()
	if len(respStr) > 1024 {
		respStr = respStr[:1024]
	}

	return resp.StatusCode, respStr, durMs, nil
}

// recordDelivery writes a webhook_deliveries row.
func recordDelivery(db *gorm.DB, endpointID int64, eventID, eventType, status string, attempts int, code *int, respBody *string, durMs *int) {
	delivery := database.WebhookDelivery{
		EndpointID: endpointID,
		EventID:    eventID,
		EventType:  eventType,
		Status:     status,
		Attempts:   attempts,
		DurationMs: durMs,
		CreatedAt:  time.Now().UTC(),
	}

	if code != nil {
		delivery.ResponseCode = code
	}
	if respBody != nil {
		delivery.ResponseBody = *respBody
	}

	if err := db.Create(&delivery).Error; err != nil {
		log.Printf("[webhook-worker] failed to record delivery: %v", err)
	}
}

// countConsecutiveFailures counts how many consecutive deliveries have failed.
func countConsecutiveFailures(db *gorm.DB, endpointID int64) int {
	var deliveries []database.WebhookDelivery
	db.Where("endpoint_id = ?", endpointID).
		Order("created_at DESC").
		Limit(maxConsecutiveFailures + 5).
		Find(&deliveries)

	count := 0
	for _, d := range deliveries {
		if d.Status == "failed" {
			count++
		} else {
			break
		}
	}
	return count
}

// stringSliceContains checks if a StringSlice contains a value.
func stringSliceContains(slice database.StringSlice, value string) bool {
	for _, s := range slice {
		if string(s) == value {
			return true
		}
	}
	return false
}
