package media

// "Lists containing this item" reads (issue #67).
//
// Trakt exposes the same read under a movie, a show, a person, and a show's
// season. Every variant resolves the item, keeps only public lists whose
// list_items row references it, ranks the lists by the requested sort, and
// returns contract list objects. official/watchlist/favorites are Trakt list
// kinds haaku does not model as lists, so they answer a computed empty array.

import (
	"errors"
	"strconv"
	"strings"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func init() {
	routes := []openapi.RouteDef{
		{
			Method:  "GET",
			Path:    "/v1/movies/{id}/lists/{type}/{sort}",
			Summary: "Get lists containing a movie by kind and sort",
			Tags:    []string{"Movies"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Lists containing the movie",
				400: "Invalid type, sort or movie id",
				404: "Movie not found",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/shows/{id}/lists/{type}/{sort}",
			Summary: "Get lists containing a show by kind and sort",
			Tags:    []string{"Shows"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Lists containing the show",
				400: "Invalid type, sort or show id",
				404: "Show not found",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/people/{id}/lists/{type}/{sort}",
			Summary: "Get lists containing a person by kind and sort",
			Tags:    []string{"People"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Lists containing the person",
				400: "Invalid type, sort or person id",
				404: "Person not found",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/shows/{id}/seasons/{season}/lists/{type}/{sort}",
			Summary: "Get lists containing a season by kind and sort",
			Tags:    []string{"Shows"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Lists containing the season",
				400: "Invalid type, sort, show id or season number",
				404: "Show or season not found",
			},
		},
	}
	for _, route := range routes {
		openapi.Register(route)
	}
}

// itemListTypes are the Trakt list kinds this read accepts. Only personal/all
// (and the plain route's default) map to rows in our store.
var itemListTypes = map[string]bool{
	"all": true, "personal": true, "official": true, "watchlist": true, "favorites": true,
}

// itemListEmptyTypes are real Trakt list kinds we do not model as lists. They
// answer 200 + [] rather than a 404 or a fabricated list.
var itemListEmptyTypes = map[string]bool{
	"official": true, "watchlist": true, "favorites": true,
}

// itemListSorts are the contract's six orderings.
var itemListSorts = map[string]bool{
	"popular": true, "likes": true, "comments": true, "items": true, "added": true, "updated": true,
}

// handleGetMovieLists serves the plain route with Trakt's defaults
// (type=personal, sort=popular).
func handleGetMovieLists(c *fiber.Ctx) error {
	return serveItemLists(c, "movie", "personal", "popular")
}

func handleGetMovieListsByTypeSort(c *fiber.Ctx) error {
	return serveItemLists(c, "movie", c.Params("type"), c.Params("sort"))
}

func handleGetShowLists(c *fiber.Ctx) error {
	return serveItemLists(c, "show", "personal", "popular")
}

func handleGetShowListsByTypeSort(c *fiber.Ctx) error {
	return serveItemLists(c, "show", c.Params("type"), c.Params("sort"))
}

func handleGetPersonListsByTypeSort(c *fiber.Ctx) error {
	return serveItemLists(c, "person", c.Params("type"), c.Params("sort"))
}

// handleGetShowSeasonLists resolves the show+season pair to our season row,
// then serves lists whose list_items reference that season.
func handleGetShowSeasonLists(c *fiber.Ctx) error {
	showID, err := response.ParseIDParam(c, "show")
	if err != nil {
		return err
	}

	seasonNumber, err := strconv.Atoi(c.Params("season"))
	if err != nil {
		return response.BadRequest(c, "invalid season number")
	}

	listType := strings.ToLower(c.Params("type"))
	sort := strings.ToLower(c.Params("sort"))
	if err := validateItemListKind(c, listType, sort); err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	var show database.Show
	if err := db.First(&show, showID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "show not found")
		}
		return response.InternalError(c)
	}

	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", showID, seasonNumber).
		First(&season).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "season not found")
		}
		return response.InternalError(c)
	}

	return writeItemLists(c, db, "season", season.ID, listType, sort)
}

