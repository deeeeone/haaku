package sync

import (
	"fmt"
	"strconv"

	"github.com/haaku-tv/haaku/internal/handlers/media"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// resolveRefs resolves one client-supplied id set to a local row of wantType.
// It accepts whatever ids the client sent — tvdb is the primary one, tmdb and
// imdb work too — and records which id was used in response headers.
//
// wantType is required because the same numeric id is reused across tables
// (a TVDB or TMDB id can name both a movie and a show). Without it a removal
// or a rating could act on the row of the wrong media type that happens to
// carry the same id.
func resolveRefs(c *fiber.Ctx, db *gorm.DB, ids idsObj, wantType string) (string, int64, error) {
	ref := idRefFor(ids).WithType(wantType)
	switch {
	case ids.TVDB > 0:
		c.Set("id_type", "tvdb")
		c.Set("id_type_param", strconv.Itoa(ids.TVDB))
	case ids.TMDB > 0:
		c.Set("id_type", "tmdb")
		c.Set("id_type_param", strconv.Itoa(ids.TMDB))
	case ids.IMDB != "":
		c.Set("id_type", "imdb")
		c.Set("id_type_param", ids.IMDB)
	}

	resolved, err := media.ResolveIDsBatch(db, []media.IDRef{ref})
	if err != nil {
		return "", 0, err
	}
	found, ok := resolved[ref]
	if !ok || found.MediaType != wantType {
		return "", 0, fmt.Errorf("not found")
	}
	return found.MediaType, found.MediaID, nil
}
