package recommendations

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	projredis "github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/gorm"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		// Personalized
		{
			Method: "GET", Path: "/v1/recommendations/movies",
			Summary: "Get personalized movie recommendations based on watch history",
			Tags:    []string{"Recommendations"}, Auth: "required",
			StatusCodes: map[int]string{200: "Movie recommendations", 401: "Unauthorized"},
		},
		{
			Method: "GET", Path: "/v1/recommendations/shows",
			Summary: "Get personalized show recommendations based on watch history",
			Tags:    []string{"Recommendations"}, Auth: "required",
			StatusCodes: map[int]string{200: "Show recommendations", 401: "Unauthorized"},
		},
		// Social
		{
			Method: "GET", Path: "/v1/social_recommendations/movies",
			Summary: "Get movie recommendations from friends' watch history",
			Tags:    []string{"Recommendations"}, Auth: "required",
			StatusCodes: map[int]string{200: "Social movie recommendations", 401: "Unauthorized"},
		},
		{
			Method: "GET", Path: "/v1/social_recommendations/shows",
			Summary: "Get show recommendations from friends' watch history",
			Tags:    []string{"Recommendations"}, Auth: "required",
			StatusCodes: map[int]string{200: "Social show recommendations", 401: "Unauthorized"},
		},
		// Anticipated
		{
			Method: "GET", Path: "/v1/media/anticipated",
			Summary: "Get anticipated upcoming media weighted by watchlist interest",
			Tags:    []string{"Media"}, Auth: "required",
			StatusCodes: map[int]string{200: "Anticipated media", 401: "Unauthorized"},
		},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires all recommendation route handlers.
func RegisterRoutes(api fiber.Router) {
	recs := api.Group("/recommendations")
	recs.Get("/movies", auth.AuthRequired(), handlePersonalizedMovies)
	recs.Get("/shows", auth.AuthRequired(), handlePersonalizedShows)

	social := api.Group("/social_recommendations")
	social.Get("/movies", auth.AuthRequired(), handleSocialMovies)
	social.Get("/shows", auth.AuthRequired(), handleSocialShows)

	api.Get("/media/anticipated", auth.AuthRequired(), handleAnticipated)
}

// ============================================================================
// SHARED TYPES
// ============================================================================

type recMovieItem struct {
	ID         int64    `json:"id"`
	TmdbID     int      `json:"tmdb_id"`
	Title      string   `json:"title"`
	Overview   string   `json:"overview,omitempty"`
	Popularity float64  `json:"popularity"`
	Genres     []string `json:"genres"`
	Released   string   `json:"released,omitempty"`
	Runtime    int      `json:"runtime,omitempty"`
	RatingAvg  float64  `json:"rating_avg"`
	Poster     string   `json:"poster,omitempty"`
}

type recShowItem struct {
	ID           int64    `json:"id"`
	TmdbID       int      `json:"tmdb_id"`
	Title        string   `json:"title"`
	Overview     string   `json:"overview,omitempty"`
	Popularity   float64  `json:"popularity"`
	Genres       []string `json:"genres"`
	FirstAired   string   `json:"first_aired,omitempty"`
	Status       string   `json:"status,omitempty"`
	EpisodeCount int      `json:"episode_count"`
	RatingAvg    float64  `json:"rating_avg"`
	Poster       string   `json:"poster,omitempty"`
}

type socialMovieItem struct {
	ID          int64    `json:"id"`
	TmdbID      int      `json:"tmdb_id"`
	Title       string   `json:"title"`
	Overview    string   `json:"overview,omitempty"`
	Popularity  float64  `json:"popularity"`
	Genres      []string `json:"genres"`
	Released    string   `json:"released,omitempty"`
	FriendCount int      `json:"friend_count"`
	RatingAvg   float64  `json:"rating_avg"`
	Poster      string   `json:"poster,omitempty"`
}

