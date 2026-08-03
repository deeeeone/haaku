package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"trakt-api/redis"

	"gorm.io/gorm"
)

// CacheRetryConfig holds retry configuration for cache operations.
type CacheRetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// Default cache retry configuration.
var DefaultCacheRetryConfig = CacheRetryConfig{
	MaxRetries: 3,
	BaseDelay:  100 * time.Millisecond,
	MaxDelay:   2 * time.Second,
}

// CacheOperation represents a single cache mutation.
type CacheOperation struct {
	Type     string        // "invalidate", "set"
	Key      string        // Cache key
	Data     interface{}   // Value for set operations
	TTL      time.Duration // TTL for set operations
	Metadata interface{}   // Diagnostic context (media IDs, user IDs, etc.)
}

// CacheTransaction collects cache operations to be executed atomically
// (best-effort) after a database transaction commits.
type CacheTransaction struct {
	operations []CacheOperation
	mu         sync.Mutex
}

// NewCacheTransaction returns an empty cache transaction.
func NewCacheTransaction() *CacheTransaction {
	return &CacheTransaction{
		operations: make([]CacheOperation, 0),
	}
}

// AddInvalidation appends a cache invalidation to the transaction.
// metadata carries diagnostic context (e.g. media Type/ID) for logging failures.
func (ct *CacheTransaction) AddInvalidation(key string, metadata interface{}) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.operations = append(ct.operations, CacheOperation{
		Type:     "invalidate",
		Key:      key,
		Metadata: metadata,
	})
}

// AddSetOperation appends a cache set to the transaction.
func (ct *CacheTransaction) AddSetOperation(key string, data interface{}, ttl time.Duration) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.operations = append(ct.operations, CacheOperation{
		Type: "set",
		Key:  key,
		Data: data,
		TTL:  ttl,
	})
}

// Execute runs all queued cache operations. Returns an error only if ALL
// operations failed; individual failures are logged but don't block the caller.
func (ct *CacheTransaction) Execute() error {
	ct.mu.Lock()
	ops := make([]CacheOperation, len(ct.operations))
	copy(ops, ct.operations)
	ct.mu.Unlock()

	// Split operations by type.
	var invalidationOps, setOps []CacheOperation
	for _, op := range ops {
		switch op.Type {
		case "invalidate":
			invalidationOps = append(invalidationOps, op)
		case "set":
			setOps = append(setOps, op)
		}
	}

	var errors []error

	if len(invalidationOps) > 0 {
		if err := ct.executeBatchInvalidations(invalidationOps); err != nil {
			log.Printf("[cache] batch invalidation failed: %v", err)
			errors = append(errors, err)
		}
	}

	for _, op := range setOps {
		if err := ct.executeSet(op); err != nil {
			log.Printf("[cache] set operation failed (key=%s): %v", op.Key, err)
			errors = append(errors, err)
		}
	}

	if len(errors) == len(ops) && len(ops) > 0 {
		return fmt.Errorf("all %d cache operations failed", len(ops))
	}

	return nil
}

// executeBatchInvalidations groups invalidations and executes them efficiently.
// For media responses, it bumps the version counter (O(1) INCR) instead of
// scanning and deleting all matching keys.
func (ct *CacheTransaction) executeBatchInvalidations(operations []CacheOperation) error {
	// Collect keys for direct DEL and version bumps.
	var delKeys []string
	var versionKeys []string

	for _, op := range operations {
		delKeys = append(delKeys, op.Key)

		// If metadata contains a version key hint, queue a version bump.
		if meta, ok := op.Metadata.(map[string]interface{}); ok {
			if vk, ok := meta["version_key"].(string); ok && vk != "" {
				versionKeys = append(versionKeys, vk)
			}
		}
	}

	// Batch delete all keys via pipeline.
	if len(delKeys) > 0 {
		if err := redis.InvalidateMultipleKeysPipeline(delKeys); err != nil {
			return fmt.Errorf("batch DEL failed: %w", err)
		}
	}

	// Bump version counters (these are INCR calls, each atomic).
	for _, vk := range versionKeys {
		if err := bumpKey(vk); err != nil {
			log.Printf("[cache] version bump failed for %s: %v", vk, err)
		}
	}

	return nil
}

// bumpKey increments a key in Redis, ignoring the return value.
// Used for version counter bumps.
func bumpKey(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), redis.RedisDefaultTimeout)
	defer cancel()
	if _, err := redis.Incr(ctx, key); err != nil {
		// Redis not initialized or other error — non-fatal.
		return err
	}
	return nil
}

// executeSet marshals the data to JSON, validates size, and sends to Redis.
func (ct *CacheTransaction) executeSet(op CacheOperation) error {
	if op.Data == nil {
		return fmt.Errorf("no data for set operation on key %s", op.Key)
	}

	data, err := json.Marshal(op.Data)
	if err != nil {
		return fmt.Errorf("json marshal failed (key=%s): %w", op.Key, err)
	}

	if len(data) > redis.RedisMaxCacheEntrySize {
		return fmt.Errorf("entry size %d exceeds max %d (key=%s)",
			len(data), redis.RedisMaxCacheEntrySize, op.Key)
	}

	return redis.SetCacheData(op.Key, data, op.TTL)
}