// serveItemLists is the shared movie/show/person path.
func serveItemLists(c *fiber.Ctx, mediaType, listType, sort string) error {
	id, err := response.ParseIDParam(c, mediaType)
	if err != nil {
		return err
	}

	listType = strings.ToLower(listType)
	sort = strings.ToLower(sort)
	if err := validateItemListKind(c, listType, sort); err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	exists, err := itemListTargetExists(db, mediaType, id)
	if err != nil {
		return response.InternalError(c)
	}
	if !exists {
		return response.NotFound(c, mediaType+" not found")
	}

	return writeItemLists(c, db, mediaType, id, listType, sort)
}

func validateItemListKind(c *fiber.Ctx, listType, sort string) error {
	if !itemListTypes[listType] {
		return response.BadRequest(c, "type must be one of: all, personal, official, watchlist, favorites")
	}
	if !itemListSorts[sort] {
		return response.BadRequest(c, "sort must be one of: popular, likes, comments, items, added, updated")
	}
	return nil
}

func itemListTargetExists(db *gorm.DB, mediaType string, id int64) (bool, error) {
	var err error
	switch mediaType {
	case "movie":
		err = db.First(&database.Movie{}, id).Error
	case "show":
		err = db.First(&database.Show{}, id).Error
	case "person":
		err = db.First(&database.Person{}, id).Error
	default:
		return false, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// writeItemLists runs the public, item-scoped list query and renders one page.
func writeItemLists(c *fiber.Ctx, db *gorm.DB, mediaType string, itemID int64, listType, sort string) error {
	page, limit := response.ParsePagination(c, 25)

	if itemListEmptyTypes[listType] {
		response.SetPaginationHeaders(c, page, limit, 0, 0)
		return c.JSON([]map[string]any{})
	}

	base := func() *gorm.DB {
		return db.Model(&database.List{}).
			Where("privacy = ?", "public").
			Where("EXISTS (SELECT 1 FROM list_items li WHERE li.list_id = lists.id AND li.media_type = ? AND li.media_id = ?)", mediaType, itemID)
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return response.InternalError(c)
	}

	query := base()
	switch sort {
	case "likes":
		query = query.Order("lists.like_count DESC, lists.id ASC")
	case "comments":
		query = query.Order("lists.comment_count DESC, lists.id ASC")
	case "items":
		// Rank by the real number of rows in the list, in one grouped subquery.
		query = query.
			Joins("LEFT JOIN (SELECT list_id, COUNT(*) AS item_count FROM list_items GROUP BY list_id) list_counts ON list_counts.list_id = lists.id").
			Order("COALESCE(list_counts.item_count, 0) DESC, lists.id ASC")
	case "added":
		query = query.Order("lists.created_at DESC, lists.id ASC")
	case "updated":
		query = query.Order("lists.updated_at DESC, lists.id ASC")
	default: // popular
		query = query.Order("(lists.like_count + lists.comment_count) DESC, lists.updated_at DESC, lists.id ASC")
	}

	var lists []database.List
	if err := query.Offset((page - 1) * limit).Limit(limit).Find(&lists).Error; err != nil {
		return response.InternalError(c)
	}

	listIDs := make([]int64, 0, len(lists))
	ownerIDs := make([]string, 0, len(lists))
	for i := range lists {
		listIDs = append(listIDs, lists[i].ID)
		ownerIDs = append(ownerIDs, lists[i].UserID)
	}

	itemCounts := map[int64]int64{}
	if len(listIDs) > 0 {
		var counts []struct {
			ListID int64
			N      int64
		}
		if err := db.Model(&database.ListItem{}).
			Select("list_id, COUNT(*) AS n").
			Where("list_id IN ?", listIDs).
			Group("list_id").
			Scan(&counts).Error; err != nil {
			return response.InternalError(c)
		}
		for _, count := range counts {
			itemCounts[count.ListID] = count.N
		}
	}

	users, err := contractshape.UserByIDBatch(db, ownerIDs)
	if err != nil {
		return response.InternalError(c)
	}

	out := make([]map[string]any, 0, len(lists))
	for i := range lists {
		list := &lists[i]
		out = append(out, contractshape.ListWithItemCount(list, users[list.UserID], itemCounts[list.ID]))
	}

	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}
