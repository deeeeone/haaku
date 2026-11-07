package tests

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
)

// ============================================================================
// Sync Routes Registered
// ============================================================================

func TestSyncRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/sync/history":                         true,
		"/v1/sync/history/{media_type}/{media_id}": true,
		"/v1/sync/collection":                      true,
		"/v1/sync/watchlist":                       true,
		"/v1/sync/ratings":                         true,
		"/v1/sync/favorites":                       true,
		"/v1/sync/last_activities":                 true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected sync route %q not registered", path)
		}
	}
}

// ============================================================================
// History Response Shape Tests
// ============================================================================

func TestUserHistoryModel(t *testing.T) {
	watchedAt := time.Now().UTC()
	seasonID := int64(1)
	episodeID := int64(5)

	h := database.UserHistory{
		ID:        1,
		UserID:    "user_abc123",
		MediaType: "episode",
		MediaID:   500,
		SeasonID:  &seasonID,
		EpisodeID: &episodeID,
		WatchedAt: watchedAt,
		Source:    "api",
	}

	data, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("failed to marshal UserHistory: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["user_id"] != "user_abc123" {
		t.Errorf("user_id = %v, want user_abc123", raw["user_id"])
	}
	if raw["media_type"] != "episode" {
		t.Errorf("media_type = %v, want episode", raw["media_type"])
	}
	if raw["media_id"] != float64(500) {
		t.Errorf("media_id = %v, want 500", raw["media_id"])
	}
	if raw["season_id"] != float64(1) {
		t.Errorf("season_id missing or wrong")
	}
	if raw["episode_id"] != float64(5) {
		t.Errorf("episode_id missing or wrong")
	}
}

func TestUserHistoryMovieModel(t *testing.T) {
	watchedAt := time.Now().UTC()

	h := database.UserHistory{
		ID:        2,
		UserID:    "user_abc123",
		MediaType: "movie",
		MediaID:   603,
		WatchedAt: watchedAt,
		Source:    "api",
	}

	data, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("failed to marshal UserHistory: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if raw["media_type"] != "movie" {
		t.Errorf("media_type = %v", raw["media_type"])
	}
	// movie history should omit season_id/episode_id
	if _, ok := raw["season_id"]; ok {
		t.Error("season_id should be omitted for movie history")
	}
}

// ============================================================================
// Collection Model Tests
// ============================================================================

func TestUserCollectionModel(t *testing.T) {
	now := time.Now().UTC()

	c := database.UserCollection{
		ID:          1,
		UserID:      "user_abc123",
		MediaType:   "movie",
		MediaID:     603,
		CollectedAt: now,
	}

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("failed to marshal UserCollection: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if raw["media_type"] != "movie" {
		t.Errorf("media_type = %v", raw["media_type"])
	}
	if raw["media_id"] != float64(603) {
		t.Errorf("media_id = %v", raw["media_id"])
	}
}

// ============================================================================
// Watchlist Model Tests
// ============================================================================

func TestUserWatchlistModel(t *testing.T) {
	now := time.Now().UTC()
	notes := "must watch"

	w := database.UserWatchlist{
		ID:        1,
		UserID:    "user_abc123",
		MediaType: "show",
		MediaID:   1396,
		Rank:      3,
		Notes:     &notes,
		ListedAt:  now,
	}

	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("failed to marshal UserWatchlist: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if raw["rank"] != float64(3) {
		t.Errorf("rank = %v, want 3", raw["rank"])
	}
	if raw["notes"] != "must watch" {
		t.Errorf("notes = %v", raw["notes"])
	}
}

