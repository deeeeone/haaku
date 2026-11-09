package calendar

// DVD and streaming release calendars (issue #69).
//
// Both are the same query over movie_releases with a different release type:
// physical for the DVD feed, digital for streaming. One movie yields at most one
// entry per feed — its earliest date of that type in the window (and in the
// requested countries, when ?countries= narrows it) — because Trakt's movie
// calendar lists titles, not individual country releases.

import (
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/gorm"
)

// Release types the calendars select (Trakt's vocabulary).
const (
	calendarReleasePhysical = "physical"
	calendarReleaseDigital  = "digital"
)

var releaseCalendarFeeds = []struct {
	segment     string
	releaseType string
	summary     string
}{
	{segment: "dvd", releaseType: calendarReleasePhysical, summary: "DVD/physical release calendar (contract path)"},
	{segment: "streaming", releaseType: calendarReleaseDigital, summary: "Streaming release calendar (contract path)"},
}

func init() {
	for _, feed := range releaseCalendarFeeds {
		for _, target := range []string{"all", "my"} {
			openapi.Register(openapi.RouteDef{
				Method:      "GET",
				Path:        "/v1/calendars/" + target + "/" + feed.segment + "/{start_date}/{days}",
				Summary:     feed.summary,
				Tags:        []string{"Calendars"},
				Auth:        authForTarget(target),
				StatusCodes: map[int]string{200: "Calendar movie entries"},
			})
		}
	}
}

// registerReleaseCalendarRoutes mounts the DVD/streaming calendars.
func registerReleaseCalendarRoutes(calendars fiber.Router) {
	for _, feed := range releaseCalendarFeeds {
		calendars.Get("/all/"+feed.segment+"/:start_date/:days",
			pinCalendarTarget("all"), handleReleaseCalendar(feed.releaseType))
		calendars.Get("/my/"+feed.segment+"/:start_date/:days",
			auth.AuthRequired(), pinCalendarTarget("my"), handleReleaseCalendar(feed.releaseType))
	}
}

// handleReleaseCalendar serves one release type for the target pinned on the
// route.
func handleReleaseCalendar(releaseType string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		target := calendarTarget(c)
		startDate, days, err := parseCalendarWindow(c)
		if err != nil {
			return err
		}
		endDate := startDate.AddDate(0, 0, days)

		userID, _ := c.Locals("user_id").(string)
		db, dbErr := database.GetDB()
		if dbErr != nil {
			return response.ServiceUnavailable(c, "database unavailable")
		}

		countries, ok := parseCountries(c.Query("countries"))
		if !ok {
			// A filter that names no valid country can match nothing; an empty
			// array is the honest answer, not an unfiltered calendar.
			return c.JSON([]map[string]any{})
		}

		var restrict []int64
		if target == "my" {
			restrict = getMovieIDsForUser(db, userID)
			if len(restrict) == 0 {
				return c.JSON([]map[string]any{})
			}
		}

		releases, err := releaseWindow(db, releaseType, startDate, endDate, countries, restrict)
		if err != nil {
			return response.InternalError(c)
		}

		ids := make([]int64, 0, len(releases))
		for _, release := range releases {
			ids = append(ids, release.MovieID)
		}
		movies := loadMoviesByID(db, ids)

		out := make([]map[string]any, 0, len(releases))
		for _, release := range releases {
			movie, ok := movies[release.MovieID]
			if !ok {
				continue
			}
			out = append(out, map[string]any{
				"released": contractshape.Date(&release.ReleaseDate),
				"movie":    contractshape.Movie(&movie),
			})
		}
		return c.JSON(out)
	}
}

// releaseWindowRow is one movie's earliest matching release in the window.
type releaseWindowRow struct {
	MovieID     int64
	ReleaseDate database.Date
}

// releaseWindow groups movie_releases by movie and takes the earliest date in
// the window, optionally narrowed to a set of countries and/or user movies.
func releaseWindow(db *gorm.DB, releaseType string, start, end time.Time, countries []string, restrict []int64) ([]releaseWindowRow, error) {
	query := db.Table("movie_releases").
		Select("movie_id, MIN(release_date) AS release_date").
		Where("release_type = ? AND release_date >= ? AND release_date < ?",
			releaseType, start.Format("2006-01-02"), end.Format("2006-01-02")).
		Group("movie_id")
	if len(countries) > 0 {
		query = query.Where("country IN ?", countries)
	}
	if restrict != nil {
		query = query.Where("movie_id IN ?", restrict)
	}

	var rows []releaseWindowRow
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].ReleaseDate.String() != rows[j].ReleaseDate.String() {
			return rows[i].ReleaseDate.String() < rows[j].ReleaseDate.String()
		}
		return rows[i].MovieID < rows[j].MovieID
	})
	return rows, nil
}

// parseCountries reads the contract's comma-separated ISO 3166-1 codes. The
// bool is false when the query named something but none of it was a country,
// which the caller renders as an empty calendar.
func parseCountries(raw string) ([]string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	out := make([]string, 0, 4)
	seen := make(map[string]bool, 4)
	for _, part := range strings.Split(raw, ",") {
		code := strings.ToLower(strings.TrimSpace(part))
		if len(code) != 2 || seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// loadMoviesByID fetches movies in one query and indexes them for the caller.
func loadMoviesByID(db *gorm.DB, ids []int64) map[int64]database.Movie {
	out := make(map[int64]database.Movie, len(ids))
	if len(ids) == 0 {
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
