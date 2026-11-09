package users

import (
	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/sync"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
)

// Contract-shaped user reads.
//
// Trakt serves the caller's settings at /users/settings (ours requires an extra
// /me segment) and a user's watchlist with the type and sort as path segments
// (ours takes the type as a segment but the sort as query params). These routes
// serve the contract paths and emit the contract shapes.

func init() {
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/users/settings",
		Summary: "Get the authenticated user's settings (contract path)",
		Tags:    []string{"Users"},
		Auth:    "required",
		StatusCodes: map[int]string{
			200: "Settings", 401: "Unauthorized", 404: "User not found",
		},
	})
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/users/{id}/watchlist/{type}/{sort_by}/{sort_how}",
		Summary: "A user's watchlist for one media type, sorted (contract path)",
		Tags:    []string{"Users"},
		Auth:    "optional",
		StatusCodes: map[int]string{
			200: "Watchlist items", 400: "Invalid type or sort",
			403: "Private profile", 404: "Not found",
		},
	})
}

// RegisterContractRoutes mounts the contract-shaped user watchlist route. Call
// it after users.RegisterProfileReadRoutes so the /users/:id/... statics win.
func RegisterContractRoutes(api fiber.Router) {
	api.Get("/users/:id/watchlist/:type/:sort_by/:sort_how", auth.PrivacyFilter("id"), handleContractUserWatchlist)
}

// handleContractSettings serves GET /v1/users/settings.
func handleContractSettings(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}
	var user database.User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
		return response.WriteError(c, fiber.StatusNotFound, "not_found", "user not found")
	}
	timezone := user.Timezone
	if timezone == "" {
		timezone = "UTC"
	}
	return c.JSON(map[string]any{
		"user": contractshape.SettingsUser(&user),
		"permissions": map[string]any{
			"commenting": true,
			"liking":     true,
			"following":  true,
		},
		"account": map[string]any{
			"timezone":    timezone,
			"date_format": "dmy",
			"time_24hr":   true,
		},
	})
}

// handleContractUserWatchlist serves
// GET /v1/users/{id}/watchlist/{type}/{sort_by}/{sort_how}.
//
// Only movies and shows are storable in user_watchlist; seasons and episodes are
// rejected rather than silently returning an empty page.
func handleContractUserWatchlist(c *fiber.Ctx) error {
	mt, ok := profileMediaType(c.Params("type"))
	if !ok || (mt != "movie" && mt != "show") {
		return response.BadRequest(c, "type must be one of movies, shows")
	}

	order, oerr := sync.ContractMediaOrder(c, "listed_at")
	if oerr != nil {
		return response.BadRequest(c, oerr.Error())
	}

	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}

	page, limit := response.ParsePagination(c, 25)
	query := db.Where("user_id = ? AND media_type = ?", target.ID, mt)
	var total int64
	if err := query.Model(&database.UserWatchlist{}).Count(&total).Error; err != nil {
		return response.InternalError(c)
	}

	var rows []database.UserWatchlist
	if err := query.Order(order).Offset(pageOffset(page, limit)).Limit(limit).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	out := make([]map[string]any, 0, len(rows))
	movies := map[int64]database.Movie{}
	shows := map[int64]database.Show{}
	for _, r := range rows {
		switch r.MediaType {
		case "movie":
			if _, seen := movies[r.MediaID]; !seen {
				var m database.Movie
				if db.First(&m, r.MediaID).Error == nil {
					movies[r.MediaID] = m
				}
			}
		case "show":
			if _, seen := shows[r.MediaID]; !seen {
				var s database.Show
				if db.First(&s, r.MediaID).Error == nil {
					shows[r.MediaID] = s
				}
			}
		}
	}
	for i := range rows {
		r := &rows[i]
		item := map[string]any{
			"rank":      r.Rank,
			"id":        r.ID,
			"listed_at": contractshape.Timestamp(r.ListedAt),
			"type":      r.MediaType,
		}
		if r.Notes != nil {
			item["notes"] = *r.Notes
		}
		if m, ok := movies[r.MediaID]; ok && r.MediaType == "movie" {
			item["movie"] = contractshape.Movie(&m)
		}
		if s, ok := shows[r.MediaID]; ok && r.MediaType == "show" {
			item["show"] = contractshape.Show(&s)
		}
		out = append(out, item)
	}

	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}
