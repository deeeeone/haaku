package discovery

import (
	"sort"
	"time"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped anticipated feeds.
//
// Trakt's /movies/anticipated and /shows/anticipated are separate paths; our
// media-wide /v1/media/anticipated takes ?type=. These routes serve the same
// watchlist-weighted anticipation from the same tables, emitting the contract's
// {list_count, movie|show} array. They are registered in the discovery group
// (before media's /movies/:id) so "anticipated" is not captured as an id.

func init() {
	for _, typ := range []string{"movies", "shows"} {
		openapi.Register(openapi.RouteDef{
			Method:  "GET",
			Path:    "/v1/" + typ + "/anticipated",
			Summary: "Anticipated upcoming " + typ + " weighted by watchlist interest (contract path)",
			Tags:    []string{titleOf(typ)},
			Auth:    "optional",
			StatusCodes: map[int]string{
				200: "Anticipated items",
			},
		})
	}
}

func titleOf(plural string) string {
	if plural == "movies" {
		return "Movies"
	}
	return "Shows"
}

// registerContractAnticipatedRoutes mounts the contract-shaped anticipated feeds.
func registerContractAnticipatedRoutes(api fiber.Router) {
	api.Get("/movies/anticipated", handleContractAnticipatedMovies)
	api.Get("/shows/anticipated", handleContractAnticipatedShows)
}

func handleContractAnticipatedMovies(c *fiber.Ctx) error {
	return serveAnticipated(c, "movie")
}

func handleContractAnticipatedShows(c *fiber.Ctx) error {
	return serveAnticipated(c, "show")
}

// serveAnticipated answers one anticipated feed from real rows. Score follows
// our media-wide feed: watchlist_count*0.7 + popularity*0.3.
func serveAnticipated(c *fiber.Ctx, mediaType string) error {
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c, "database unavailable")
	}
	_, limit := response.ParsePagination(c, 20)

	switch mediaType {
	case "movie":
		now := time.Now().UTC()
		from := now.AddDate(0, 0, -30).Format("2006-01-02")
		to := now.AddDate(0, 0, 90).Format("2006-01-02")
		var movies []database.Movie
		if err := db.Where("released >= ? AND released <= ?", from, to).
			Order("popularity DESC").Limit(limit * 3).Find(&movies).Error; err != nil {
			return response.InternalError(c, "database query error")
		}
		counts := watchlistCounts(db, "movie", movieIDs(movies))
		type scored struct {
			score float64
			movie *database.Movie
		}
		items := make([]scored, 0, len(movies))
		for i := range movies {
			score := float64(counts[movies[i].ID])*0.7 + movies[i].Popularity*0.3
			items = append(items, scored{score: score, movie: &movies[i]})
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].score > items[j].score })
		if len(items) > limit {
			items = items[:limit]
		}
		out := make([]map[string]any, 0, len(items))
		for _, it := range items {
			out = append(out, map[string]any{
				"list_count": counts[it.movie.ID],
				"movie":      contractshape.Movie(it.movie),
			})
		}
		return c.JSON(out)

	case "show":
		today := time.Now().UTC().Format("2006-01-02")
		var shows []database.Show
		if err := db.Where("id IN (SELECT show_id FROM episodes WHERE aired_at >= ?)", today).
			Order("popularity DESC").Limit(limit * 3).Find(&shows).Error; err != nil {
			return response.InternalError(c, "database query error")
		}
		counts := watchlistCounts(db, "show", showIDs(shows))
		type scored struct {
			score float64
			show  *database.Show
		}
		items := make([]scored, 0, len(shows))
		for i := range shows {
			score := float64(counts[shows[i].ID])*0.7 + shows[i].Popularity*0.3
			items = append(items, scored{score: score, show: &shows[i]})
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].score > items[j].score })
		if len(items) > limit {
			items = items[:limit]
		}
		out := make([]map[string]any, 0, len(items))
		for _, it := range items {
			out = append(out, map[string]any{
				"list_count": counts[it.show.ID],
				"show":       contractshape.Show(it.show),
			})
		}
		return c.JSON(out)
	}
	return response.BadRequest(c, "unsupported media type")
}

func watchlistCounts(db *gorm.DB, mediaType string, ids []int64) map[int64]int {
	counts := make(map[int64]int, len(ids))
	if len(ids) == 0 {
		return counts
	}
	var rows []struct {
		MediaID int64
		Cnt     int
	}
	db.Model(&database.UserWatchlist{}).
		Select("media_id, COUNT(*) AS cnt").
		Where("media_type = ? AND media_id IN ?", mediaType, ids).
		Group("media_id").
		Scan(&rows)
	for _, r := range rows {
		counts[r.MediaID] = r.Cnt
	}
	return counts
}

func movieIDs(movies []database.Movie) []int64 {
	ids := make([]int64, 0, len(movies))
	for i := range movies {
		ids = append(ids, movies[i].ID)
	}
	return ids
}

func showIDs(shows []database.Show) []int64 {
	ids := make([]int64, 0, len(shows))
	for i := range shows {
		ids = append(ids, shows[i].ID)
	}
	return ids
}