type socialShowItem struct {
	ID          int64    `json:"id"`
	TmdbID      int      `json:"tmdb_id"`
	Title       string   `json:"title"`
	Overview    string   `json:"overview,omitempty"`
	Popularity  float64  `json:"popularity"`
	Genres      []string `json:"genres"`
	FirstAired  string   `json:"first_aired,omitempty"`
	Status      string   `json:"status,omitempty"`
	FriendCount int      `json:"friend_count"`
	RatingAvg   float64  `json:"rating_avg"`
	Poster      string   `json:"poster,omitempty"`
}

type anticipatedItem struct {
	MediaType      string   `json:"media_type"`
	ID             int64    `json:"id"`
	TmdbID         int      `json:"tmdb_id"`
	Title          string   `json:"title"`
	Overview       string   `json:"overview,omitempty"`
	Popularity     float64  `json:"popularity"`
	Genres         []string `json:"genres"`
	WatchlistCount int      `json:"watchlist_count"`
	Score          float64  `json:"score"`
	ReleaseDate    string   `json:"release_date,omitempty"`
	NextAirDate    string   `json:"next_air_date,omitempty"`
	Status         string   `json:"status,omitempty"`
	Poster         string   `json:"poster,omitempty"`
}

// ============================================================================
// PERSONALIZED RECOMMENDATIONS
// ============================================================================

