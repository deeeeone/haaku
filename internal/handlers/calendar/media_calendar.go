package calendar

// Merged media calendar and the hot-release feed (issue #69).
//
// /calendars/{target}/media is one dated stream of two kinds of row: movies
// with an availability date (movies.released — the same rows /calendars/
// {target}/movies serves) and episodes with an air date. /calendars/releases/hot
// is the same stream for all users, filtered to titles with a real signal and
// ordered by it: the number of distinct users who have the movie (or the show,
// including via any episode) on a watchlist, in a collection, or in their
// history inside the window.

import (
	"fmt"
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/gorm"
)

func init() {
	for _, target := range []string{"all", "my"} {
		openapi.Register(openapi.RouteDef{
			Method:      "GET",
			Path:        "/v1/calendars/" + target + "/media/{start_date}/{days}",
			Summary:     "Movies and episodes releasing in a date range (contract path)",
			Tags:        []string{"Calendars"},
			Auth:        authForTarget(target),
			StatusCodes: map[int]string{200: "Calendar media entries"},
		})
	}
	openapi.Register(openapi.RouteDef{
		Method:      "GET",
		Path:        "/v1/calendars/releases/hot/{start_date}/{days}",
		Summary:     "Titles with a watchlist, collection or history signal releasing in a date range",
		Tags:        []string{"Calendars"},
		Auth:        "none",
		StatusCodes: map[int]string{200: "Hot release entries"},
	})
}

// registerMediaCalendarRoutes mounts the merged media calendar and the hot feed.
func registerMediaCalendarRoutes(calendars fiber.Router) {
	calendars.Get("/all/media/:start_date/:days", pinCalendarTarget("all"), handleMediaCalendar)
	calendars.Get("/my/media/:start_date/:days", auth.AuthRequired(), pinCalendarTarget("my"), handleMediaCalendar)
	calendars.Get("/releases/hot/:start_date/:days", handleHotCalendar)
}

// calendarMediaEntry is one row plus the sort keys the merged feeds need.
type calendarMediaEntry struct {
	date string
	kind int // 0 = movie, 1 = episode
	id   int64
	body map[string]any
}

// handleMediaCalendar serves the target's movies and episodes in one window.
func handleMediaCalendar(c *fiber.Ctx) error {
	target := calendarTarget(c)
	startDate, days, err := parseCalendarWindow(c)
	if err != nil {
		return err
	}
	typeFilter, ok := parseMediaTypeFilter(c.Query("type"))
	if !ok {
		return response.BadRequest(c, "invalid type filter, expected movie or show")
	}

	userID, _ := c.Locals("user_id").(string)
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	entries, queryErr := mediaEntries(db, target, userID, startDate, startDate.AddDate(0, 0, days), typeFilter)
	if queryErr != nil {
		return response.InternalError(c)
	}
	return c.JSON(mediaEntryBodies(entries))
}