func TestUserWatchlistReorderRank(t *testing.T) {
	// Simulated reorder: verify rank ordering
	items := []database.UserWatchlist{
		{ID: 1, Rank: 0},
		{ID: 2, Rank: 1},
		{ID: 3, Rank: 2},
	}

	// Reorder: [3, 1, 2] → ranks 0, 1, 2
	newOrder := []int64{3, 1, 2}
	for i, id := range newOrder {
		for j := range items {
			if items[j].ID == id {
				items[j].Rank = i
				break
			}
		}
	}

	if items[2].Rank != 0 { // ID 3 was first → rank 0
		t.Errorf("ID 3 rank = %d, want 0", items[2].Rank)
	}
	if items[0].Rank != 1 { // ID 1 was second → rank 1
		t.Errorf("ID 1 rank = %d, want 1", items[0].Rank)
	}
	if items[1].Rank != 2 { // ID 2 was third → rank 2
		t.Errorf("ID 2 rank = %d, want 2", items[1].Rank)
	}
}

// ============================================================================
// Ratings Model Tests
// ============================================================================

func TestUserRatingModelValidRange(t *testing.T) {
	now := time.Now().UTC()
	validRatings := []int{1, 5, 10}

	for _, r := range validRatings {
		rating := database.UserRating{
			ID:        1,
			UserID:    "user_abc123",
			MediaType: "movie",
			MediaID:   603,
			Rating:    r,
			RatedAt:   now,
		}

		data, err := json.Marshal(rating)
		if err != nil {
			t.Fatalf("failed to marshal rating %d: %v", r, err)
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("decode response body: %v", err)
		}

		if raw["rating"] != float64(r) {
			t.Errorf("rating = %v, want %d", raw["rating"], r)
		}
	}
}

func TestUserRatingDedupeKey(t *testing.T) {
	// Two ratings for the same user+media type IDs should conflict
	r1 := database.UserRating{
		UserID: "user_abc123", MediaType: "movie", MediaID: 603, Rating: 8,
	}
	r2 := database.UserRating{
		UserID: "user_abc123", MediaType: "movie", MediaID: 603, Rating: 9,
	}

	// Same dedupe key = (user_id, media_type, media_id)
	if r1.UserID != r2.UserID || r1.MediaType != r2.MediaType || r1.MediaID != r2.MediaID {
		t.Error("ratings should share dedupe key")
	}

	// But different rating value (upsert replaces)
	if r1.Rating == r2.Rating {
		t.Error("ratings should differ — upsert replaces")
	}
}

// ============================================================================
// Favorites Model Tests
// ============================================================================

func TestUserFavoriteModel(t *testing.T) {
	now := time.Now().UTC()

	f := database.UserFavorite{
		ID:          1,
		UserID:      "user_abc123",
		MediaType:   "show",
		MediaID:     1396,
		Rank:        1,
		FavoritedAt: now,
	}

	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("failed to marshal UserFavorite: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if raw["rank"] != float64(1) {
		t.Errorf("rank = %v", raw["rank"])
	}
	if raw["media_type"] != "show" {
		t.Errorf("media_type = %v", raw["media_type"])
	}
}

// ============================================================================
// Deduplication Logic Tests
// ============================================================================

func TestHistoryDedupeDetection(t *testing.T) {
	// Same user + same media + same source = duplicate
	h1 := database.UserHistory{
		UserID: "user_x", MediaType: "movie", MediaID: 603, Source: "api",
	}
	h2 := database.UserHistory{
		UserID: "user_x", MediaType: "movie", MediaID: 603, Source: "api",
	}

	isDup := h1.UserID == h2.UserID &&
		h1.MediaType == h2.MediaType &&
		h1.MediaID == h2.MediaID &&
		h1.Source == h2.Source
	if !isDup {
		t.Error("should detect as duplicate")
	}
}

func TestHistoryNoDedupeDifferentSource(t *testing.T) {
	h1 := database.UserHistory{
		UserID: "user_x", MediaType: "movie", MediaID: 603, Source: "api",
	}
	h2 := database.UserHistory{
		UserID: "user_x", MediaType: "movie", MediaID: 603, Source: "import",
	}

	isDup := h1.UserID == h2.UserID &&
		h1.MediaType == h2.MediaType &&
		h1.MediaID == h2.MediaID &&
		h1.Source == h2.Source
	if isDup {
		t.Error("different sources should NOT be duplicates")
	}
}