func handlePersonalizedMovies(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	genreFilter := c.Query("genre", "")
	page, limit := response.ParsePagination(c, 20)

	// Step 1: Get user's top 5 genres from history + favorites + ratings.
	topGenres, err := fetchTopGenresCached(db, userID, 5)
	if err != nil {
		return response.InternalError(c)
	}

	if len(topGenres) == 0 {
		return c.JSON(fiber.Map{
			"items":     []recMovieItem{},
			"page":      page,
			"limit":     limit,
			"row_count": 0,
		})
	}

	// Step 2: Build query for unwatched movies in those genres.
	var total int64
	var movies []database.Movie

	baseQuery := db.
		Where("movies.genres ?| ?", topGenres).
		Where("NOT EXISTS (SELECT 1 FROM user_history WHERE user_id = ? AND media_type = ? AND media_id = movies.id)", userID, "movie").
		Where("NOT EXISTS (SELECT 1 FROM user_hidden WHERE user_id = ? AND media_type = ? AND media_id = movies.id)", userID, "movie")

	if genreFilter != "" {
		baseQuery = baseQuery.Where("movies.genres ? ?", genreFilter)
	}

	// Count total
	if err := baseQuery.Distinct("movies.id").Count(&total).Error; err != nil {
		return response.InternalError(c)
	}

	// Fetch page
	if err := baseQuery.
		Select("DISTINCT movies.*").
		Order("movies.popularity DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&movies).Error; err != nil {
		return response.InternalError(c)
	}

	results := make([]recMovieItem, 0, len(movies))
	for _, m := range movies {
		results = append(results, recMovieItem{
			ID:         m.ID,
			TmdbID:     m.TmdbID,
			Title:      m.Title,
			Overview:   m.Overview,
			Popularity: m.Popularity,
			Genres:     m.Genres.Values(),
			Released:   m.Released.String(),
			Runtime:    m.Runtime,
			RatingAvg:  m.RatingAvg,
			Poster:     m.Images.StringValue("poster"),
		})
	}
	if results == nil {
		results = make([]recMovieItem, 0)
	}

	return c.JSON(fiber.Map{
		"items":     results,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

func handlePersonalizedShows(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	genreFilter := c.Query("genre", "")
	page, limit := response.ParsePagination(c, 20)

	// Step 1: Get user's top 5 genres.
	topGenres, err := fetchTopGenresCached(db, userID, 5)
	if err != nil {
		return response.InternalError(c)
	}

	if len(topGenres) == 0 {
		return c.JSON(fiber.Map{
			"items":     []recShowItem{},
			"page":      page,
			"limit":     limit,
			"row_count": 0,
		})
	}

	// Step 2: Build query for unwatched shows in those genres.
	// "Unwatched" = no episode history for this show.
	var total int64
	var shows []database.Show

	baseQuery := db.
		Where("shows.genres ?| ?", topGenres).
		Where(`NOT EXISTS (
			SELECT 1 FROM user_history uh
			JOIN episodes e ON uh.media_id = e.id
			WHERE uh.user_id = ? AND uh.media_type = 'episode' AND e.show_id = shows.id
		)`, userID).
		Where("NOT EXISTS (SELECT 1 FROM user_hidden WHERE user_id = ? AND media_type = ? AND media_id = shows.id)", userID, "show")

	if genreFilter != "" {
		baseQuery = baseQuery.Where("shows.genres ? ?", genreFilter)
	}

	if err := baseQuery.Distinct("shows.id").Count(&total).Error; err != nil {
		return response.InternalError(c)
	}

	if err := baseQuery.
		Select("DISTINCT shows.*").
		Order("shows.popularity DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&shows).Error; err != nil {
		return response.InternalError(c)
	}

	results := make([]recShowItem, 0, len(shows))
	for _, s := range shows {
		results = append(results, recShowItem{
			ID:           s.ID,
			TmdbID:       s.TmdbID,
			Title:        s.Title,
			Overview:     s.Overview,
			Popularity:   s.Popularity,
			Genres:       s.Genres.Values(),
			FirstAired:   s.FirstAired.String(),
			Status:       s.Status,
			EpisodeCount: s.EpisodeCount,
			RatingAvg:    s.RatingAvg,
			Poster:       s.Images.StringValue("poster"),
		})
	}
	if results == nil {
		results = make([]recShowItem, 0)
	}

	return c.JSON(fiber.Map{
		"items":     results,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

// ============================================================================
// SOCIAL RECOMMENDATIONS
// ============================================================================

func handleSocialMovies(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 20)
	cutoff := time.Now().AddDate(0, 0, -90)

	// Use raw SQL for efficiency with subqueries.
	type row struct {
		ID          int64   `json:"id"`
		TmdbID      int     `json:"tmdb_id"`
		Title       string  `json:"title"`
		Overview    string  `json:"overview"`
		Popularity  float64 `json:"popularity"`
		Genres      database.StringSlice
		Released    *database.Date `json:"released"`
		Runtime     int            `json:"runtime"`
		RatingAvg   float64        `json:"rating_avg"`
		Images      database.JSONMap
		FriendCount int `json:"friend_count"`
		FullCount   int `json:"full_count"`
	}

	var rows []row

	sql := `
		WITH mutual_friends AS (
			SELECT f1.following_id AS friend_id
			FROM user_follows f1
			INNER JOIN user_follows f2
				ON f1.following_id = f2.follower_id
				AND f1.follower_id = f2.following_id
			WHERE f1.follower_id = ?
				AND f1.approved_at IS NOT NULL
				AND f2.approved_at IS NOT NULL
		),
		friend_watches AS (
			SELECT uh.media_id, COUNT(DISTINCT uh.user_id) AS friend_count
			FROM user_history uh
			INNER JOIN mutual_friends mf ON uh.user_id = mf.friend_id
			WHERE uh.media_type = 'movie'
				AND uh.watched_at >= ?
			GROUP BY uh.media_id
		)
		SELECT m.*, fw.friend_count, COUNT(*) OVER() AS full_count
		FROM movies m
		INNER JOIN friend_watches fw ON m.id = fw.media_id
		WHERE NOT EXISTS (
			SELECT 1 FROM user_history uh2
			WHERE uh2.user_id = ? AND uh2.media_type = 'movie' AND uh2.media_id = m.id
		)
		ORDER BY fw.friend_count DESC, m.popularity DESC
		LIMIT ? OFFSET ?
	`

	if err := db.Raw(sql, userID, cutoff, userID, limit, (page-1)*limit).Scan(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	total := int64(0)
	results := make([]socialMovieItem, 0, len(rows))
	for _, r := range rows {
		results = append(results, socialMovieItem{
			ID:          r.ID,
			TmdbID:      r.TmdbID,
			Title:       r.Title,
			Overview:    r.Overview,
			Popularity:  r.Popularity,
			Genres:      r.Genres.Values(),
			Released:    r.Released.String(),
			FriendCount: r.FriendCount,
			RatingAvg:   r.RatingAvg,
			Poster:      r.Images.StringValue("poster"),
		})
		if total == 0 && r.FullCount > 0 {
			total = int64(r.FullCount)
		}
	}
	if results == nil {
		results = make([]socialMovieItem, 0)
	}

	return c.JSON(fiber.Map{
		"items":     results,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

func handleSocialShows(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 20)
	cutoff := time.Now().AddDate(0, 0, -90)

	type row struct {
		ID           int64   `json:"id"`
		TmdbID       int     `json:"tmdb_id"`
		Title        string  `json:"title"`
		Overview     string  `json:"overview"`
		Popularity   float64 `json:"popularity"`
		Genres       database.StringSlice
		FirstAired   *database.Date `json:"first_aired"`
		Status       string         `json:"status"`
		EpisodeCount int            `json:"episode_count"`
		RatingAvg    float64        `json:"rating_avg"`
		Images       database.JSONMap
		FriendCount  int `json:"friend_count"`
		FullCount    int `json:"full_count"`
	}

	var rows []row

	// Map episode watches to shows, count distinct friends who watched any
	// episode of each show, then filter out shows the user has already watched.
	sql := `
		WITH mutual_friends AS (
			SELECT f1.following_id AS friend_id
			FROM user_follows f1
			INNER JOIN user_follows f2
				ON f1.following_id = f2.follower_id
				AND f1.follower_id = f2.following_id
			WHERE f1.follower_id = ?
				AND f1.approved_at IS NOT NULL
				AND f2.approved_at IS NOT NULL
		),
		friend_episode_watches AS (
			SELECT e.show_id AS media_id, COUNT(DISTINCT uh.user_id) AS friend_count
			FROM user_history uh
			INNER JOIN mutual_friends mf ON uh.user_id = mf.friend_id
			INNER JOIN episodes e ON uh.media_id = e.id
			WHERE uh.media_type = 'episode'
				AND uh.watched_at >= ?
			GROUP BY e.show_id
		)
		SELECT s.*, fw.friend_count, COUNT(*) OVER() AS full_count
		FROM shows s
		INNER JOIN friend_episode_watches fw ON s.id = fw.media_id
		WHERE NOT EXISTS (
			SELECT 1 FROM user_history uh2
			INNER JOIN episodes e2 ON uh2.media_id = e2.id
			WHERE uh2.user_id = ? AND uh2.media_type = 'episode' AND e2.show_id = s.id
		)
		ORDER BY fw.friend_count DESC, s.popularity DESC
		LIMIT ? OFFSET ?
	`

	if err := db.Raw(sql, userID, cutoff, userID, limit, (page-1)*limit).Scan(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	total := int64(0)
	results := make([]socialShowItem, 0, len(rows))
	for _, r := range rows {
		results = append(results, socialShowItem{
			ID:          r.ID,
			TmdbID:      r.TmdbID,
			Title:       r.Title,
			Overview:    r.Overview,
			Popularity:  r.Popularity,
			Genres:      r.Genres.Values(),
			FirstAired:  r.FirstAired.String(),
			Status:      r.Status,
			FriendCount: r.FriendCount,
			RatingAvg:   r.RatingAvg,
			Poster:      r.Images.StringValue("poster"),
		})
		if total == 0 && r.FullCount > 0 {
			total = int64(r.FullCount)
		}
	}
	if results == nil {
		results = make([]socialShowItem, 0)
	}

	return c.JSON(fiber.Map{
		"items":     results,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

// ============================================================================
// ANTICIPATED MEDIA
// ============================================================================

func handleAnticipated(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	mediaType := c.Query("type", "")
	page, limit := response.ParsePagination(c, 20)

	type row struct {
		MediaType      string  `json:"media_type"`
		ID             int64   `json:"id"`
		TmdbID         int     `json:"tmdb_id"`
		Title          string  `json:"title"`
		Overview       string  `json:"overview"`
		Popularity     float64 `json:"popularity"`
		Genres         database.StringSlice
		WatchlistCount int              `json:"watchlist_count"`
		Score          float64          `json:"score"`
		ReleaseDate    *database.Date   `json:"release_date"`
		NextAirDate    *database.Date   `json:"next_air_date"`
		Status         string           `json:"status"`
		Images         database.JSONMap `json:"images"`
		FullCount      int              `json:"full_count"`
	}

	var rows []row

	// Shows: upcoming episodes joined with shows, weighted by watchlist count.
	// Movies: released in last 30 days + next 90 days, weighted similarly.
	// Combined with weighted score: watchlist_count * 0.7 + popularity * 0.3.
	// NOTE: full_count is intentionally NOT computed per CTE here — when both
	// types are returned the two parts are UNIONed, so the row count must be
	// taken over the combined set (see the "both" wrapper below), otherwise
	// the reported total is the per-type count, not the combined total.
	sql := `
		SELECT
			'show' AS media_type,
			s.id,
			s.tmdb_id,
			s.title,
			s.overview,
			s.popularity,
			COALESCE(wl.cnt, 0) AS watchlist_count,
			(COALESCE(wl.cnt, 0) * 0.7 + s.popularity * 0.3) AS score,
			NULL::date AS release_date,
			MIN(e.aired_at) AS next_air_date,
			s.status,
			s.images
		FROM shows s
		INNER JOIN episodes e ON e.show_id = s.id
		LEFT JOIN (
			SELECT media_id, COUNT(*) AS cnt
			FROM user_watchlist
			WHERE media_type = 'show'
			GROUP BY media_id
		) wl ON wl.media_id = s.id
		WHERE e.aired_at >= CURRENT_DATE
		GROUP BY s.id, s.tmdb_id, s.title, s.overview, s.popularity, wl.cnt, s.status, s.images

		UNION ALL

		SELECT
			'movie' AS media_type,
			m.id,
			m.tmdb_id,
			m.title,
			m.overview,
			m.popularity,
			COALESCE(wl.cnt, 0) AS watchlist_count,
			(COALESCE(wl.cnt, 0) * 0.7 + m.popularity * 0.3) AS score,
			m.released AS release_date,
			NULL::date AS next_air_date,
			NULL AS status,
			m.images
		FROM movies m
		LEFT JOIN (
			SELECT media_id, COUNT(*) AS cnt
			FROM user_watchlist
			WHERE media_type = 'movie'
			GROUP BY media_id
		) wl ON wl.media_id = m.id
		WHERE m.released BETWEEN CURRENT_DATE - INTERVAL '30 days' AND CURRENT_DATE + INTERVAL '90 days'
	`

	// Optionally filter by type.
	var args []interface{}
	switch mediaType {
	case "movies":
		sql = `
			SELECT
				'movie' AS media_type,
				m.id,
				m.tmdb_id,
				m.title,
				m.overview,
				m.popularity,
				COALESCE(wl.cnt, 0) AS watchlist_count,
				(COALESCE(wl.cnt, 0) * 0.7 + m.popularity * 0.3) AS score,
				m.released AS release_date,
				NULL::date AS next_air_date,
				NULL AS status,
				m.images,
				COUNT(*) OVER() AS full_count
			FROM movies m
			LEFT JOIN (
				SELECT media_id, COUNT(*) AS cnt
				FROM user_watchlist
				WHERE media_type = 'movie'
				GROUP BY media_id
			) wl ON wl.media_id = m.id
			WHERE m.released BETWEEN CURRENT_DATE - INTERVAL '30 days' AND CURRENT_DATE + INTERVAL '90 days'
			ORDER BY score DESC
			LIMIT ? OFFSET ?
		`
		args = append(args, limit, (page-1)*limit)
	case "shows":
		sql = `
			SELECT
				'show' AS media_type,
				s.id,
				s.tmdb_id,
				s.title,
				s.overview,
				s.popularity,
				COALESCE(wl.cnt, 0) AS watchlist_count,
				(COALESCE(wl.cnt, 0) * 0.7 + s.popularity * 0.3) AS score,
				NULL::date AS release_date,
				MIN(e.aired_at) AS next_air_date,
				s.status,
				s.images,
				COUNT(*) OVER() AS full_count
			FROM shows s
			INNER JOIN episodes e ON e.show_id = s.id
			LEFT JOIN (
				SELECT media_id, COUNT(*) AS cnt
				FROM user_watchlist
				WHERE media_type = 'show'
				GROUP BY media_id
			) wl ON wl.media_id = s.id
			WHERE e.aired_at >= CURRENT_DATE
			GROUP BY s.id, s.tmdb_id, s.title, s.overview, s.popularity, wl.cnt, s.status, s.images
			ORDER BY score DESC
			LIMIT ? OFFSET ?
		`
		args = append(args, limit, (page-1)*limit)
	default:
		// Both: wrap in a subquery. full_count is computed here over the
		// UNIONed result (not per-type) so the reported total matches the
		// combined item set that ORDER/LIMIT paginate over. ORDER BY score
		// applies to the whole combined set, then page slices.
		sql = `
			SELECT * FROM (
				SELECT combined.*, COUNT(*) OVER() AS full_count
				FROM (
					` + sql + `
				) combined
			) paged
			ORDER BY score DESC
			LIMIT ? OFFSET ?
		`
		args = append(args, limit, (page-1)*limit)
	}

	if err := db.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	total := int64(0)
	results := make([]anticipatedItem, 0, len(rows))
	for _, r := range rows {
		results = append(results, anticipatedItem{
			MediaType:      r.MediaType,
			ID:             r.ID,
			TmdbID:         r.TmdbID,
			Title:          r.Title,
			Overview:       r.Overview,
			Popularity:     r.Popularity,
			Genres:         r.Genres.Values(),
			WatchlistCount: r.WatchlistCount,
			Score:          r.Score,
			ReleaseDate:    r.ReleaseDate.String(),
			NextAirDate:    r.NextAirDate.String(),
			Status:         r.Status,
			Poster:         r.Images.StringValue("poster"),
		})
		if total == 0 && r.FullCount > 0 {
			total = int64(r.FullCount)
		}
	}
	if results == nil {
		results = make([]anticipatedItem, 0)
	}

	return c.JSON(fiber.Map{
		"items":     results,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

// ============================================================================
// HELPERS
// ============================================================================

// fetchTopGenres returns the user's top N genres from history, favorites, and ratings.
func fetchTopGenres(db *gorm.DB, userID string, n int) ([]string, error) {
	sql := `
		SELECT genre, COUNT(*) AS cnt FROM (
			-- Movie watch history
			SELECT jsonb_array_elements_text(m.genres) AS genre
			FROM user_history uh
			JOIN movies m ON uh.media_id = m.id
			WHERE uh.user_id = ? AND uh.media_type = 'movie' AND m.genres IS NOT NULL
			UNION ALL
			-- Show watch history (via episodes)
			SELECT jsonb_array_elements_text(s.genres) AS genre
			FROM user_history uh
			JOIN episodes e ON uh.media_id = e.id
			JOIN shows s ON e.show_id = s.id
			WHERE uh.user_id = ? AND uh.media_type = 'episode' AND s.genres IS NOT NULL
			UNION ALL
			-- Movie favorites
			SELECT jsonb_array_elements_text(m.genres) AS genre
			FROM user_favorites uf
			JOIN movies m ON uf.media_id = m.id
			WHERE uf.user_id = ? AND uf.media_type = 'movie' AND m.genres IS NOT NULL
			UNION ALL
			-- Show favorites
			SELECT jsonb_array_elements_text(s.genres) AS genre
			FROM user_favorites uf
			JOIN shows s ON uf.media_id = s.id
			WHERE uf.user_id = ? AND uf.media_type = 'show' AND s.genres IS NOT NULL
			UNION ALL
			-- Movie ratings
			SELECT jsonb_array_elements_text(m.genres) AS genre
			FROM user_ratings ur
			JOIN movies m ON ur.media_id = m.id
			WHERE ur.user_id = ? AND ur.media_type = 'movie' AND m.genres IS NOT NULL
			UNION ALL
			-- Show ratings
			SELECT jsonb_array_elements_text(s.genres) AS genre
			FROM user_ratings ur
			JOIN shows s ON ur.media_id = s.id
			WHERE ur.user_id = ? AND ur.media_type = 'show' AND s.genres IS NOT NULL
		) all_genres
		WHERE genre IS NOT NULL AND genre != ''
		GROUP BY genre
		ORDER BY cnt DESC
		LIMIT ?
	`

	type genreRow struct {
		Genre string
		Cnt   int
	}

	var rows []genreRow
	if err := db.Raw(sql, userID, userID, userID, userID, userID, userID, n).Scan(&rows).Error; err != nil {
		return nil, err
	}

	genres := make([]string, 0, len(rows))
	for _, r := range rows {
		genres = append(genres, r.Genre)
	}
	if genres == nil {
		genres = make([]string, 0)
	}
	return genres, nil
}

// ---------------------------------------------------------------------------
// Cached fetchTopGenres
// ---------------------------------------------------------------------------

// topGenresCacheTTL bounds how long a per-user top-genres result is served from
// Redis. Version-keying the entry means a history/favorites/ratings write that
// bumps the user version counter invalidates the result immediately (O(1)); the
// TTL is a backstop for the version counter expiring.
const topGenresCacheTTL = 30 * time.Minute

func topGenresCacheKey(userID string, version int64) string {
	return fmt.Sprintf("recommendations:topgenres:%s:v%d", userID, version)
}

// fetchTopGenresCached returns the user's top N genres, serving from Redis when
// a fresh version-keyed entry exists and computing from the DB otherwise. It is
// best-effort: on Redis unavailability or version-fetch failure it falls through
// to the DB scan and never fails the request.
func fetchTopGenresCached(db *gorm.DB, userID string, n int) ([]string, error) {
	if projredis.GetClient() != nil {
		if version, err := cache.GetUserVersion(userID); err == nil {
			key := topGenresCacheKey(userID, version)
			if genres, ok := getCachedTopGenres(key); ok {
				return genres, nil
			}
			genres, err := fetchTopGenres(db, userID, n)
			if err != nil {
				return nil, err
			}
			setCachedTopGenres(key, genres)
			return genres, nil
		}
	}
	return fetchTopGenres(db, userID, n)
}

func getCachedTopGenres(key string) ([]string, bool) {
	data, err := projredis.GetCacheData(key)
	if err != nil {
		return nil, false
	}
	var genres []string
	if err := json.Unmarshal(data, &genres); err != nil {
		log.Printf("[recommendations] cache unmarshal error for key %s: %v", key, err)
		return nil, false
	}
	return genres, true
}

func setCachedTopGenres(key string, genres []string) {
	data, err := json.Marshal(genres)
	if err != nil {
		log.Printf("[recommendations] cache marshal error for key %s: %v", key, err)
		return
	}
	if err := projredis.SetCacheData(key, data, topGenresCacheTTL); err != nil {
		log.Printf("[recommendations] cache set error for key %s: %v", key, err)
	}
}

// ============================================================================
// ERROR HELPERS
// ============================================================================
