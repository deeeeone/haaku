package lists

import (
	"strings"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ============================================================================
// Item reads
// ============================================================================

var contractItemSortColumns = map[string]string{
	"rank":  "rank",
	"added": "added_at",
}

// resolveContractItemSort maps the contract's sort_by to a list_items column.
// Only rank and added exist as columns on list_items; anything else falls back
// to rank. The {type}/{sort_by}/{sort_how} rows are graded divergent for that
// reason (see docs/parity/parity.yaml).
func resolveContractItemSort(sortBy, sortHow string) (string, string) {
	col, ok := contractItemSortColumns[sortBy]
	if !ok {
		col = "rank"
	}
	dir := "ASC"
	if strings.EqualFold(sortHow, "desc") {
		dir = "DESC"
	}
	return col, dir
}

var contractItemTypes = map[string]bool{"movie": true, "show": true, "season": true, "episode": true}

func handleContractListItemsMovie(c *fiber.Ctx) error {
	return serveContractListItems(c, []string{"movie"}, "", "")
}
func handleContractListItemsShow(c *fiber.Ctx) error {
	return serveContractListItems(c, []string{"show"}, "", "")
}
func handleContractListItemsMovieShow(c *fiber.Ctx) error {
	return serveContractListItems(c, []string{"movie", "show"}, "", "")
}
func handleContractListItemsAll(c *fiber.Ctx) error {
	return serveContractListItems(c, []string{"movie", "show", "season", "episode"}, "", "")
}

// handleContractListItemsTyped honours the generic {type}/{sort_by}/{sort_how}
// form: {type} is a comma-separated filter over list_items.media_type.
func handleContractListItemsTyped(c *fiber.Ctx) error {
	types := strings.Split(c.Params("type"), ",")
	clean := make([]string, 0, len(types))
	for _, t := range types {
		t = strings.TrimSpace(t)
		if !contractItemTypes[t] {
			return response.BadRequest(c, "type must be a comma-separated list of movie, show, season, episode")
		}
		clean = append(clean, t)
	}
	return serveContractListItems(c, clean, c.Params("sort_by"), c.Params("sort_how"))
}

func serveContractListItems(c *fiber.Ctx, types []string, sortBy, sortHow string) error {
	db, err := contractDB(c)
	if err != nil {
		return err
	}

	// Two route shapes reach here: /lists/{id}/items/... (the list id is "id")
	// and /users/{id}/lists/{list_id}/items/... (id is the owner, list_id the
	// list).
	var list *database.List
	if ownerParam := c.Params("list_id"); ownerParam != "" {
		owner := auth.ResolveTargetID(c, "id")
		if owner == "" {
			return response.Unauthorized(c)
		}
		list, err = contractLoadList(db, ownerParam)
		if err != nil {
			return listNotFound(c, err)
		}
		if list.UserID != owner {
			return response.NotFound(c, "list not found")
		}
	} else {
		list, err = contractLoadList(db, c.Params("id"))
		if err != nil {
			return listNotFound(c, err)
		}
	}

	if !canViewList(list, userID(c)) {
		return response.Forbidden(c, "this list is private")
	}

	page, limit := response.ParsePagination(c, 25)
	col, dir := resolveContractItemSort(sortBy, sortHow)

	query := db.Where("list_id = ? AND media_type IN ?", list.ID, types)
	var total int64
	if err := query.Model(&database.ListItem{}).Count(&total).Error; err != nil {
		return response.InternalError(c)
	}
	var items []database.ListItem
	if err := query.Order(col + " " + dir).Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return response.InternalError(c)
	}

	out, err := contractListItemObjects(db, items)
	if err != nil {
		return response.InternalError(c)
	}
	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}

// contractListItemObjects renders stored list items in the contract's item
// shape, resolving each referenced media row.
func contractListItemObjects(db *gorm.DB, items []database.ListItem) ([]map[string]any, error) {
	movies := map[int64]database.Movie{}
	shows := map[int64]database.Show{}
	seasons := map[int64]database.Season{}
	episodes := map[int64]database.Episode{}
	seasonNumbers := map[int64]int{}

	for _, it := range items {
		switch it.MediaType {
		case "movie":
			if _, ok := movies[it.MediaID]; !ok {
				var m database.Movie
				if db.First(&m, it.MediaID).Error == nil {
					movies[it.MediaID] = m
				}
			}
		case "show":
			if _, ok := shows[it.MediaID]; !ok {
				var s database.Show
				if db.First(&s, it.MediaID).Error == nil {
					shows[it.MediaID] = s
				}
			}
		case "season":
			if _, ok := seasons[it.MediaID]; !ok {
				var s database.Season
				if db.First(&s, it.MediaID).Error == nil {
					seasons[it.MediaID] = s
				}
			}
		case "episode":
			if _, ok := episodes[it.MediaID]; !ok {
				var e database.Episode
				if db.First(&e, it.MediaID).Error == nil {
					episodes[it.MediaID] = e
					var s database.Season
					if db.First(&s, e.SeasonID).Error == nil {
						seasonNumbers[it.MediaID] = s.SeasonNumber
					}
				}
			}
		}
	}

	out := make([]map[string]any, 0, len(items))
	for i := range items {
		it := &items[i]
		obj := map[string]any{
			"rank":      it.Rank,
			"id":        it.ID,
			"listed_at": contractshape.Timestamp(it.AddedAt),
			"notes":     contractshape.NullableString(it.Notes),
			"type":      it.MediaType,
		}
		switch it.MediaType {
		case "movie":
			if m, ok := movies[it.MediaID]; ok {
				obj["movie"] = contractshape.Movie(&m)
			} else {
				obj["movie"] = nil
			}
		case "show":
			if s, ok := shows[it.MediaID]; ok {
				obj["show"] = contractshape.Show(&s)
			} else {
				obj["show"] = nil
			}
		case "season":
			if s, ok := seasons[it.MediaID]; ok {
				obj["season"] = contractshape.Season(&s)
			} else {
				obj["season"] = nil
			}
		case "episode":
			if e, ok := episodes[it.MediaID]; ok {
				obj["episode"] = contractshape.Episode(&e, seasonNumbers[it.MediaID])
			} else {
				obj["episode"] = nil
			}
		}
		out = append(out, obj)
	}
	return out, nil
}
