package httpapi

import (
	"strings"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"

	"github.com/gofiber/fiber/v2"
)

// handleMediaResolve handles GET /v1/media/resolve.
//
// This is the ingest-on-demand door, and it is what makes the search page
// usable end to end: /v1/search answers with live TVDB results that are NOT in
// the local catalogue yet, and every other endpoint addresses media by its
// local row id (watchlist, watching, ratings, comments). Resolving fetches the
// TVDB record, merges the TMDB supplement, writes the movie/show with its
// seasons and episodes, and returns the canonical row in the same shape
// /v1/movies/{id} returns — so the client just navigates.
//
// It is idempotent: a title already in the catalogue is returned without a
// provider call, which is also why it is safe for the UI to call it on every
// click rather than tracking what it has already resolved.
func handleMediaResolve(c *fiber.Ctx) error {
	mediaType := strings.ToLower(strings.TrimSpace(c.Query("type")))
	if mediaType != "movie" && mediaType != "show" {
		return badRequest(c, "type must be one of: movie, show")
	}

	idType := strings.ToLower(strings.TrimSpace(c.Query("id_type", "tvdb")))
	switch idType {
	case "tvdb", "tmdb", "imdb":
	default:
		return badRequest(c, "id_type must be one of: tvdb, tmdb, imdb")
	}

	externalID := strings.TrimSpace(c.Query("id"))
	if externalID == "" {
		return badRequest(c, "id parameter is required")
	}

	resolver, err := metadataResolver()
	if err != nil {
		return writeError(c, fiber.StatusServiceUnavailable, "provider_unavailable",
			"metadata provider is not configured")
	}

	resolvedType, mediaID, err := resolver.ResolveByExternalID(c.UserContext(), mediaType, idType, externalID)
	if err != nil {
		status, code, message := metadataStatusFor(err)
		return writeError(c, status, code, message)
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Answer with the SAME payload the media endpoints use, so a client can
	// navigate to /movies/{id} or /shows/{id} with what it already has.
	if resolvedType == "movie" {
		var movie database.Movie
		if err := db.Where("id = ?", mediaID).First(&movie).Error; err != nil {
			return writeError(c, fiber.StatusInternalServerError, "internal", "resolved movie could not be read back")
		}
		return c.JSON(media.BuildMovieResponse(&movie, "full"))
	}

	var show database.Show
	if err := db.Where("id = ?", mediaID).First(&show).Error; err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "resolved show could not be read back")
	}
	return c.JSON(media.BuildShowResponse(&show, "full"))
}
