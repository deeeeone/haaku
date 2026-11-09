package lists

import (
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped popular/trending lists.
//
// Trakt's /lists/popular/{type} and /lists/trending/{type} carry the list kind
// as a path segment; our /v1/lists/popular and /v1/lists/trending rank every
// public list. These routes filter the same ranking by the media type of the
// list's items and emit the contract's {like_count, comment_count, list} shape
// as a bare array.

func init() {
	for _, seg := range []string{"popular", "trending"} {
		openapi.Register(openapi.RouteDef{
			Method:  "GET",
			Path:    "/v1/lists/" + seg + "/{type}",
			Summary: "Get " + seg + " lists of one kind (contract path)",
			Tags:    []string{"Lists"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: seg + " lists",
				400: "Invalid list type",
			},
		})
	}
}

// listTypeMedia maps Trakt's list-kind segment onto the media_type our list
// items carry.
var listTypeMedia = map[string]string{
	"movies": "movie",
	"shows":  "show",
	"people": "person",
}

// RegisterContractRoutes mounts the contract-shaped public list feeds. Called
// from routes.go after lists.RegisterRoutes so the static /lists/popular and
// /lists/trending routes are already registered.
func RegisterContractRoutes(api fiber.Router) {
	api.Get("/lists/popular/:type", handleContractPopularLists)
	api.Get("/lists/trending/:type", handleContractTrendingLists)
}

func handleContractPopularLists(c *fiber.Ctx) error  { return contractListFeed(c, "popular") }
func handleContractTrendingLists(c *fiber.Ctx) error { return contractListFeed(c, "trending") }

// contractListFeed serves one public list feed filtered by kind.
func contractListFeed(c *fiber.Ctx, kind string) error {
	mediaType, ok := listTypeMedia[c.Params("type")]
	if !ok {
		return response.BadRequest(c, "invalid list type: must be one of movies, shows, people")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 25)

	build := func() *gorm.DB {
		return db.Model(&database.List{}).
			Where("privacy = ?", "public").
			Where("EXISTS (SELECT 1 FROM list_items li WHERE li.list_id = lists.id AND li.media_type = ?)", mediaType)
	}

	var total int64
	build().Count(&total)

	order := "like_count DESC, created_at DESC"
	if kind == "trending" {
		// Momentum without a Postgres-only epoch expression: likes first, then
		// the newest lists among equal like counts.
		order = "like_count DESC, created_at DESC"
	}

	var lists []database.List
	if err := build().Order(order).Offset((page - 1) * limit).Limit(limit).Find(&lists).Error; err != nil {
		return response.InternalError(c)
	}

	out, err := contractListItems(db, lists)
	if err != nil {
		return response.InternalError(c)
	}
	return c.JSON(out)
}

// contractListItems shapes list rows as contract list objects, resolving each
// owner and each item count in one batched query. Errors are returned rather than
// swallowed: an owner that could not be read renders as User(nil), i.e.
// "deleted": true, and a 2xx that marks every list owner deleted is worse than a
// 500.
func contractListItems(db *gorm.DB, lists []database.List) ([]map[string]any, error) {
	userIDs := make([]string, 0, len(lists))
	seen := map[string]bool{}
	for _, l := range lists {
		if !seen[l.UserID] {
			seen[l.UserID] = true
			userIDs = append(userIDs, l.UserID)
		}
	}
	users := map[string]database.User{}
	if len(userIDs) > 0 {
		var rows []database.User
		if err := db.Where("id IN ?", userIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for i := range rows {
			users[rows[i].ID] = rows[i]
		}
	}

	// item_count is the real number of rows in the list, in one grouped query
	// rather than a per-list count or a hard-coded zero.
	listIDs := make([]int64, 0, len(lists))
	for i := range lists {
		listIDs = append(listIDs, lists[i].ID)
	}
	itemCounts := map[int64]int64{}
	if len(listIDs) > 0 {
		var counts []struct {
			ListID int64
			N      int64
		}
		if err := db.Model(&database.ListItem{}).
			Select("list_id, count(*) as n").
			Where("list_id IN ?", listIDs).
			Group("list_id").
			Scan(&counts).Error; err != nil {
			return nil, err
		}
		for _, c := range counts {
			itemCounts[c.ListID] = c.N
		}
	}

	out := make([]map[string]any, 0, len(lists))
	for i := range lists {
		l := &lists[i]
		var user *database.User
		if u, ok := users[l.UserID]; ok {
			user = &u
		}
		out = append(out, map[string]any{
			"like_count":    l.LikeCount,
			"comment_count": l.CommentCount,
			"list":          contractList(l, user, itemCounts[l.ID]),
		})
	}
	return out, nil
}

// contractList renders one list in the contract's shape.
func contractList(l *database.List, user *database.User, itemCount int64) map[string]any {
	images := map[string]any{}
	if l.CoverURL != "" {
		images["poster"] = []string{l.CoverURL}
	}
	return map[string]any{
		"name":            l.Name,
		"description":     contractshape.NullableString(l.Description),
		"privacy":         l.Privacy,
		"share_link":      "",
		"type":            "personal",
		"display_numbers": l.IsRanked,
		"allow_comments":  l.AllowComments,
		"sort_by":         l.SortBy,
		"sort_how":        l.SortHow,
		"created_at":      contractshape.Timestamp(l.CreatedAt),
		"updated_at":      contractshape.Timestamp(l.UpdatedAt),
		"item_count":      itemCount,
		"comment_count":   l.CommentCount,
		"likes":           l.LikeCount,
		"ids":             contractshape.ListIDs(l),
		"user":            contractshape.User(user),
		"images":          images,
	}
}
