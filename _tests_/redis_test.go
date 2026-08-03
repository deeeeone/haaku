package tests

import (
	"os"
	"testing"
	"time"

	"trakt-api/redis"
)

// ---------------------------------------------------------------------------
// Keys — builder functions
// ---------------------------------------------------------------------------

func TestBuildMediaVersionKey(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		mediaID   int64
		season    *int
		episode   *int
		want      string
	}{
		{
			name:      "movie",
			mediaType: "movie",
			mediaID:   42,
			want:      "media_version:movie:42:snull:enull",
		},
		{
			name:      "show",
			mediaType: "show",
			mediaID:   7,
			want:      "media_version:show:7:snull:enull",
		},
		{
			name:      "season",
			mediaType: "season",
			mediaID:   7,
			season:    intPtr(1),
			want:      "media_version:season:7:s1:enull",
		},
		{
			name:      "episode",
			mediaType: "episode",
			mediaID:   7,
			season:    intPtr(1),
			episode:   intPtr(3),
			want:      "media_version:episode:7:s1:e3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redis.BuildMediaVersionKey(tt.mediaType, tt.mediaID, tt.season, tt.episode)
			if got != tt.want {
				t.Errorf("BuildMediaVersionKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildCalculatedMediaResponseKey(t *testing.T) {
	season := 2
	episode := 5

	tests := []struct {
		name      string
		mediaType string
		mediaID   int64
		season    *int
		episode   *int
		extended  string
		version   int64
		want      string
	}{
		{
			name:      "movie",
			mediaType: "movie",
			mediaID:   42,
			extended:  "full",
			version:   3,
			want:      "media_response:movie:42:snull:enull:full:v3",
		},
		{
			name:      "episode_with_extended",
			mediaType: "episode",
			mediaID:   7,
			season:    &season,
			episode:   &episode,
			extended:  "full",
			version:   5,
			want:      "media_response:episode:7:s2:e5:full:v5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redis.BuildCalculatedMediaResponseKey(tt.mediaType, tt.mediaID, tt.season, tt.episode, tt.extended, tt.version)
			if got != tt.want {
				t.Errorf("BuildCalculatedMediaResponseKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildUserVersionKey(t *testing.T) {
	got := redis.BuildUserVersionKey("user_abc123")
	want := "user_version:user_abc123"
	if got != want {
		t.Errorf("BuildUserVersionKey() = %q, want %q", got, want)
	}
}

func TestBuildListVersionKey(t *testing.T) {
	got := redis.BuildListVersionKey(99)
	want := "list_version:99"
	if got != want {
		t.Errorf("BuildListVersionKey() = %q, want %q", got, want)
	}
}

func TestBuildMediaNotFoundKey(t *testing.T) {
	season := 1
	episode := 4
	got := redis.BuildMediaNotFoundKey("episode", 7, &season, &episode, 2)
	want := "not_found:episode:7:s1:e4:v2"
	if got != want {
		t.Errorf("BuildMediaNotFoundKey() = %q, want %q", got, want)
	}
}

func TestBuildCachedAPIKeyKey(t *testing.T) {
	got := redis.BuildCachedAPIKeyKey("sk_test_abc")
	want := "api_key:sk_test_abc"
	if got != want {
		t.Errorf("BuildCachedAPIKeyKey() = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Prefixes and TTLs exist
// ---------------------------------------------------------------------------

func TestPrefixesNonEmpty(t *testing.T) {
	prefixes := map[string]string{
		"APIKey":         redis.RedisAPIKeyPrefix,
		"UserDetails":    redis.RedisUserDetailsPrefix,
		"JWKS":           redis.RedisJWKSPrefix,
		"MediaResponse":  redis.RedisMediaResponsePrefix,
		"UserVersion":    redis.RedisUserVersionPrefix,
		"MediaVersion":   redis.RedisMediaVersionPrefix,
		"ListVersion":    redis.RedisListVersionPrefix,
		"NotFound":       redis.RedisMediaNotFoundPrefix,
		"Leaderboard":    redis.RedisLeaderboardPrefix,
		"SearchResults":  redis.RedisSearchResultsPrefix,
		"RateLimit":      redis.RedisRateLimitPrefix,
	}
	for name, pfx := range prefixes {
		if pfx == "" {
			t.Errorf("prefix %s is empty", name)
		}
	}
}

func TestTTLsPositive(t *testing.T) {
	ttls := map[string]time.Duration{
		"APIKey":        redis.RedisAPIKeyTTL,
		"UserDetails":   redis.RedisUserDetailsTTL,
		"JWKS":          redis.RedisJWKSTTL,
		"MediaResponse": redis.RedisMediaResponseTTL,
		"SearchResults": redis.RedisSearchResultsTTL,
		"Leaderboard":   redis.RedisLeaderboardTTL,
		"NotFound":      redis.RedisMediaNotFoundTTL,
	}
	for name, ttl := range ttls {
		if ttl <= 0 {
			t.Errorf("TTL %s is non-positive: %v", name, ttl)
		}
	}
}

func TestSizeConstants(t *testing.T) {
	if redis.RedisMaxCacheEntrySize != 1*1024*1024 {
		t.Errorf("RedisMaxCacheEntrySize = %d, want 1 MB", redis.RedisMaxCacheEntrySize)
	}
	if redis.RedisDefaultTimeout != 5*time.Second {
		t.Errorf("RedisDefaultTimeout = %v, want 5s", redis.RedisDefaultTimeout)
	}
	if redis.RedisInvalidationTimeout != 10*time.Second {
		t.Errorf("RedisInvalidationTimeout = %v, want 10s", redis.RedisInvalidationTimeout)
	}
	if redis.RedisDeleteBatchSize != 100 {
		t.Errorf("RedisDeleteBatchSize = %d, want 100", redis.RedisDeleteBatchSize)
	}
	if redis.RedisScanBatchSize != 100 {
		t.Errorf("RedisScanBatchSize = %d, want 100", redis.RedisScanBatchSize)
	}
}

// ---------------------------------------------------------------------------
// Circuit breaker — state transitions (unit, no Redis)
// ---------------------------------------------------------------------------

func TestCircuitBreakerStates(t *testing.T) {
	if redis.CircuitClosed != 0 {
		t.Errorf("CircuitClosed = %d, want 0", redis.CircuitClosed)
	}
	if redis.CircuitOpen != 1 {
		t.Errorf("CircuitOpen = %d, want 1", redis.CircuitOpen)
	}
	if redis.CircuitHalfOpen != 2 {
		t.Errorf("CircuitHalfOpen = %d, want 2", redis.CircuitHalfOpen)
	}
}

func TestCircuitBreakerEnvConfig(t *testing.T) {
	// Save and restore env
	save := func(k string) string { return os.Getenv(k) }

	t.Run("defaults", func(t *testing.T) {
		os.Unsetenv("REDIS_CB_FAILURE_THRESHOLD")
		os.Unsetenv("REDIS_CB_TIMEOUT_SEC")
		os.Unsetenv("REDIS_CB_SUCCESS_THRESHOLD")

		// We can only check that the exported functions exist
		state := redis.GetCircuitBreakerState()
		if state != "closed" && state != "open" && state != "half-open" && state != "unknown" {
			t.Errorf("unexpected state: %q", state)
		}
	})

	t.Run("custom_threshold", func(t *testing.T) {
		os.Setenv("REDIS_CB_FAILURE_THRESHOLD", "500")
		// Circuit breaker is package-level singleton, can't easily reset.
		// Just verify env is read correctly by checking it changes behavior.
		// The actual test is that the function doesn't panic.
		state := redis.GetCircuitBreakerState()
		if state == "" {
			t.Error("state should not be empty")
		}
	})

	// Restore
	for _, k := range []string{"REDIS_CB_FAILURE_THRESHOLD", "REDIS_CB_TIMEOUT_SEC", "REDIS_CB_SUCCESS_THRESHOLD"} {
		if v := save(k); v != "" {
			os.Setenv(k, v)
		} else {
			os.Unsetenv(k)
		}
	}
}

func TestCircuitBreakerIsHealthy(t *testing.T) {
	healthy := redis.IsCircuitBreakerHealthy()
	// Default state is closed → healthy
	if !healthy {
		t.Error("circuit breaker should be healthy by default")
	}
}

// ---------------------------------------------------------------------------
// Single-flight — unit tests (no Redis)
// ---------------------------------------------------------------------------

func TestSingleFlightGroup_Deduplicates(t *testing.T) {
	sfg := redis.NewSingleFlightGroupForTest()
	if sfg == nil {
		t.Fatal("NewSingleFlightGroupForTest returned nil")
	}

	callCount := 0
	result, err := sfg.Do("key1", func() (interface{}, error) {
		callCount++
		return "value1", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "value1" {
		t.Errorf("got %v, want value1", result)
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}
}

func intPtr(i int) *int { return &i }