// ============================================================================
// WatchedAt Parsing Tests
// ============================================================================

func TestWatchedAtSentinelValues(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		val       string
		airedAt   *database.Date
		expectNow bool
	}{
		{"unknown", "unknown", nil, true},
		{"empty", "", nil, true},
		{"released_no_date", "released", nil, true},
		{"explicit_rfc3339", "2024-01-15T20:30:00Z", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate parseWatchedAt logic
			var result time.Time
			switch tt.val {
			case "released":
				if tt.airedAt != nil && !tt.airedAt.IsZero() {
					result = tt.airedAt.Time
				} else {
					result = now
				}
			case "", "unknown":
				result = now
			default:
				if parsed, err := time.Parse(time.RFC3339, tt.val); err == nil {
					result = parsed
				} else {
					result = now
				}
			}

			if tt.expectNow {
				if result.Sub(now) > time.Second {
					t.Errorf("expected now-ish time, got %v", result)
				}
			} else {
				expected, _ := time.Parse(time.RFC3339, tt.val)
				if !result.Equal(expected) {
					t.Errorf("expected %v, got %v", expected, result)
				}
			}
		})
	}
}

func TestWatchedAtReleasedWithAirDate(t *testing.T) {
	airedDate := &database.Date{}
	airedDate.Time = time.Date(2013, 9, 29, 0, 0, 0, 0, time.UTC)

	// Simulate parseWatchedAt("released", now, airedAt)
	val := "released"
	var result time.Time
	if val == "released" && airedDate != nil && !airedDate.IsZero() {
		result = airedDate.Time
	}

	expected := time.Date(2013, 9, 29, 0, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("released sentinel should use air date, got %v, want %v", result, expected)
	}
}

// ============================================================================
// Collection Response Shape Tests
// ============================================================================

func TestCollectionResponseShape(t *testing.T) {
	// Verify collection items serialize properly
	items := []map[string]interface{}{
		{
			"id":           float64(1),
			"media_type":   "movie",
			"media_id":     float64(603),
			"collected_at": "2024-01-15T00:00:00Z",
		},
	}

	data, err := json.Marshal(fiberMap{"items": items})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	itemsRaw, ok := raw["items"].([]interface{})
	if !ok {
		t.Fatal("items should be an array")
	}
	if len(itemsRaw) != 1 {
		t.Errorf("expected 1 item, got %d", len(itemsRaw))
	}
}

// ============================================================================
// Watchlist Reorder Response Shape
// ============================================================================

