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
//
// This form is TYPE-AGNOSTIC and exists for the callers that genuinely cannot
// know the type — an id carried in a request body (a sync item, a scrobble).
// A route that names the media type must use ResolveIDAs instead: a bare
// numeric id is not unique across media types (movies, shows and people are
// separate sequences, each starting at 1), so a type-agnostic lookup has to
// pick one table and the others become unreachable at that id.
func ResolveID(c *fiber.Ctx, param string) (mediaType string, id int64, err error) {
	return resolveIDParam(c, param, "")
}

// ResolveIDAs resolves a route's :id param to a row of the expected media type.
//
// The route that names the type is authoritative: /shows/1 is show row 1, even
// when movie row 1 exists. An id that exists only as another media type keeps
// answering the same 404 the handlers used to write themselves ("not a show"),
// and an id that exists nowhere answers "media not found" — so the wrong-type
// case is still distinguishable from the missing one.
func ResolveIDAs(c *fiber.Ctx, param, want string) (mediaType string, id int64, err error) {
	if want == "" {
		return "", 0, fiber.NewError(fiber.StatusInternalServerError, "ResolveIDAs requires a media type")
	}
	return resolveIDParam(c, param, want)
}

// resolveIDParam is the one resolution path. want scopes it to a single media
// type; "" resolves against every table.
func resolveIDParam(c *fiber.Ctx, param, want string) (string, int64, error) {
	// Check for ?id_type= query param (external ID lookup).
	if idType := c.Query("id_type"); idType != "" {
		return resolveByExternalID(c, param, idType, want)
	}

	// Check if param is purely numeric.
	if isNumeric(param) {
		id, err := strconv.ParseInt(param, 10, 64)
		if err != nil {
			return notFound(c, "media not found")
		}
		return resolveNumericID(c, id, want)
	}

	// Otherwise treat as slug.
	return resolveBySlug(c, param, want)
}

// notFound writes the 404 envelope and returns ErrHandled.
//
// The envelope writers return whatever c.JSON returns, which is nil on success,
// so a resolver that wrote its own error and handed that nil back let the
// handler run on with id 0 — the caller's `if err != nil` never fired.
func notFound(c *fiber.Ctx, message string) (string, int64, error) {
	_ = response.NotFound(c, message)
	return "", 0, response.ErrHandled
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

// resolveNumericID resolves a bare numeric id. With a wanted type the id is
// looked up in that type's table first, so two media types that share a row id
// cannot shadow each other; the type-agnostic form keeps the movie → show →
// person precedence it always had.
func resolveNumericID(c *fiber.Ctx, id int64, want string) (string, int64, error) {
	if want != "" {
		found, err := mediaRowExists(want, id)
		if err != nil {
			return "", 0, err
		}
		if found {
			return want, id, nil
		}
	}

	// Not the wanted type (or no type was wanted): find out what it is, to say
	// whether it is missing outright or simply a different media type.
	existingType, found, err := findMediaTypeByID(id)
	if err != nil {
		return "", 0, err
	}
	if !found {
		return notFound(c, "media not found")
	}
	if want != "" {
		return notFound(c, "not a "+want)
	}
	return existingType, id, nil
}

// resolveByExternalID handles ?id_type=tmdb|imdb|tvdb lookups.
func resolveByExternalID(c *fiber.Ctx, externalID string, idType, want string) (string, int64, error) {
	// Validate id_type.
	switch idType {
	case "tmdb", "imdb", "tvdb":
		// valid
	default:
		_ = response.BadRequest(c, "invalid id_type: must be tmdb, imdb, or tvdb")
		return "", 0, response.ErrHandled
	}

	return resolveExternalRow(c, idType, externalID, want)
}

// resolveBySlug handles slug lookups.
func resolveBySlug(c *fiber.Ctx, slug, want string) (string, int64, error) {
	return resolveExternalRow(c, "slug", slug, want)
}

// resolveExternalRow resolves an external_ids row and verifies the media row it
// points at exists.
//
// want scopes the lookup to one media type, which matters because an external
// id is not unique to a type either: the same TMDB id can sit on both a movie
// and a show row, and the same slug can belong to both (a movie and a series
// named the same thing). Without the scope the first row wins and the other
// media type is unreachable through its own route.
func resolveExternalRow(c *fiber.Ctx, source, externalID, want string) (string, int64, error) {
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return "", 0, fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	query := db.Where("source = ? AND external_id = ?", source, externalID)
	if want != "" {
		query = query.Where("media_type = ?", want)
	}

	var ext database.ExternalID
	if err := query.First(&ext).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", 0, fiber.NewError(fiber.StatusInternalServerError, "database query error")
		}
		if want != "" {
			// The id may belong to another media type: that is a wrong-type
			// answer, not a missing one.
			var other database.ExternalID
			if otherErr := db.Where("source = ? AND external_id = ?", source, externalID).
				First(&other).Error; otherErr == nil {
				return notFound(c, "not a "+want)
			}
		}
		return notFound(c, "media not found")
	}

	// The row the id points at must actually exist: a stale external_ids row
	// must not resolve to a phantom.
	mediaType := ext.MediaType
	if want != "" {
		mediaType = want
	}
	found, verr := mediaRowExists(mediaType, ext.MediaID)
	if verr != nil {
		return "", 0, verr
	}
	if !found {
		return notFound(c, "media not found")
	}

	return mediaType, ext.MediaID, nil
}

// mediaTypeModels maps a media type to the table it lives in.
var mediaTypeModels = map[string]func() interface{}{
	"movie":  func() interface{} { return &database.Movie{} },
	"show":   func() interface{} { return &database.Show{} },
	"person": func() interface{} { return &database.Person{} },
}

// mediaRowExists reports whether a row with this id exists in that type's table.
func mediaRowExists(mediaType string, id int64) (bool, error) {
	model, ok := mediaTypeModels[mediaType]
	if !ok {
		return false, fiber.NewError(fiber.StatusInternalServerError, "unknown media type: "+mediaType)
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return false, fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var count int64
	if err := db.Model(model()).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	return count > 0, nil
}

// findMediaTypeByID checks the movies, shows, and people tables to determine the media
// type for a given canonical ID. Returns (mediaType, found, error).
//
// Movie first is the precedence the type-agnostic callers have always had; it
// is only reachable for ids whose type the caller does not know.
func findMediaTypeByID(id int64) (string, bool, error) {
	for _, mediaType := range []string{"movie", "show", "person"} {
		found, err := mediaRowExists(mediaType, id)
		if err != nil {
			return "", false, err
		}
		if found {
			return mediaType, true, nil
		}
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
