package sync

import "github.com/haaku-tv/haaku/internal/database"

// Exported views of the read-response shapes and their builders. The
// public-profile read surface (/v1/users/{id}/history, /collection, /watchlist,
// /ratings) returns exactly what the caller's own /v1/sync/... routes return,
// so both surfaces share one definition instead of drifting apart. Aliases
// keep the internal names (and existing tests) untouched.

// HistoryResponse is a single watch-history row.
type HistoryResponse = historyResponse

// CollectionResponse is a single collection row.
type CollectionResponse = collectionResponse

// WatchlistResponse is a single watchlist row.
type WatchlistResponse = watchlistResponse

// RatingResponse is a single rating row.
type RatingResponse = ratingResponse

// BuildHistoryResponse renders a history row.
func BuildHistoryResponse(h *database.UserHistory) HistoryResponse {
	return buildHistoryResponse(h)
}

// BuildCollectionResponses renders collection rows.
func BuildCollectionResponses(items []database.UserCollection) []CollectionResponse {
	return buildCollectionResponses(items)
}

// BuildWatchlistResponses renders watchlist rows.
func BuildWatchlistResponses(items []database.UserWatchlist) []WatchlistResponse {
	return buildWatchlistResponses(items)
}

// BuildRatingResponses renders rating rows.
func BuildRatingResponses(items []database.UserRating) []RatingResponse {
	return buildRatingResponses(items)
}
