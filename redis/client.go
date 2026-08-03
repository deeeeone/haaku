package redis

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Global Redis client (singleton, initialized once).
var (
	rdb   *redis.Client
	rOnce sync.Once
)

// InitRedis parses REDIS_URL, creates the go-redis client, pings to verify,
// and stores the singleton. Safe to call multiple times (sync.Once).
func InitRedis() error {
	var err error
	rOnce.Do(func() {
		redisURL := os.Getenv("REDIS_URL")
		if redisURL == "" {
			redisURL = "redis://localhost:6379"
		}

		opts, parseErr := redis.ParseURL(redisURL)
		if parseErr != nil {
			err = fmt.Errorf("invalid REDIS_URL: %w", parseErr)
			return
		}

		// Configure connection pooling for resilience.
		opts.DB = getRedisDB()
		opts.PoolSize = 100
		opts.MinIdleConns = 10
		opts.MaxRetries = 3
		opts.PoolTimeout = 4 * time.Second
		opts.ConnMaxIdleTime = 5 * time.Minute
		opts.ConnMaxLifetime = 30 * time.Minute

		log.Printf("[redis] connecting to DB %d, pool=%d", opts.DB, opts.PoolSize)

		rdb = redis.NewClient(opts)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if pingErr := rdb.Ping(ctx).Err(); pingErr != nil {
			err = fmt.Errorf("redis ping failed: %w", pingErr)
			rdb = nil
			return
		}

		log.Println("[redis] connected")
	})
	return err
}

// getRedisDB returns the Redis DB number from REDIS_DB env, default 0.
func getRedisDB() int {
	if v := os.Getenv("REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

// GetClient returns the Redis client singleton, or nil if not initialized.
func GetClient() *redis.Client {
	return rdb
}

// CloseRedis shuts down the Redis connection.
func CloseRedis() error {
	if rdb != nil {
		return rdb.Close()
	}
	return nil
}

// requireClient returns the Redis client if initialized, or an error.
func requireClient() (*redis.Client, error) {
	if rdb == nil {
		return nil, fmt.Errorf("redis not initialized — call InitRedis first")
	}
	return rdb, nil
}

// isClientNil returns true if Redis is not initialized.
func isClientNil() bool { return rdb == nil }

// ---------------------------------------------------------------------------
// Circuit-breaker-wrapped core operations
// ---------------------------------------------------------------------------

// Set wraps client.Set with circuit breaker.
func Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return globalCircuitBreaker.Call(func() error {
		return rdb.Set(ctx, key, value, expiration).Err()
	})
}

// Get wraps client.Get with circuit breaker.
func Get(ctx context.Context, key string) (string, error) {
	var val string
	err := globalCircuitBreaker.Call(func() error {
		var getErr error
		val, getErr = rdb.Get(ctx, key).Result()
		return getErr
	})
	return val, err
}

// Del wraps client.Del with circuit breaker.
func Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return globalCircuitBreaker.Call(func() error {
		return rdb.Del(ctx, keys...).Err()
	})
}

// Incr wraps client.Incr with circuit breaker.
func Incr(ctx context.Context, key string) (int64, error) {
	var val int64
	err := globalCircuitBreaker.Call(func() error {
		var incrErr error
		val, incrErr = rdb.Incr(ctx, key).Result()
		return incrErr
	})
	return val, err
}

// Expire wraps client.Expire with circuit breaker.
func Expire(ctx context.Context, key string, expiration time.Duration) error {
	return globalCircuitBreaker.Call(func() error {
		return rdb.Expire(ctx, key, expiration).Err()
	})
}

// Ping wraps client.Ping with circuit breaker.
func Ping(ctx context.Context) error {
	return globalCircuitBreaker.Call(func() error {
		return rdb.Ping(ctx).Err()
	})
}

// Pipeline executes a pipeline function wrapped in the circuit breaker.
func Pipeline(ctx context.Context, fn func(redis.Pipeliner) error) error {
	return globalCircuitBreaker.Call(func() error {
		pipe := rdb.Pipeline()
		if err := fn(pipe); err != nil {
			return err
		}
		_, err := pipe.Exec(ctx)
		return err
	})
}

// ---------------------------------------------------------------------------
// Higher-level cache helpers
// ---------------------------------------------------------------------------

// SetCacheData stores raw bytes in Redis with size validation and circuit breaker.
func SetCacheData(key string, data []byte, ttl time.Duration) error {
	if rdb == nil {
		return fmt.Errorf("redis not initialized")
	}
	if len(data) > RedisMaxCacheEntrySize {
		return fmt.Errorf("cache entry size %d bytes exceeds max %d bytes (key: %s)",
			len(data), RedisMaxCacheEntrySize, key)
	}

	return executeWithRetry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), RedisDefaultTimeout)
		defer cancel()
		return rdb.Set(ctx, key, data, ttl).Err()
	}, defaultRetryConfig)
}

