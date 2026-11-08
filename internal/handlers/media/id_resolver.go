package media

import (
	"errors"
	"strconv"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/gorm"
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
type ResolvedMedia = database.MediaRef

// IDRef is one item's external-id set as a client sent it; see
// database.ResolveIDRefs for the resolution rules (TVDB first, then TMDB, then
// IMDb, reading both the media columns and the external_ids table).
type IDRef = database.IDRef

// ResolveIDsBatch resolves a batch of external-id sets to canonical media refs.
// The implementation lives in internal/database so the metadata resolver can
// apply the same rules without importing a handler package.
func ResolveIDsBatch(db *gorm.DB, refs []IDRef) (map[IDRef]ResolvedMedia, error) {
	return database.ResolveIDRefs(db, refs)
}

// ResolveTMDBBatch resolves a set of TMDB ids to canonical media refs, keyed by
// TMDB id. It is ResolveIDsBatch restricted to one id source, kept for the
// callers that only ever have TMDB ids.
func ResolveTMDBBatch(db *gorm.DB, tmdbIDs []int) (map[int]ResolvedMedia, error) {
	result := make(map[int]ResolvedMedia, len(tmdbIDs))
	if len(tmdbIDs) == 0 {
		return result, nil
	}

	refs := make([]IDRef, 0, len(tmdbIDs))
	for _, id := range tmdbIDs {
		refs = append(refs, IDRef{TMDB: id})
	}
	resolved, err := ResolveIDsBatch(db, refs)
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		if mediaID, ok := resolved[ref]; ok {
			result[ref.TMDB] = mediaID
		}
	}
	return result, nil
}