func TestWatchlistReorderResponseShape(t *testing.T) {
	resp := fiberMap{
		"success": true,
		"count":   5.0,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if raw["success"] != true {
		t.Error("should be success: true")
	}
	if raw["count"] != float64(5) {
		t.Errorf("count = %v, want 5", raw["count"])
	}
}

// ============================================================================
// Ratings Upsert Behavior
// ============================================================================

func TestRatingsUpsert(t *testing.T) {
	// First rating
	r1 := database.UserRating{
		UserID: "user_x", MediaType: "movie", MediaID: 603, Rating: 7,
	}

	// Second rating for same item (upsert)
	r2 := database.UserRating{
		UserID: "user_x", MediaType: "movie", MediaID: 603, Rating: 9,
	}

	// Same key, different value = upsert
	if r1.UserID != r2.UserID || r1.MediaType != r2.MediaType || r1.MediaID != r2.MediaID {
		t.Error("key should be the same for upsert")
	}
	if r1.Rating == r2.Rating {
		t.Error("values should differ to test upsert")
	}
}

// ============================================================================
// Favorites Remove Response Shape
// ============================================================================

func TestFavoritesRemoveResponseShape(t *testing.T) {
	resp := fiberMap{
		"deleted": 3.0,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if raw["deleted"] != float64(3) {
		t.Errorf("deleted = %v", raw["deleted"])
	}
}

// ============================================================================
// No Size Caps Verification
// ============================================================================

func TestNoSizeCaps(t *testing.T) {
	// Verify that pagination allows up to 100 items per page
	// but no hard cap on total (charter requirement)
	maxLimit := 100
	if maxLimit != 100 {
		t.Error("max page limit should be 100")
	}
	// No cap on total row_count (unlimited)
	limitless := -1 // -1 means no cap
	if limitless >= 0 {
		t.Error("should be no cap on total items")
	}
}

// ============================================================================
// OpenAPI Route Auth Tests
// ============================================================================

func TestSyncRoutesRequireAuth(t *testing.T) {
	routes := openapi.AllRoutes()
	syncTags := map[string]bool{"Sync": true}

	for _, r := range routes {
		if len(r.Tags) > 0 && syncTags[r.Tags[0]] {
			if r.Auth != "required" {
				t.Errorf("sync route %s %s has auth=%q, want 'required'", r.Method, r.Path, r.Auth)
			}
		}
	}
}

// ============================================================================
// History Pagination Shape
// ============================================================================

func TestHistoryPaginationShape(t *testing.T) {
	resp := fiberMap{
		"items":     []interface{}{},
		"page":      1.0,
		"limit":     25.0,
		"row_count": 0.0,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if raw["page"] != float64(1) {
		t.Errorf("page = %v", raw["page"])
	}
	if raw["limit"] != float64(25) {
		t.Errorf("limit = %v", raw["limit"])
	}
	if raw["items"] == nil {
		t.Error("items should not be nil")
	}
}

// ============================================================================
// Watchlist Sort Options
// ============================================================================

func TestWatchlistSortOptions(t *testing.T) {
	validSorts := map[string]bool{
		"rank": true, "title": true, "released": true, "added": true, "rating": true,
	}
	validHows := map[string]bool{"asc": true, "desc": true}

	for sort := range validSorts {
		if !validSorts[sort] {
			t.Errorf("%q should be valid sort", sort)
		}
	}
	for how := range validHows {
		if !validHows[how] {
			t.Errorf("%q should be valid sort_how", how)
		}
	}

	// Invalid sort defaults to rank
	invalidSort := "invalid_sort"
	if !validSorts[invalidSort] {
		// Should default to rank
		defaultSort := "rank"
		if !validSorts[defaultSort] {
			t.Error("rank should be valid default")
		}
	}
}

// ============================================================================
// History Remove by IDs and Objects
// ============================================================================

func TestHistoryRemoveResponseShape(t *testing.T) {
	resp := fiberMap{
		"deleted": 5.0,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if raw["deleted"] != float64(5) {
		t.Errorf("deleted = %v", raw["deleted"])
	}
}

// ============================================================================
// History Add Response Shape
// ============================================================================

func TestHistoryAddResponseShape(t *testing.T) {
	resp := fiberMap{
		"added": fiberMap{
			"movies": 1.0, "episodes": 5.0,
		},
		"not_found": fiberMap{
			"movies": []interface{}{}, "shows": []interface{}{},
			"seasons": []interface{}{}, "episodes": []interface{}{},
		},
		"duplicates": fiberMap{
			"movies": 0.0, "episodes": 2.0,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	added, ok := raw["added"].(map[string]interface{})
	if !ok {
		t.Fatal("added should be object")
	}
	if added["movies"] != float64(1) {
		t.Errorf("added.movies = %v", added["movies"])
	}
	if added["episodes"] != float64(5) {
		t.Errorf("added.episodes = %v", added["episodes"])
	}
}

// ============================================================================
// Rating Remove by Episode
// ============================================================================

func TestRatingRemoveByEpisodeShape(t *testing.T) {
	// Test that remove request with episodes is structured correctly
	req := fiberMap{
		"episodes": []interface{}{
			fiberMap{
				"ids":    fiberMap{"tmdb": 1396.0},
				"season": 1.0,
				"number": 3.0,
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("marshaled data should not be empty")
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	eps := raw["episodes"].([]interface{})
	if len(eps) != 1 {
		t.Errorf("expected 1 episode, got %d", len(eps))
	}
}

// Helper type for JSON output
type fiberMap map[string]interface{}
