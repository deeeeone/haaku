// Package lists serves the native and Trakt-contract list surfaces.
//
// Contract-shaped list management: the write surface (create/edit/delete a
// list, add/remove/reorder items, like/unlike, comments, report,
// collaborations) and the item-level reads Trakt clients use to build a list.
// This file holds only the route table; the handlers live in the sibling
// contract_lists_*.go files, grouped by concern.
//
// These routes live at the contract's paths under /v1
// (/v1/users/{id}/lists/{list_id}/..., /v1/lists/{id}/items/...) and emit the
// contract's envelopes, where our native surface
// (/v1/users/me/lists, /v1/lists/{id}/items) predates it. Every response is
// built from stored rows: item_count is a real COUNT, likes/comments are real
// rows, and the {type} path segment is a real filter over list_items.media_type.
//
// Registration order matters. These routes are registered LAST (see
// routes.go) so the static /users/me/lists and /users/me/lists/collaborations
// routes registered earlier win, and nothing registered here can shadow a
// later static route. In the table below the static `collaborations` and
// `reorder` segments are registered before the {list_id} parameter.
package lists

import (
	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/openapi"

	"github.com/gofiber/fiber/v2"
)

func init() {
	registerContractListRoutes()
}

func registerContractListRoutes() {
	defs := []openapi.RouteDef{
		// CRUD
		{Method: "POST", Path: "/v1/users/{id}/lists", Summary: "Create a list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{201: "Created list", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden"}},
		{Method: "GET", Path: "/v1/users/{id}/lists/collaborations", Summary: "Lists the user can collaborate on (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Collaborated lists", 401: "Unauthorized", 403: "Forbidden"}},
		{Method: "POST", Path: "/v1/users/{id}/lists/reorder", Summary: "Reorder a user's lists (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Reordered", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden"}},
		{Method: "GET", Path: "/v1/users/{id}/lists/{list_id}/", Summary: "Get a list summary (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "List", 403: "Private", 404: "Not found"}},
		{Method: "PUT", Path: "/v1/users/{id}/lists/{list_id}/", Summary: "Update a list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Updated list", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden", 404: "Not found"}},
		{Method: "DELETE", Path: "/v1/users/{id}/lists/{list_id}/", Summary: "Delete a list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{204: "Deleted", 401: "Unauthorized", 403: "Forbidden", 404: "Not found"}},

		// Items
		{Method: "POST", Path: "/v1/users/{id}/lists/{list_id}/items", Summary: "Add items to a list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{201: "Items added", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden", 404: "Not found"}},
		{Method: "POST", Path: "/v1/users/{id}/lists/{list_id}/items/remove", Summary: "Remove items from a list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Items removed", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden", 404: "Not found"}},
		{Method: "POST", Path: "/v1/users/{id}/lists/{list_id}/items/reorder", Summary: "Reorder items in a list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Reordered", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden", 404: "Not found"}},
		{Method: "POST", Path: "/v1/users/{id}/lists/{list_id}/reorder", Summary: "Reorder a list's items (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Reordered", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden", 404: "Not found"}},
		{Method: "PUT", Path: "/v1/users/{id}/lists/{list_id}/items/{list_item_id}", Summary: "Update a list item (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{204: "Updated", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden", 404: "Not found"}},

		{Method: "GET", Path: "/v1/users/{id}/lists/{list_id}/items/movie", Summary: "A list's movies (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Items", 403: "Private", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/lists/{list_id}/items/show", Summary: "A list's shows (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Items", 403: "Private", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/lists/{list_id}/items/movie,show", Summary: "A list's movies and shows (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Items", 403: "Private", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/lists/{list_id}/items/movie,show,season,episode", Summary: "All of a list's items (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Items", 403: "Private", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/lists/{list_id}/items/{type}/{sort_by}/{sort_how}", Summary: "A list's items by type and sort (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Items", 400: "Invalid type or sort", 403: "Private", 404: "Not found"}},

		{Method: "GET", Path: "/v1/lists/{id}/items/movie", Summary: "A list's movies (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Items", 403: "Private", 404: "Not found"}},
		{Method: "GET", Path: "/v1/lists/{id}/items/show", Summary: "A list's shows (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Items", 403: "Private", 404: "Not found"}},
		{Method: "GET", Path: "/v1/lists/{id}/items/movie,show", Summary: "A list's movies and shows (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Items", 403: "Private", 404: "Not found"}},
		{Method: "GET", Path: "/v1/lists/{id}/items/movie,show,episode,season", Summary: "All of a list's items (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Items", 403: "Private", 404: "Not found"}},
		{Method: "GET", Path: "/v1/lists/{id}/items/{type}/{sort_by}/{sort_how}", Summary: "A list's items by type and sort (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Items", 400: "Invalid type or sort", 403: "Private", 404: "Not found"}},

		// Social
		{Method: "GET", Path: "/v1/users/{id}/lists/{list_id}/likes", Summary: "Who liked a list (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Likes", 403: "Private", 404: "Not found"}},
		{Method: "POST", Path: "/v1/users/{id}/lists/{list_id}/like", Summary: "Like a list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{204: "Liked", 401: "Unauthorized", 403: "Private", 404: "Not found"}},
		{Method: "DELETE", Path: "/v1/users/{id}/lists/{list_id}/like", Summary: "Unlike a list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{204: "Unliked", 401: "Unauthorized", 403: "Private", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/lists/{list_id}/comments/{sort}", Summary: "A list's comments (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Comments", 403: "Private", 404: "Not found"}},
		{Method: "GET", Path: "/v1/lists/{id}/comments/{sort}", Summary: "A list's comments (contract path)", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Comments", 403: "Private", 404: "Not found"}},
		{Method: "POST", Path: "/v1/users/{id}/lists/{list_id}/report", Summary: "Report a list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 400: "Invalid request", 401: "Unauthorized", 404: "Not found", 409: "Already reported"}},
		{Method: "POST", Path: "/v1/lists/{id}/report", Summary: "Report a list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 400: "Invalid request", 401: "Unauthorized", 404: "Not found", 409: "Already reported"}},

		// Smart lists
		{Method: "GET", Path: "/v1/users/{id}/smart-lists", Summary: "A user's smart lists (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Smart lists", 401: "Unauthorized", 403: "Forbidden"}},
		{Method: "POST", Path: "/v1/users/{id}/smart-lists", Summary: "Create a smart list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{201: "Created", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden"}},
		{Method: "GET", Path: "/v1/users/{id}/smart-lists/{list_id}/", Summary: "Get a smart list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Smart list", 401: "Unauthorized", 403: "Forbidden", 404: "Not found"}},
		{Method: "PUT", Path: "/v1/users/{id}/smart-lists/{list_id}/", Summary: "Update a smart list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Updated", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden", 404: "Not found"}},
		{Method: "DELETE", Path: "/v1/users/{id}/smart-lists/{list_id}/", Summary: "Delete a smart list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{204: "Deleted", 401: "Unauthorized", 403: "Forbidden", 404: "Not found"}},
		{Method: "GET", Path: "/v1/smart-lists/{list_id}/items/{type}/{sort_by}/{sort_how}", Summary: "Resolve a smart list's items by type and sort (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Items", 400: "Invalid type or sort", 401: "Unauthorized", 403: "Forbidden", 404: "Not found"}},

		// Smart list draft
		{Method: "GET", Path: "/v1/users/{id}/lists/{list_id}/smart-list-draft", Summary: "A smart-list draft derived from a list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Draft", 401: "Unauthorized", 403: "Forbidden", 404: "Not found"}},
		{Method: "POST", Path: "/v1/users/{id}/lists/{list_id}/smart-list-draft/convert", Summary: "Convert a draft into a smart list (contract path)", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{201: "Created", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden", 404: "Not found"}},
	}
	for _, d := range defs {
		openapi.Register(d)
	}
}

// RegisterManagementContractRoutes mounts the contract-shaped list-management
// routes. Call it LAST in httpapi.RegisterRoutes: the static /users/me/lists
// and /users/me/lists/collaborations routes registered earlier must win, and
// nothing here may shadow a later static route.
func RegisterManagementContractRoutes(api fiber.Router) {
	// User-scoped list CRUD. Static segments (collaborations, reorder) before
	// the {list_id} parameter at the same depth.
	api.Post("/users/:id/lists", auth.AuthRequired(), handleContractCreateList)
	api.Get("/users/:id/lists/collaborations", auth.AuthRequired(), handleContractCollaborations)
	api.Post("/users/:id/lists/reorder", auth.AuthRequired(), handleContractReorderUserLists)

	api.Get("/users/:id/lists/:list_id/", handleContractGetList)
	api.Put("/users/:id/lists/:list_id/", auth.AuthRequired(), handleContractUpdateList)
	api.Delete("/users/:id/lists/:list_id/", auth.AuthRequired(), handleContractDeleteList)

	// Items. Static sub-segments (remove, reorder) before {list_item_id}.
	api.Post("/users/:id/lists/:list_id/items", auth.AuthRequired(), handleContractAddItems)
	api.Post("/users/:id/lists/:list_id/items/remove", auth.AuthRequired(), handleContractRemoveItems)
	api.Post("/users/:id/lists/:list_id/items/reorder", auth.AuthRequired(), handleContractReorderItems)
	api.Post("/users/:id/lists/:list_id/reorder", auth.AuthRequired(), handleContractReorderItems)
	api.Put("/users/:id/lists/:list_id/items/:list_item_id", auth.AuthRequired(), handleContractUpdateItem)

	// Item reads. The static CSV forms are registered before the generic
	// {type}/{sort_by}/{sort_how} route; they are at different depths, so both
	// are reachable.
	api.Get("/users/:id/lists/:list_id/items/movie", handleContractListItemsMovie)
	api.Get("/users/:id/lists/:list_id/items/show", handleContractListItemsShow)
	api.Get("/users/:id/lists/:list_id/items/movie,show", handleContractListItemsMovieShow)
	api.Get("/users/:id/lists/:list_id/items/movie,show,season,episode", handleContractListItemsAll)
	api.Get("/users/:id/lists/:list_id/items/:type/:sort_by/:sort_how", handleContractListItemsTyped)

	api.Get("/lists/:id/items/movie", handleContractListItemsMovie)
	api.Get("/lists/:id/items/show", handleContractListItemsShow)
	api.Get("/lists/:id/items/movie,show", handleContractListItemsMovieShow)
	api.Get("/lists/:id/items/movie,show,episode,season", handleContractListItemsAll)
	api.Get("/lists/:id/items/:type/:sort_by/:sort_how", handleContractListItemsTyped)

	// Social.
	api.Get("/users/:id/lists/:list_id/likes", handleContractListLikes)
	api.Post("/users/:id/lists/:list_id/like", auth.AuthRequired(), handleContractLikeList)
	api.Delete("/users/:id/lists/:list_id/like", auth.AuthRequired(), handleContractUnlikeList)
	api.Get("/users/:id/lists/:list_id/comments/:sort", handleContractListComments)
	api.Get("/lists/:id/comments/:sort", handleContractListComments)
	api.Post("/users/:id/lists/:list_id/report", auth.AuthRequired(), handleContractReportList)
	api.Post("/lists/:id/report", auth.AuthRequired(), handleContractReportList)

	// Smart lists.
	api.Get("/users/:id/smart-lists", auth.AuthRequired(), handleContractGetSmartLists)
	api.Post("/users/:id/smart-lists", auth.AuthRequired(), handleContractCreateSmartList)
	api.Get("/users/:id/smart-lists/:list_id/", auth.AuthRequired(), handleContractGetSmartList)
	api.Put("/users/:id/smart-lists/:list_id/", auth.AuthRequired(), handleContractUpdateSmartList)
	api.Delete("/users/:id/smart-lists/:list_id/", auth.AuthRequired(), handleContractDeleteSmartList)
	api.Get("/smart-lists/:list_id/items/:type/:sort_by/:sort_how", auth.AuthRequired(), handleContractSmartListItems)

	// Smart list draft.
	api.Get("/users/:id/lists/:list_id/smart-list-draft", auth.AuthRequired(), handleContractSmartListDraft)
	api.Post("/users/:id/lists/:list_id/smart-list-draft/convert", auth.AuthRequired(), handleContractConvertDraft)
}
