package media

// GET /movies/streaming/{period} (issue #69).
//
// "Recently available on streaming services": every movie with a digital
// release inside the period, ranked by how many distinct users watched it in
// that same period. The rank is global (a page-2 row keeps its real rank), and
// delta is always null because the contract's rank movement would need a
// previous period's stored ranks, which we do not keep.

import (
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"
)

// streamingReleaseType is the movie_releases type a digital release is stored
// under.
const streamingReleaseType = "digital"

// streamingNow is a seam so tests can pin the current day.
var streamingNow = time.Now

func init() {
	openapi.Register(openapi.RouteDef{
		Method:      "GET",
		Path:        "/v1/movies/streaming/{period}",
		Summary:     "Movies with a digital release in the period, ranked by watchers in that period",
		Tags:        []string{"Movies"},
		Auth:        "none",
		StatusCodes: map[int]string{200: "Ranked streaming movies"},
	})
}

// registerStreamingRoutes mounts the contract path. It is called before the
// parameterised /movies/:id routes so "streaming" is never read as an id.
func registerStreamingRoutes(movies fiber.Router) {
	movies.Get("/streaming/:period", handleMovieStreaming)
}

// handleMovieStreaming serves one period's ranked list.
func handleMovieStreaming(c *fiber.Ctx) error {
	start, end, ok := streamingPeriodWindow(c.Params("period"), streamingNow())
	if !ok {
		return response.BadRequest(c, "invalid period, expected daily, weekly, or monthly")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	type releaseRow struct {
		MovieID     int64
		ReleaseDate database.Date
	}
	var releases []releaseRow
	if err := db.Table("movie_releases").
		Select("movie_id, MIN(release_date) AS release_date").
		Where("release_type = ? AND release_date >= ? AND release_date < ?",
			streamingReleaseType, start.Format("2006-01-02"), end.Format("2006-01-02")).
		Group("movie_id").
		Scan(&releases).Error; err != nil {
		return response.InternalError(c)
	}

	movieIDs := make([]int64, 0, len(releases))
	for _, release := range releases {
		movieIDs = append(movieIDs, release.MovieID)
	}

	watchers := make(map[int64]int64, len(movieIDs))
	if len(movieIDs) > 0 {
		var rows []struct {
			MediaID  int64
			Watchers int64
		}
		if err := db.Table("user_history").
			Select("media_id, COUNT(DISTINCT user_id) AS watchers").
			Where("media_type = 'movie' AND watched_at >= ? AND watched_at < ? AND media_id IN ?", start, end, movieIDs).
			Group("media_id").
			Scan(&rows).Error; err == nil {
			for _, row := range rows {
				watchers[row.MediaID] = row.Watchers
			}
		}
	}

	sort.SliceStable(releases, func(i, j int) bool {
		left := watchers[releases[i].MovieID]
		right := watchers[releases[j].MovieID]
		if left != right {
			return left > right
		}
		if releases[i].ReleaseDate.String() != releases[j].ReleaseDate.String() {
			return releases[i].ReleaseDate.String() > releases[j].ReleaseDate.String()
		}
		return releases[i].MovieID < releases[j].MovieID
	})

	page, limit := response.ParsePagination(c, 25)
	total := len(releases)
	first := (page - 1) * limit
	if first > total {
		first = total
	}
	last := first + limit
	if last > total {
		last = total
	}
	window := releases[first:last]

	pageIDs := make([]int64, 0, len(window))
	for _, release := range window {
		pageIDs = append(pageIDs, release.MovieID)
	}
	movies := loadMovieRows(pageIDs)

	out := make([]map[string]any, 0, len(window))
	for i, release := range window {
		movie, ok := movies[release.MovieID]
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"rank":  first + i + 1,
			"delta": nil,
			"movie": contractshape.Movie(&movie),
		})
	}

	response.SetPaginationHeaders(c, page, limit, int64(total), len(out))
	return c.JSON(out)
}

// loadMovieRows fetches the page's movies in one query.
func loadMovieRows(ids []int64) map[int64]database.Movie {
	out := make(map[int64]database.Movie, len(ids))
	if len(ids) == 0 {
		return out
	}
	db, err := database.GetDB()
	if err != nil {
		return out
	}
	var movies []database.Movie
	if err := db.Where("id IN ?", ids).Find(&movies).Error; err != nil {
		return out
	}
	for _, movie := range movies {
		out[movie.ID] = movie
	}
	return out
}

// streamingPeriodWindow maps the contract's period enum onto a half-open date
// window ending today: daily is today, weekly is the last 7 days, monthly the
// last 30. The bool is false for an unknown period (the caller answers 400).
func streamingPeriodWindow(period string, now time.Time) (time.Time, time.Time, bool) {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch period {
	case "daily":
		return today, today.AddDate(0, 0, 1), true
	case "weekly":
		return today.AddDate(0, 0, -6), today.AddDate(0, 0, 1), true
	case "monthly":
		return today.AddDate(0, 0, -29), today.AddDate(0, 0, 1), true
	default:
		return time.Time{}, time.Time{}, false
	}
}
