package redis

import (
	"fmt"
	"time"
)

// Redis key prefixes — every key namespace gets its own prefix.
const (
	RedisAPIKeyPrefix        = "api_key:"
	RedisUserDetailsPrefix   = "user_details:"
	RedisJWKSPrefix          = "jwks:"
	RedisMediaResponsePrefix = "media_response:"
	RedisUserVersionPrefix   = "user_version:"
	RedisMediaVersionPrefix  = "media_version:"
	RedisListVersionPrefix   = "list_version:"
	RedisMediaNotFoundPrefix = "not_found:"
	RedisLeaderboardPrefix   = "leaderboard:"
	RedisSearchResultsPrefix = "search_results:"
	RedisRateLimitPrefix     = "rate_limit:"
)

// Cache TTLs — tuned for freshness-vs-hit-rate trade-offs.
const (
	RedisAPIKeyTTL        = 1 * time.Hour    // 1h — API key lookups are rare, keep warm
	RedisUserDetailsTTL   = 15 * time.Minute // 15m — profile changes are infrequent
	RedisJWKSTTL          = 5 * time.Minute  // 5m — short TTL for security (key rotation)
	RedisMediaResponseTTL = 30 * time.Minute // 30m — media responses can be stale for a bit
	RedisSearchResultsTTL = 30 * time.Minute // 30m — search results don't change rapidly
	RedisLeaderboardTTL   = 24 * time.Hour   // 24h — leaderboard recomputed daily
	RedisMediaNotFoundTTL = 5 * time.Minute  // 5m — negative cache for missing media
)

// Redis sizing and timeout constants.
const (
	RedisMaxCacheEntrySize    = 1 * 1024 * 1024  // 1 MB — reject cache entries larger than this
	RedisDefaultTimeout       = 5 * time.Second   // Default timeout for Redis operations
	RedisInvalidationTimeout  = 10 * time.Second  // Timeout for cache invalidation operations
	RedisDeleteBatchSize      = 100               // Batch size for key deletion operations
	RedisScanBatchSize        = 100               // Batch size for SCAN operations
)

// BuildMediaVersionKey creates the version counter key for a media entity.
// INCR this key to invalidate all cached responses for the media.
// Format: media_version:<type>:<id>:s<season>:e<episode>
func BuildMediaVersionKey(mediaType string, mediaID int64, season, episode *int) string {
	return fmt.Sprintf("%s%s:%d:%s:%s",
		RedisMediaVersionPrefix, mediaType, mediaID,
		fmtNullableInt(season, "s"), fmtNullableInt(episode, "e"))
}

// BuildCalculatedMediaResponseKey creates a unique key for a cached media response.
// The key includes all parameters that affect the output plus the version number
// for O(1) invalidation via version counter bumps.
// Format: media_response:<type>:<id>:s<season>:e<episode>:<extended>:v<version>
func BuildCalculatedMediaResponseKey(mediaType string, mediaID int64, season, episode *int, extended string, version int64) string {
	return fmt.Sprintf("%s%s:%d:%s:%s:%s:v%d",
		RedisMediaResponsePrefix, mediaType, mediaID,
		fmtNullableInt(season, "s"), fmtNullableInt(episode, "e"),
		extended, version)
}

// BuildUserVersionKey creates the version counter key for a user's cached data.
// Format: user_version:<userID>
func BuildUserVersionKey(userID string) string {
	return RedisUserVersionPrefix + userID
}

// BuildListVersionKey creates the version counter key for a list's cached data.
// Format: list_version:<listID>
func BuildListVersionKey(listID int64) string {
	return fmt.Sprintf("%s%d", RedisListVersionPrefix, listID)
}

// BuildMediaNotFoundKey creates the negative-cache sentinel key for a media.
// The version is included so invalidation via version increment naturally expires old entries.
// Format: not_found:<type>:<id>:s<season>:e<episode>:v<version>
func BuildMediaNotFoundKey(mediaType string, mediaID int64, season, episode *int, version int64) string {
	return fmt.Sprintf("%s%s:%d:%s:%s:v%d",
		RedisMediaNotFoundPrefix, mediaType, mediaID,
		fmtNullableInt(season, "s"), fmtNullableInt(episode, "e"),
		version)
}

// BuildCachedAPIKeyKey creates a key for the API key → user ID cache.
// Format: api_key:<key>
func BuildCachedAPIKeyKey(key string) string {
	return RedisAPIKeyPrefix + key
}

// fmtNullableInt formats an optional int pointer for key strings.
// Returns "<prefix>null" if nil, or "<prefix><value>" if non-nil.
func fmtNullableInt(v *int, prefix string) string {
	if v == nil {
		return prefix + "null"
	}
	return fmt.Sprintf("%s%d", prefix, *v)
}