// handleHotCalendar serves the hot feed: the same public rows, filtered to
// titles with at least one watchlist/collection/history user and ranked by that
// count. The response schema has no rank field, so the ordering is the signal.
func handleHotCalendar(c *fiber.Ctx) error {
	startDate, days, err := parseCalendarWindow(c)
	if err != nil {
		return err
	}
	typeFilter, ok := parseMediaTypeFilter(c.Query("type"))
	if !ok {
		return response.BadRequest(c, "invalid type filter, expected movie or show")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	entries, queryErr := mediaEntries(db, "all", "", startDate, startDate.AddDate(0, 0, days), typeFilter)
	if queryErr != nil {
		return response.InternalError(c)
	}

	scores := hotScores(db, entries)
	// A hot feed ranks TITLES, not every episode of a hot show: entries arrive
	// date-ordered, so the first row per movie/show wins the title's place.
	kept := make([]calendarMediaEntry, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		key := mediaEntryKey(entry)
		if seen[key] || scores[key] == 0 {
			continue
		}
		seen[key] = true
		kept = append(kept, entry)
	}
	sort.SliceStable(kept, func(i, j int) bool {
		left := scores[mediaEntryKey(kept[i])]
		right := scores[mediaEntryKey(kept[j])]
		if left != right {
			return left > right
		}
		if kept[i].date != kept[j].date {
			return kept[i].date < kept[j].date
		}
		return kept[i].id < kept[j].id
	})
	return c.JSON(mediaEntryBodies(kept))
}

// parseMediaTypeFilter validates the contract's movie|show enum.
func parseMediaTypeFilter(raw string) (string, bool) {
	switch raw {
	case "", "movie", "show":
		return raw, true
	default:
		return "", false
	}
}

// mediaEntries builds the merged rows for a window, honouring the target and
// the optional type filter.
func mediaEntries(db *gorm.DB, target, userID string, start, end time.Time, typeFilter string) ([]calendarMediaEntry, error) {
	var out []calendarMediaEntry

	if typeFilter != "show" {
		query := db.Where("released >= ? AND released < ?",
			start.Format("2006-01-02"), end.Format("2006-01-02"))
		if target != "all" {
			movieIDs := getMovieIDsForUser(db, userID)
			if len(movieIDs) == 0 {
				query = query.Where("1 = 0")
			} else {
				query = query.Where("id IN ?", movieIDs)
			}
		}
		var movies []database.Movie
		if err := query.Find(&movies).Error; err != nil {
			return nil, err
		}
		for i := range movies {
			movie := &movies[i]
			out = append(out, calendarMediaEntry{
				date: movie.Released.String(),
				kind: 0,
				id:   movie.ID,
				body: map[string]any{
					"released": contractshape.Date(movie.Released),
					"movie":    contractshape.Movie(movie),
				},
			})
		}
	}

	if typeFilter != "movie" {
		query := episodeWindowQuery(db, target, userID, start, end)
		if query != nil {
			var episodes []database.Episode
			if err := query.Order("e.aired_at ASC, e.episode_number ASC").Find(&episodes).Error; err != nil {
				return nil, err
			}
			shows, seasons := fetchEpisodeRelations(db, episodes)
			out = append(out, episodeMediaEntries(episodes, shows, seasons)...)
		}
	}

	return out, nil
}

// episodeMediaEntries shapes episodes into the contract release-item shape.
func episodeMediaEntries(episodes []database.Episode, shows map[int64]database.Show, seasons map[int64]database.Season) []calendarMediaEntry {
	out := make([]calendarMediaEntry, 0, len(episodes))
	for i := range episodes {
		episode := &episodes[i]
		show, ok := shows[episode.ShowID]
		if !ok {
			continue
		}
		seasonNumber := 0
		if season, ok := seasons[episode.SeasonID]; ok {
			seasonNumber = season.SeasonNumber
		}
		out = append(out, calendarMediaEntry{
			date: episode.AiredAt.String(),
			kind: 1,
			id:   episode.ShowID,
			body: map[string]any{
				"first_aired": contractshape.Date(episode.AiredAt),
				"episode":     contractshape.Episode(episode, seasonNumber),
				"show":        contractshape.Show(&show),
			},
		})
	}
	return out
}

// mediaEntryBodies sorts a merged feed by date (then kind and id, for a stable
// order) and returns the contract bodies.
func mediaEntryBodies(entries []calendarMediaEntry) []map[string]any {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].date != entries[j].date {
			return entries[i].date < entries[j].date
		}
		if entries[i].kind != entries[j].kind {
			return entries[i].kind < entries[j].kind
		}
		return entries[i].id < entries[j].id
	})
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.body)
	}
	return out
}

// mediaEntryKey names one row's score: a movie and a show can share an id.
func mediaEntryKey(entry calendarMediaEntry) string {
	return fmt.Sprintf("%d:%d", entry.kind, entry.id)
}

// hotScores counts DISTINCT users per title across watchlist, collection and
// history (and, for shows, users with any episode in their history). A title
// absent from the map has no signal and is not hot.
func hotScores(db *gorm.DB, entries []calendarMediaEntry) map[string]int64 {
	movieIDs := make([]int64, 0, len(entries))
	showIDs := make([]int64, 0, len(entries))
	for _, entry := range entries {
		if entry.kind == 0 {
			movieIDs = append(movieIDs, entry.id)
		} else {
			showIDs = append(showIDs, entry.id)
		}
	}

	scores := make(map[string]int64, len(movieIDs)+len(showIDs))
	if len(movieIDs) > 0 {
		var rows []struct {
			MediaID int64
			Users   int64
		}
		if err := db.Raw(`SELECT media_id, COUNT(DISTINCT user_id) AS users FROM (
			SELECT user_id, media_id FROM user_watchlist WHERE media_type = 'movie'
			UNION SELECT user_id, media_id FROM user_collection WHERE media_type = 'movie'
			UNION SELECT user_id, media_id FROM user_history WHERE media_type = 'movie'
		) u WHERE media_id IN ? GROUP BY media_id`, movieIDs).Scan(&rows).Error; err == nil {
			for _, row := range rows {
				scores[mediaEntryKey(calendarMediaEntry{kind: 0, id: row.MediaID})] = row.Users
			}
		}
	}
	if len(showIDs) > 0 {
		var rows []struct {
			ShowID int64
			Users  int64
		}
		if err := db.Raw(`SELECT show_id, COUNT(DISTINCT user_id) AS users FROM (
			SELECT user_id, media_id AS show_id FROM user_watchlist WHERE media_type = 'show'
			UNION SELECT user_id, media_id AS show_id FROM user_collection WHERE media_type = 'show'
			UNION SELECT user_id, media_id AS show_id FROM user_history WHERE media_type = 'show'
			UNION SELECT uh.user_id AS user_id, e.show_id AS show_id
				FROM user_history uh JOIN episodes e ON e.id = uh.media_id
				WHERE uh.media_type = 'episode'
		) u WHERE show_id IN ? GROUP BY show_id`, showIDs).Scan(&rows).Error; err == nil {
			for _, row := range rows {
				scores[mediaEntryKey(calendarMediaEntry{kind: 1, id: row.ShowID})] = row.Users
			}
		}
	}
	return scores
}
