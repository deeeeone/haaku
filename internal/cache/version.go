package cache

import "github.com/haaku-tv/haaku/redis"

// IncrementMediaVersion atomically bumps the version counter for a media entity.
// This invalidates all cached responses for that media in O(1) — callers
// will fetch the new version and compute fresh responses.
func IncrementMediaVersion(mediaType string, mediaID int64, season, episode *int) (int64, error) {
	return redis.IncrementMediaVersion(mediaType, mediaID, season, episode)
}

// IncrementUserVersion atomically bumps the version counter for a user.
func IncrementUserVersion(userID string) (int64, error) {
	return redis.IncrementUserVersion(userID)
}

// IncrementListVersion atomically bumps the version counter for a list.
func IncrementListVersion(listID int64) (int64, error) {
	return redis.IncrementListVersion(listID)
}

// GetMediaVersion returns the current version counter for a media entity.
func GetMediaVersion(mediaType string, mediaID int64, season, episode *int) (int64, error) {
	return redis.GetMediaVersion(mediaType, mediaID, season, episode)
}

// GetUserVersion returns the current version counter for a user.
func GetUserVersion(userID string) (int64, error) {
	return redis.GetUserVersion(userID)
}

// GetListVersion returns the current version counter for a list.
func GetListVersion(listID int64) (int64, error) {
	return redis.GetListVersion(listID)
}
