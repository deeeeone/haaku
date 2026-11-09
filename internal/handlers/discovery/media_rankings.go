package discovery

// Cross-type media feeds (issue #66): GET /v1/media/trending and
// GET /v1/media/popular.
//
// Both merge movies and shows into one list. Ordering is the existing
// discovery signal (popularity DESC), reused through the same movie/show row
// loaders rather than a second ranking definition. Trending carries the
// contract's `watchers` counter, computed from distinct users with history in
// the last 24 hours; popular carries the bare movie/show object the contract's
// union allows.

import (
	"fmt"
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"gorm.io/gorm"
)

func init() {
	routes := []openapi.RouteDef{
		{
			Method:      "GET",
			Path:        "/v1/media/trending",
			Summary:     "Trending movies and shows merged into one feed",
			Tags:        []string{"Media"},
			Auth:        "none",
			StatusCodes: map[int]string{200: "Trending media"},
		},
		{
			Method:      "GET",
			Path:        "/v1/media/popular",
			Summary:     "Popular movies and shows merged into one feed",
			Tags:        []string{"Media"},
			Auth:        "none",
			StatusCodes: map[int]string{200: "Popular media"},
		},
	}
	for _, route := range routes {
		openapi.Register(route)
	}
}

func registerMediaRankingRoutes(api fiber.Router) {
	media := api.Group("/media")
	media.Get("/trending", handleMediaTrending)
	media.Get("/popular", handleMediaPopular)
}

func handleMediaTrending(c *fiber.Ctx) error {
	return serveMediaList(c, "trending")
}

func handleMediaPopular(c *fiber.Ctx) error {
	return serveMediaList(c, "popular")
}

// mediaItem is one candidate in the merged feed.
type mediaItem struct {
	kind       string
	id         int64
	popularity float64
}

func serveMediaList(c *fiber.Ctx, kind string) error {
	page, limit := response.ParsePagination(c, 25)

	cacheKey := fmt.Sprintf("media:%s:page%d:limit%d", kind, page, limit)
	if data, ok := discoveryGetCache(cacheKey); ok {
		c.Set("Content-Type", "application/json")
		return c.Send(data)
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	var movieTotal, showTotal int64
	if err := db.Model(&database.Movie{}).Count(&movieTotal).Error; err != nil {
		return response.InternalError(c, "database query error")
	}
	if err := db.Model(&database.Show{}).Count(&showTotal).Error; err != nil {
		return response.InternalError(c, "database query error")
	}
	total := movieTotal + showTotal

	offset := int64(page-1) * int64(limit)
	if offset >= total {
		out := make([]map[string]any, 0)
		response.SetPaginationHeaders(c, page, limit, total, 0)
		discoverySetCache(cacheKey, out)
		return c.JSON(out)
	}

	take := offset + int64(limit)
	if take > total {
		take = total
	}
	// A merge's first N entries cannot need more than N from either side.
	items, err := mergedMedia(db, int(take))
	if err != nil {
		return response.InternalError(c, "database query error")
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].popularity != items[j].popularity {
			return items[i].popularity > items[j].popularity
		}
		if items[i].kind != items[j].kind {
			return items[i].kind < items[j].kind
		}
		return items[i].id < items[j].id
	})

	first := int(offset)
	last := first + limit
	if last > len(items) {
		last = len(items)
	}
	window := items[first:last]

	movieIDs := make([]int64, 0, len(window))
	showIDs := make([]int64, 0, len(window))
	for _, item := range window {
		if item.kind == "movie" {
			movieIDs = append(movieIDs, item.id)
		} else {
			showIDs = append(showIDs, item.id)
		}
	}
	movies := loadMovieMap(db, movieIDs)
	shows := loadShowMap(db, showIDs)

	watchers := map[int64]int64{}
	if kind == "trending" {
		watchers, err = recentWatchers(db, rankingsNow().Add(-24*time.Hour), movieIDs, showIDs)
		if err != nil {
			return response.InternalError(c, "database query error")
		}
	}

	out := make([]map[string]any, 0, len(window))
	for _, item := range window {
		if item.kind == "movie" {
			movie, found := movies[item.id]
			if !found {
				continue
			}
			media := contractshape.Movie(&movie)
			if kind == "trending" {
				out = append(out, map[string]any{
					"watchers": watchers[item.id],
					"movie":    media,
				})
			} else {
				out = append(out, media)
			}
			continue
		}

		show, found := shows[item.id]
		if !found {
			continue
		}
		media := contractshape.Show(&show)
		if kind == "trending" {
			out = append(out, map[string]any{
				"watchers": watchers[-item.id],
				"show":     media,
			})
		} else {
			out = append(out, media)
		}
	}

	response.SetPaginationHeaders(c, page, limit, total, len(out))
	discoverySetCache(cacheKey, out)
	return c.JSON(out)
}

// mergedMedia fetches the first take movies and shows by the shared popularity
// signal. Taking `take` from each side is enough for any merged prefix of that
// length.
func mergedMedia(db *gorm.DB, take int) ([]mediaItem, error) {
	var movies []database.Movie
	if err := db.Select("id, popularity").Order("popularity DESC, id ASC").
		Limit(take).Find(&movies).Error; err != nil {
		return nil, err
	}
	var shows []database.Show
	if err := db.Select("id, popularity").Order("popularity DESC, id ASC").
		Limit(take).Find(&shows).Error; err != nil {
		return nil, err
	}

	items := make([]mediaItem, 0, len(movies)+len(shows))
	for _, movie := range movies {
		items = append(items, mediaItem{kind: "movie", id: movie.ID, popularity: movie.Popularity})
	}
	for _, show := range shows {
		items = append(items, mediaItem{kind: "show", id: show.ID, popularity: show.Popularity})
	}
	return items, nil
}

// recentWatchers counts distinct users with history in the window for the given
// movies and shows. Plays may be recorded against a show itself or one of its
// episodes; both forms are unioned before COUNT(DISTINCT user_id). Show counts
// are keyed by the negative id so one map can hold both media types.
func recentWatchers(db *gorm.DB, start time.Time, movieIDs, showIDs []int64) (map[int64]int64, error) {
	watchers := make(map[int64]int64, len(movieIDs)+len(showIDs))

	if len(movieIDs) > 0 {
		var rows []struct {
			MediaID  int64
			Watchers int64
		}
		if err := db.Table("user_history").
			Select("media_id, COUNT(DISTINCT user_id) AS watchers").
			Where("media_type = 'movie' AND watched_at >= ? AND media_id IN ?", start, movieIDs).
			Group("media_id").Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			watchers[row.MediaID] = row.Watchers
		}
	}

	if len(showIDs) > 0 {
		var rows []struct {
			ShowID   int64
			Watchers int64
		}
		// A show watch can be stored on the show or on one of its episodes.
		// UNION ALL then COUNT(DISTINCT user_id) counts a user once even when
		// both forms exist.
		if err := db.Raw(`
			SELECT show_id, COUNT(DISTINCT user_id) AS watchers FROM (
				SELECT e.show_id AS show_id, h.user_id
				FROM user_history h JOIN episodes e ON e.id = h.media_id
				WHERE h.media_type = 'episode' AND h.watched_at >= ? AND e.show_id IN ?
				UNION ALL
				SELECT h.media_id AS show_id, h.user_id
				FROM user_history h
				WHERE h.media_type = 'show' AND h.watched_at >= ? AND h.media_id IN ?
			) watched GROUP BY show_id
		`, start, showIDs, start, showIDs).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			watchers[-row.ShowID] = row.Watchers
		}
	}

	return watchers, nil
}
