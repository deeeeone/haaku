package notes

import (
	"errors"
	"strconv"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped note subject read.
//
// Trakt's GET /notes/{id}/item returns the media the note is attached to: an
// object with a `type` plus the standard media object for that type. Our notes
// store the target as (media_type, media_id) on user_notes, so this resolves
// the real row rather than echoing the stored pair.

func init() {
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/notes/{id}/item",
		Summary: "The media item a note is attached to (owner only)",
		Tags:    []string{"Notes"},
		Auth:    "required",
		StatusCodes: map[int]string{
			200: "Attached item",
			403: "Not your note",
			404: "Note or item not found",
		},
	})
}

// handleGetNoteItem serves GET /v1/notes/{id}/item.
func handleGetNoteItem(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return response.BadRequest(c, "invalid note id")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var note database.UserNote
	if err := db.First(&note, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "note not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}
	if note.UserID != userID {
		return response.Forbidden(c, "note belongs to another user")
	}

	item, itemErr := noteSubjectItem(db, &note)
	if itemErr != nil {
		return response.InternalError(c, "database query error")
	}
	if item == nil {
		return response.NotFound(c, "note's media item not found")
	}
	return c.JSON(item)
}

// noteSubjectItem loads the media row a note points at and renders the
// contract's item object. A nil map means the referenced row does not exist.
func noteSubjectItem(db *gorm.DB, note *database.UserNote) (map[string]any, error) {
	switch note.MediaType {
	case "movie":
		var m database.Movie
		if err := db.First(&m, note.MediaID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return map[string]any{"type": "movie", "movie": contractshape.Movie(&m)}, nil
	case "show":
		var s database.Show
		if err := db.First(&s, note.MediaID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return map[string]any{"type": "show", "show": contractshape.Show(&s)}, nil
	case "season":
		var s database.Season
		if err := db.First(&s, note.MediaID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return map[string]any{"type": "season", "season": contractshape.Season(&s)}, nil
	case "episode":
		var e database.Episode
		if err := db.First(&e, note.MediaID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
		seasonNumber := 0
		var season database.Season
		// Season 0 legitimately has no row, so a not-found is tolerated and
		// renders as 0. Any other database error is real and must surface, not
		// be silently flattened into "this episode has no season".
		if err := db.First(&season, e.SeasonID).Error; err == nil {
			seasonNumber = season.SeasonNumber
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		return map[string]any{"type": "episode", "episode": contractshape.Episode(&e, seasonNumber)}, nil
	}
	// user_notes only stores the four media types above; anything else is an
	// unknown target rather than a fabricated object.
	return nil, nil
}
