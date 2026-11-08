package stats

import (
	"log"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// ComputeUserStats aggregates user_history and user_ratings for a single user
// and upserts into user_stats.
// ---------------------------------------------------------------------------

// ComputeUserStats aggregates watch history and ratings for a user and upserts
// the result into the user_stats table.
func ComputeUserStats(db *gorm.DB, userID string) error {
	stats := database.UserStats{
		UserID: userID,
	}

	// Aggregate from user_history: plays, minutes, unique counts.
	type watchAgg struct {
		Plays   int64
		Minutes int64
	}
	var watch watchAgg
	// user_history has no play_count/runtime_minutes columns — this used to
	// select them and fail on every real database (which then aborted the
	// surrounding request transaction, so every later query in the stats
	// computation returned 25P02). A watch event is one history row, and the
	// runtime comes from the media row it points at: a movie's own runtime, or
	// the episode's when the row is an episode. A whole-show watch has no
	// single runtime and therefore contributes no minutes.
	err := db.Raw(`
		SELECT
			COUNT(*) AS plays,
			COALESCE(SUM(CASE h.media_type
				WHEN 'movie' THEN COALESCE(m.runtime, 0)
				WHEN 'episode' THEN COALESCE(e.runtime, 0)
				ELSE 0
			END), 0) AS minutes
		FROM user_history h
		LEFT JOIN movies m ON h.media_type = 'movie' AND m.id = h.media_id
		LEFT JOIN episodes e ON h.media_type = 'episode' AND e.id = h.episode_id
		WHERE h.user_id = ?
	`, userID).Scan(&watch).Error
	if err != nil {
		log.Printf("[stats] failed to aggregate user_history for %s: %v", userID, err)
	} else {
		stats.Plays = watch.Plays
		stats.Minutes = watch.Minutes
	}

	// Unique movies watched.
	if err := db.Raw(`
		SELECT COUNT(DISTINCT media_id)
		FROM user_history
		WHERE user_id = ? AND media_type = 'movie'
	`, userID).Scan(&stats.UniqueMovies).Error; err != nil {
		log.Printf("[stats] failed to count unique movies for %s: %v", userID, err)
	}

	// Unique shows watched.
	if err := db.Raw(`
		SELECT COUNT(DISTINCT media_id)
		FROM user_history
		WHERE user_id = ? AND media_type = 'show'
	`, userID).Scan(&stats.UniqueShows).Error; err != nil {
		log.Printf("[stats] failed to count unique shows for %s: %v", userID, err)
	}

	// Unique episodes watched.
	if err := db.Raw(`
		SELECT COUNT(DISTINCT episode_id)
		FROM user_history
		WHERE user_id = ? AND media_type = 'episode' AND episode_id IS NOT NULL
	`, userID).Scan(&stats.UniqueEpisodes).Error; err != nil {
		log.Printf("[stats] failed to count unique episodes for %s: %v", userID, err)
	}

	// Ratings count.
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM user_ratings
		WHERE user_id = ?
	`, userID).Scan(&stats.RatingsCount).Error; err != nil {
		log.Printf("[stats] failed to count ratings for %s: %v", userID, err)
	}

	now := time.Now()
	stats.ComputedAt = &now

	// Upsert into user_stats.
	return db.Where("user_id = ?", userID).Assign(stats).FirstOrCreate(&stats).Error
}

// ComputeAllStats recomputes stats for all users. This is a batch operation
// intended for worker/cron use, not for request-time execution.
func ComputeAllStats(db *gorm.DB) error {
	var userIDs []string
	if err := db.Table("users").Pluck("id", &userIDs).Error; err != nil {
		return err
	}

	for _, userID := range userIDs {
		if err := ComputeUserStats(db, userID); err != nil {
			log.Printf("[stats] failed to compute stats for user %s: %v", userID, err)
			// Continue with remaining users.
		}
	}

	return nil
}

// GetUserStats retrieves cached stats for a user. Returns nil, nil if no stats exist.
func GetUserStats(db *gorm.DB, userID string) (*database.UserStats, error) {
	var stats database.UserStats
	err := db.Where("user_id = ?", userID).First(&stats).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &stats, nil
}