// GetCacheData retrieves raw bytes from Redis with circuit breaker and
// single-flight deduplication for thundering-herd protection.
func GetCacheData(key string) ([]byte, error) {
	if rdb == nil {
		return nil, fmt.Errorf("redis not initialized")
	}
	res, err := cacheSingleFlight.Do(key, func() (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), RedisDefaultTimeout)
		defer cancel()

		data, getErr := rdb.Get(ctx, key).Bytes()
		if getErr != nil {
			return nil, getErr
		}
		return data, nil
	})

	if err != nil {
		return nil, err
	}
	return res.([]byte), nil
}

// InvalidateMultipleKeysPipeline deletes multiple cache keys in a single
// pipelined operation for efficiency.
func InvalidateMultipleKeysPipeline(keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	if _, err := requireClient(); err != nil {
		return err // Redis not initialized — graceful no-op for tests
	}

	return executeWithRetry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), RedisInvalidationTimeout)
		defer cancel()

		pipe := rdb.Pipeline()
		for _, key := range keys {
			pipe.Del(ctx, key)
		}
		_, err := pipe.Exec(ctx)
		return err
	}, defaultRetryConfig)
}

// ---------------------------------------------------------------------------
// Version counter helpers — O(1) atomic INCR for cache invalidation
// ---------------------------------------------------------------------------

// IncrementMediaVersion atomically bumps the version counter for a media entity.
// Returns the new version number.
func IncrementMediaVersion(mediaType string, mediaID int64, season, episode *int) (int64, error) {
	if _, err := requireClient(); err != nil {
		return 0, err
	}
	key := BuildMediaVersionKey(mediaType, mediaID, season, episode)

	var version int64
	err := executeWithRetry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), RedisDefaultTimeout)
		defer cancel()

		val, incrErr := rdb.Incr(ctx, key).Result()
		if incrErr != nil {
			return fmt.Errorf("failed to incr media version: %w", incrErr)
		}
		version = val

		// Set TTL so version keys eventually expire for old media.
		rdb.Expire(ctx, key, RedisMediaResponseTTL)
		return nil
	}, defaultRetryConfig)

	return version, err
}

// IncrementUserVersion atomically bumps the version counter for a user.
func IncrementUserVersion(userID string) (int64, error) {
	if _, err := requireClient(); err != nil {
		return 0, err
	}
	key := BuildUserVersionKey(userID)

	var version int64
	err := executeWithRetry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), RedisDefaultTimeout)
		defer cancel()

		val, incrErr := rdb.Incr(ctx, key).Result()
		if incrErr != nil {
			return fmt.Errorf("failed to incr user version: %w", incrErr)
		}
		version = val
		rdb.Expire(ctx, key, RedisMediaResponseTTL)
		return nil
	}, defaultRetryConfig)

	return version, err
}

// IncrementListVersion atomically bumps the version counter for a list.
func IncrementListVersion(listID int64) (int64, error) {
	if _, err := requireClient(); err != nil {
		return 0, err
	}
	key := BuildListVersionKey(listID)

	var version int64
	err := executeWithRetry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), RedisDefaultTimeout)
		defer cancel()

		val, incrErr := rdb.Incr(ctx, key).Result()
		if incrErr != nil {
			return fmt.Errorf("failed to incr list version: %w", incrErr)
		}
		version = val
		rdb.Expire(ctx, key, RedisMediaResponseTTL)
		return nil
	}, defaultRetryConfig)

	return version, err
}

// GetMediaVersion returns the current version counter for a media entity.
// Returns 0 if no version counter exists (never cached).
func GetMediaVersion(mediaType string, mediaID int64, season, episode *int) (int64, error) {
	if _, err := requireClient(); err != nil {
		return 0, err
	}
	key := BuildMediaVersionKey(mediaType, mediaID, season, episode)

	var version int64
	err := executeWithRetry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), RedisDefaultTimeout)
		defer cancel()

		val, getErr := rdb.Get(ctx, key).Int64()
		if getErr == redis.Nil {
			version = 0
			return nil
		}
		if getErr != nil {
			return fmt.Errorf("failed to get media version: %w", getErr)
		}
		version = val
		return nil
	}, defaultRetryConfig)

	return version, err
}

// GetUserVersion returns the current version counter for a user.
func GetUserVersion(userID string) (int64, error) {
	if _, err := requireClient(); err != nil {
		return 0, err
	}
	key := BuildUserVersionKey(userID)

	var version int64
	err := executeWithRetry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), RedisDefaultTimeout)
		defer cancel()

		val, getErr := rdb.Get(ctx, key).Int64()
		if getErr == redis.Nil {
			version = 0
			return nil
		}
		if getErr != nil {
			return fmt.Errorf("failed to get user version: %w", getErr)
		}
		version = val
		return nil
	}, defaultRetryConfig)

	return version, err
}

// GetListVersion returns the current version counter for a list.
func GetListVersion(listID int64) (int64, error) {
	if _, err := requireClient(); err != nil {
		return 0, err
	}
	key := BuildListVersionKey(listID)

	var version int64
	err := executeWithRetry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), RedisDefaultTimeout)
		defer cancel()

		val, getErr := rdb.Get(ctx, key).Int64()
		if getErr == redis.Nil {
			version = 0
			return nil
		}
		if getErr != nil {
			return fmt.Errorf("failed to get list version: %w", getErr)
		}
		version = val
		return nil
	}, defaultRetryConfig)

	return version, err
}
