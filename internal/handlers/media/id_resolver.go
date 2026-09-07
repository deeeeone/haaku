package media

import (
	"errors"
	"strconv"

	"trakt-api/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"trakt-api/internal/response"
)

// ResolveID resolves a route param to a canonical media ID and type.
//
// Supported input forms (per api-conventions.md §12):
//   - Numeric id: /movies/12345 → queries the movies table directly.
//   - Slug: /movies/breaking-bad-2008 → queries external_ids(source='slug').
//   - External with ?id_type=: /movies/tt0903747?id_type=imdb → queries external_ids
//     by source + external_id. Valid id_type values: tmdb, imdb, tvdb.
//
// Returns (mediaType, canonicalID, nil) on success.
// On failure, calls the httpapi error helpers and returns an error the caller
// should propagate to the Fiber handler return.
func ResolveID(c *fiber.Ctx, param string) (mediaType string, id int64, err error) {
	// Check for ?id_type= query param (external ID lookup).
	idType := c.Query("id_type")
	if idType != "" {
		return resolveByExternalID(c, param, idType)
	}

	// Check if param is purely numeric.
	if isNumeric(param) {
		id, err := strconv.ParseInt(param, 10, 64)
		if err != nil {
			return "", 0, response.NotFound(c, "media not found")
		}
		// Determine media type by querying both tables.
		mt, found, dbErr := findMediaTypeByID(id)
		if dbErr != nil {
			return "", 0, dbErr
		}
		if !found {
			return "", 0, response.NotFound(c, "media not found")
		}
		return mt, id, nil
	}

	// Otherwise treat as slug.
	return resolveBySlug(c, param)
}

// isNumeric returns true if s contains only ASCII digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// resolveByExternalID handles ?id_type=tmdb|imdb|tvdb lookups.
func resolveByExternalID(c *fiber.Ctx, externalID string, idType string) (string, int64, error) {
	// Validate id_type.
	switch idType {
	case "tmdb", "imdb", "tvdb":
		// valid
	default:
		return "", 0, response.BadRequest(c, "invalid id_type: must be tmdb, imdb, or tvdb")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return "", 0, fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var ext database.ExternalID
	if err := db.Where("source = ? AND external_id = ?", idType, externalID).First(&ext).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", 0, response.NotFound(c, "media not found")
		}
		return "", 0, fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	// Verify the media row actually exists.
	if _, found, verr := findMediaTypeByID(ext.MediaID); verr != nil {
		return "", 0, verr
	} else if !found {
		return "", 0, response.NotFound(c, "media not found")
	}

	return ext.MediaType, ext.MediaID, nil
}

// resolveBySlug handles slug lookups.
func resolveBySlug(c *fiber.Ctx, slug string) (string, int64, error) {
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return "", 0, fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var ext database.ExternalID
	if err := db.Where("source = ? AND external_id = ?", "slug", slug).First(&ext).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", 0, response.NotFound(c, "media not found")
		}
		return "", 0, fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	// Verify the media row actually exists.
	if _, found, verr := findMediaTypeByID(ext.MediaID); verr != nil {
		return "", 0, verr
	} else if !found {
		return "", 0, response.NotFound(c, "media not found")
	}

	return ext.MediaType, ext.MediaID, nil
}

// findMediaTypeByID checks the movies, shows, and people tables to determine the media
// type for a given canonical ID. Returns (mediaType, found, error).
func findMediaTypeByID(id int64) (string, bool, error) {
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return "", false, fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	// Check movies first.
	var count int64
	if err := db.Model(&database.Movie{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return "", false, fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	if count > 0 {
		return "movie", true, nil
	}

	// Check shows.
	if err := db.Model(&database.Show{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return "", false, fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	if count > 0 {
		return "show", true, nil
	}

	// Check people.
	if err := db.Model(&database.Person{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return "", false, fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	if count > 0 {
		return "person", true, nil
	}

	return "", false, nil
}

// ResolvedMedia is the canonical identity of a resolved media entity.
type ResolvedMedia struct {
	MediaType string
	MediaID   int64
}

// ResolveTMDBBatch resolves a set of TMDB ids to canonical media refs using a
// bounded number of queries (one external_ids lookup plus at most one existence
// check per media type), avoiding the per-item N+1 that a loop of single
// resolutions would incur. Unresolvable or missing ids are simply absent from
// the returned map; callers treat an absent key as "not found".
func ResolveTMDBBatch(db *gorm.DB, tmdbIDs []int) (map[int]ResolvedMedia, error) {
	result := make(map[int]ResolvedMedia, len(tmdbIDs))
	if len(tmdbIDs) == 0 {
		return result, nil
	}

	// Deduplicate and drop zero ids.
	seen := make(map[int]bool, len(tmdbIDs))
	keys := make([]string, 0, len(tmdbIDs))
	for _, id := range tmdbIDs {
		if id != 0 && !seen[id] {
			seen[id] = true
			keys = append(keys, strconv.Itoa(id))
		}
	}
	if len(keys) == 0 {
		return result, nil
	}

	var exts []database.ExternalID
	if err := db.Where("source = ? AND external_id IN ?", "tmdb", keys).Find(&exts).Error; err != nil {
		return nil, err
	}

	type row struct {
		key       int
		mediaType string
		mediaID   int64
	}
	rows := make([]row, 0, len(exts))
	var movies, shows, seasons, episodes []int64
	for _, e := range exts {
		key, _ := strconv.Atoi(e.ExternalID)
		rows = append(rows, row{key: key, mediaType: e.MediaType, mediaID: e.MediaID})
		switch e.MediaType {
		case "movie":
			movies = append(movies, e.MediaID)
		case "show":
			shows = append(shows, e.MediaID)
		case "season":
			seasons = append(seasons, e.MediaID)
		case "episode":
			episodes = append(episodes, e.MediaID)
		}
	}

	existing := verifyMediaBatch(db, movies, shows, seasons, episodes)
	for _, r := range rows {
		if existing[r.mediaType][r.mediaID] {
			result[r.key] = ResolvedMedia{MediaType: r.mediaType, MediaID: r.mediaID}
		}
	}
	return result, nil
}

// verifyMediaBatch reports which of the given media ids actually exist, grouped
// by media type. Each media type is checked with at most one query.
func verifyMediaBatch(db *gorm.DB, movies, shows, seasons, episodes []int64) map[string]map[int64]bool {
	existing := make(map[string]map[int64]bool, 4)
	check := func(table interface{}, name string, ids []int64) {
		if len(ids) == 0 {
			existing[name] = map[int64]bool{}
			return
		}
		var found []int64
		if err := db.Model(table).Where("id IN ?", ids).Distinct("id").Pluck("id", &found).Error; err != nil {
			existing[name] = map[int64]bool{}
			return
		}
		set := make(map[int64]bool, len(found))
		for _, id := range found {
			set[id] = true
		}
		existing[name] = set
	}
	check(&database.Movie{}, "movie", movies)
	check(&database.Show{}, "show", shows)
	check(&database.Season{}, "season", seasons)
	check(&database.Episode{}, "episode", episodes)
	return existing
}

// --- Error helpers (thin wrappers around httpapi) ---
