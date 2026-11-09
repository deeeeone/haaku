package calendar

import (
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"

	"github.com/gofiber/fiber/v2"
)

// Contract-shaped release calendars.
//
// Trakt spells the three "hot releases" feeds as path segments:
//
//	/calendars/releases/hot/new/{start_date}/{days}
//	/calendars/releases/hot/premieres/{start_date}/{days}
//	/calendars/releases/hot/finales/{start_date}/{days}
//
// Our episode calendar already computes exactly those row sets behind
// ?type=new|premieres|finale on /v1/calendars/all/shows/{start_date}/{days}.
// These routes share that handler: a Locals middleware pins the subtype and the
// public target, and the handler emits the contract's bare array instead of our
// paginated envelope.

type contractCalendarKind struct {
	segment string // Trakt path segment ("finales", "new", "premieres")
	subtype string // our ?type value ("finale", "new", "premieres")
}

// contractCalendarKinds is the closed set of release feeds we serve. Note the
// name mismatch on finales/finale: Trakt's segment is plural, our filter value
// is singular.
var contractCalendarKinds = []contractCalendarKind{
	{segment: "new", subtype: "new"},
	{segment: "premieres", subtype: "premieres"},
	{segment: "finales", subtype: "finale"},
}

func init() {
	for _, k := range contractCalendarKinds {
		openapi.Register(openapi.RouteDef{
			Method:  "GET",
			Path:    "/v1/calendars/releases/hot/" + k.segment + "/{start_date}/{days}",
			Summary: "Release calendar for the " + k.segment + " feed (contract path)",
			Tags:    []string{"Calendars"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Calendar release entries",
				400: "Invalid start_date or days parameter",
			},
		})
	}
}

// registerContractReleaseRoutes mounts the contract-shaped release calendars.
func registerContractReleaseRoutes(calendars fiber.Router) {
	for _, k := range contractCalendarKinds {
		calendars.Get("/releases/hot/"+k.segment+"/:start_date/:days", pinCalendarContract(k.subtype), handleCalendarShows)
	}
}

// pinCalendarContract selects the shared show-calendar handler's target,
// subtype and contract output shape for one release feed.
func pinCalendarContract(subtype string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals("calendar_target", "all")
		c.Locals("calendar_type", subtype)
		c.Locals("calendar_contract", true)
		return c.Next()
	}
}

// contractReleaseEntries shapes the rows the show calendar already selected into
// the contract's release-item shape: {first_aired, episode, show}.
func contractReleaseEntries(episodes []database.Episode, shows map[int64]database.Show, seasons map[int64]database.Season) []map[string]any {
	out := make([]map[string]any, 0, len(episodes))
	for i := range episodes {
		ep := &episodes[i]
		show, ok := shows[ep.ShowID]
		if !ok {
			continue
		}
		seasonNumber := 0
		if s, ok := seasons[ep.SeasonID]; ok {
			seasonNumber = s.SeasonNumber
		}
		out = append(out, map[string]any{
			"first_aired": contractshape.Date(ep.AiredAt),
			"episode":     contractshape.Episode(ep, seasonNumber),
			"show":        contractshape.Show(&show),
		})
	}
	return out
}
