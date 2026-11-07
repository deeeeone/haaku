package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"trakt-api/internal/obs"

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

// webhookWorkerMetrics instruments this worker for the metrics listener: a
// heartbeat so a dead worker is detectable (the gauge stops advancing) plus
// delivery outcomes. The name matches obs.WorkerWebhooks, which is also the
// TKILLER_WORKER selection name.
var webhookWorkerMetrics = obs.NewWorker(obs.WorkerWebhooks)

// DeliverWebhooks runs one delivery pass: it reads a batch of events from the
// Redis Stream "events" via the "webhooks" consumer group and POSTs each one to
// the matching endpoints, retrying with backoff and disabling an endpoint after
// too many consecutive failures.
//
// It is a single pass, not a loop: the worker runtime (internal/workers) owns
// the loop, the heartbeat and the shutdown, so the same code can run
// in-process in the API or in a dedicated worker process. A pass that finds
// nothing returns nil.
func DeliverWebhooks(ctx context.Context, db *gorm.DB, rdb *goredis.Client) error {
	if rdb == nil {
		return nil // Redis is unavailable; the next pass retries.
	}
	if db == nil {
		return nil // Database is unavailable; the next pass retries.
	}
	ensureConsumerGroup(ctx, rdb)
	return processWebhookEvents(ctx, db, rdb)
}

// ensureConsumerGroup creates the consumer group for webhooks if it doesn't exist.
func ensureConsumerGroup(ctx context.Context, rdb *goredis.Client) {
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := rdb.XGroupCreateMkStream(readCtx, streamName, consumerGroup, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		log.Printf("[webhook-worker] failed to create consumer group: %v", err)
	}
}

// processWebhookEvents reads pending/new events from Redis Stream and delivers webhooks.
func processWebhookEvents(ctx context.Context, db *gorm.DB, rdb *goredis.Client) error {
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	streams, err := rdb.XReadGroup(readCtx, &goredis.XReadGroupArgs{
		Group:    consumerGroup,
		Consumer: consumerName,
		Streams:  []string{streamName, ">"},
		Count:    50,
		Block:    1 * time.Second,
	}).Result()

	if err != nil {
		if err != goredis.Nil {
			return fmt.Errorf("webhook worker XReadGroup: %w", err)
		}
		return nil
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			handleWebhookEvent(ctx, db, msg)
			_ = rdb.XAck(readCtx, streamName, consumerGroup, msg.ID).Err()
		}
	}
	return nil
}

// handleWebhookEvent processes a single Redis Stream message, finds matching
// webhook endpoints, and delivers the event.
func handleWebhookEvent(ctx context.Context, db *gorm.DB, msg goredis.XMessage) {
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
		if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
			// A malformed payload is dropped rather than half-applied; the
			// delivery below falls back to an empty map.
			log.Printf("[webhook-worker] discarding unparseable outbox payload: %v", err)
			payload = nil
		}
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
		deliverWithRetries(ctx, db, &ep, eventID, canonical, body)
	}
}

// deliverWithRetries attempts to POST the webhook payload with exponential backoff.
func deliverWithRetries(ctx context.Context, db *gorm.DB, ep *database.WebhookEndpoint, eventID, eventType, body string) {
	var lastCode int
	var lastBody string
	var lastDuration int

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		code, respBody, durMs, err := doWebhookPost(ctx, ep, body)

		lastCode = code
		lastBody = respBody
		lastDuration = durMs

		if err == nil && code >= 200 && code < 300 {
			recordDelivery(db, ep.ID, eventID, eventType, "success", attempt, &code, nil, &durMs)
			webhookWorkerMetrics.Processed(1)
			return
		}

		// 429: slow down but don't count as failure.
		if code == 429 {
			recordDelivery(db, ep.ID, eventID, eventType, "retrying", attempt, &code, &respBody, &durMs)
			if !sleepCtx(ctx, 10*time.Second) {
				return
			}
			continue
		}

		// Non-2xx or network error — backoff.
		if attempt < maxAttempts {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * backoffBase
			log.Printf("[webhook-worker] delivery attempt %d for endpoint %d failed with %d, retrying in %v",
				attempt, ep.ID, code, backoff)
			if !sleepCtx(ctx, backoff) {
				return
			}
		}
	}

	// All attempts exhausted — mark as failed.
	recordDelivery(db, ep.ID, eventID, eventType, "failed", maxAttempts, &lastCode, &lastBody, &lastDuration)
	webhookWorkerMetrics.Failed(1)

	// Check consecutive failures.
	consecFailures := countConsecutiveFailures(db, ep.ID)
	if consecFailures >= maxConsecutiveFailures {
		log.Printf("[webhook-worker] endpoint %d has %d consecutive failures — disabling", ep.ID, consecFailures)
		db.Model(ep).Update("active", false)
	}
}

// doWebhookPost sends the payload via HTTP POST to the webhook endpoint.
func doWebhookPost(ctx context.Context, ep *database.WebhookEndpoint, body string) (int, string, int, error) {
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
	secret, err := webhooks.DecryptSecret(ep.ID, ep.SecretHash)
	if err != nil {
		log.Printf("[webhook-worker] endpoint %d: %v", ep.ID, err)
		return 0, "", 0, fmt.Errorf("cannot decrypt webhook secret: %w", err)
	}
	if secret == "" {
		// An endpoint with no stored secret cannot be signed. Refuse rather than
		// sign with an empty key (a row whose secret was never written).
		log.Printf("[webhook-worker] endpoint %d has no stored secret", ep.ID)
		return 0, "", 0, errors.New("webhook endpoint has no stored secret")
	}
	timestamp := time.Now().Unix()
	sig := webhooks.GenerateHMAC(secret, timestamp, body)
	sigHeader := fmt.Sprintf("t=%d,v1=%s", timestamp, sig)

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, "POST", ep.URL, bytes.NewBufferString(body))
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
	defer func() { _ = resp.Body.Close() }()

	respBuf := new(bytes.Buffer)
	// A truncated read is still reported to the caller as the response body.
	_, _ = respBuf.ReadFrom(resp.Body)
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
		if s == value {
			return true
		}
	}
	return false
}

// sleepCtx waits for d or until ctx is done, reporting whether the wait
// completed. It replaces a bare time.Sleep so a worker pass yields promptly on
// shutdown instead of holding the process open for a backoff interval.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