// ---------------------------------------------------------------------------
// WithCacheTransaction — DB transaction + async cache ops
// ---------------------------------------------------------------------------

// WithCacheTransaction executes a database transaction first. After commit,
// the returned CacheTransaction's operations run asynchronously with retry.
// This keeps response times fast — cache is eventually consistent.
func WithCacheTransaction(dbFn func(tx *gorm.DB) (*CacheTransaction, error)) error {
	var cacheTx *CacheTransaction

	// Execute the database transaction synchronously.
	err := withTransactionDB(dbFn, &cacheTx)
	if err != nil {
		return err
	}

	// Run cache operations asynchronously.
	if cacheTx != nil {
		cacheTx.mu.Lock()
		ops := make([]CacheOperation, len(cacheTx.operations))
		copy(ops, cacheTx.operations)
		cacheTx.mu.Unlock()

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			tempTx := &CacheTransaction{operations: ops}
			if cacheErr := ExecuteCacheWithRetry(ctx, tempTx, DefaultCacheRetryConfig); cacheErr != nil {
				logCacheFailure(tempTx, cacheErr)
				retryCacheOperations(ctx, tempTx)
			}
		}()
	}

	return nil
}

// withTransactionDB is the DB integration point — it calls the database
// package's WithTransaction helper. This avoids a circular import because
// the database package imports this cache package.
//
// The actual implementation lives in internal/database/tx_cache.go which
// provides this function. We use a package-level variable so the database
// package can register it without creating a circular dependency.
var withTransactionDB = func(fn func(tx *gorm.DB) (*CacheTransaction, error), cacheTx **CacheTransaction) error {
	return fmt.Errorf("cache: WithTransaction bridge not registered — call cache.RegisterDBTx from database package")
}

// RegisterDBTx is called by internal/database during init to wire up the
// DB transaction bridge. This breaks the circular dependency between
// database → cache → database.
func RegisterDBTx(fn func(func(tx *gorm.DB) (*CacheTransaction, error), **CacheTransaction) error) {
	withTransactionDB = fn
}

// ---------------------------------------------------------------------------
// ExecuteCacheWithRetry
// ---------------------------------------------------------------------------

// ExecuteCacheWithRetry runs cache operations with exponential backoff,
// respecting context cancellation.
func ExecuteCacheWithRetry(ctx context.Context, cacheTx *CacheTransaction, config CacheRetryConfig) error {
	var err error
	delay := config.BaseDelay

	for i := 0; i <= config.MaxRetries; i++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("cache operation cancelled after %d attempts: %w", i, ctxErr)
		}

		err = cacheTx.Execute()
		if err == nil {
			return nil
		}

		if i < config.MaxRetries {
			log.Printf("[cache] operation failed (attempt %d/%d): %v, retrying in %v",
				i+1, config.MaxRetries+1, err, delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return fmt.Errorf("cache operation cancelled during backoff after %d attempts: %w",
					i+1, ctx.Err())
			}
			delay *= 2
			if delay > config.MaxDelay {
				delay = config.MaxDelay
			}
		}
	}

	return fmt.Errorf("cache operations failed after %d retries: %w", config.MaxRetries, err)
}

// ---------------------------------------------------------------------------
// Failure logging and background compensation
// ---------------------------------------------------------------------------

// logCacheFailure emits a structured CRITICAL log with extracted IDs
// for manual triage.
func logCacheFailure(cacheTx *CacheTransaction, err error) {
	var mediaIDs []int64
	var userIDs []string
	var keys []string

	for _, op := range cacheTx.operations {
		keys = append(keys, op.Key)
		if meta, ok := op.Metadata.(map[string]interface{}); ok {
			if id, ok := meta["media_id"].(int64); ok && id != 0 {
				mediaIDs = append(mediaIDs, id)
			}
			if uid, ok := meta["user_id"].(string); ok && uid != "" {
				userIDs = append(userIDs, uid)
			}
		}
	}

	mediaIDs = uniqueInt64s(mediaIDs)
	userIDs = uniqueStrs(userIDs)

	log.Printf("[cache] CRITICAL: operations failed after successful DB commit. "+
		"err=%v mediaIDs=%v userIDs=%v keys=%v opCount=%d",
		err, mediaIDs, userIDs, keys, len(cacheTx.operations))
}

// retryCacheOperations attempts a background retry with a more aggressive config
// (8 attempts over ~30s). This is a best-effort compensation for cache drift.
func retryCacheOperations(ctx context.Context, cacheTx *CacheTransaction) {
	config := CacheRetryConfig{
		MaxRetries: 8,
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   5 * time.Second,
	}

	if err := ExecuteCacheWithRetry(ctx, cacheTx, config); err != nil {
		log.Printf("[cache] background retry exhausted: %v", err)
	} else {
		log.Printf("[cache] background retry succeeded — consistency restored")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func uniqueInt64s(slice []int64) []int64 {
	seen := make(map[int64]bool)
	var out []int64
	for _, v := range slice {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func uniqueStrs(slice []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, v := range slice {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}