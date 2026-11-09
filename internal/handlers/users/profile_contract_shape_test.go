package users

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
)

// TestHistoryEpisodeRowCarriesShow proves /history/{episodes,shows} emit the
// contract's required BOTH episode and show objects on every row, and that the
// embedded ids carry null (not an omitted key, not an empty string) for an
// external id we do not hold.
func TestHistoryEpisodeRowCarriesShow(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	show, _, episodes := seedContractMedia(t, db)
	seedEpisodeHistory(t, db, "target", episodes[0], time.Now().UTC(), "api")

	app := newContractApp("stranger")
	for _, path := range []string{
		"/v1/users/target/history/episodes",
		"/v1/users/target/history/shows",
	} {
		status, body := doGET(t, app, path)
		if status != http.StatusOK {
			t.Fatalf("%s: want 200, got %d (%s)", path, status, body)
		}
		var items []map[string]any
		if err := json.Unmarshal([]byte(body), &items); err != nil {
			t.Fatalf("%s: decode: %v (%s)", path, err, body)
		}
		if len(items) != 1 {
			t.Fatalf("%s: want 1 row, got %d (%s)", path, len(items), body)
		}
		ep, ok := items[0]["episode"].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing episode object: %s", path, body)
		}
		sh, ok := items[0]["show"].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing contract-required show object: %s", path, body)
		}
		if sh["title"] != show.Title {
			t.Errorf("%s: show.title = %v, want %q", path, sh["title"], show.Title)
		}
		// Episode ids: trakt present, the external ids we do not store are null.
		ids, ok := ep["ids"].(map[string]any)
		if !ok {
			t.Fatalf("%s: episode.ids is not an object: %s", path, body)
		}
		for _, key := range []string{"imdb", "tmdb", "tvdb"} {
			v, present := ids[key]
			if !present {
				t.Errorf("%s: episode.ids.%s key absent; want an explicit null", path, key)
			} else if v != nil {
				t.Errorf("%s: episode.ids.%s = %#v; want null (we store no external episode id)", path, key, v)
			}
		}
	}
}

// TestHistoryMovieIDsAreNullWhenUnheld proves the movie ids object carries the
// contract's nullable external ids as JSON null rather than dropping the keys or
// emitting an empty string.
func TestHistoryMovieIDsAreNullWhenUnheld(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")

	movie := database.Movie{Title: "No External Ids", Slug: "no-external-ids", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	if err := db.Create(&database.UserHistory{
		UserID: "target", MediaType: "movie", MediaID: movie.ID, WatchedAt: time.Now().UTC(), Source: "api",
	}).Error; err != nil {
		t.Fatalf("seed history: %v", err)
	}

	status, body := doGET(t, newContractApp("stranger"), "/v1/users/target/history/movies")
	if status != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", status, body)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 row, got %d (%s)", len(items), body)
	}
	ids := items[0]["movie"].(map[string]any)["ids"].(map[string]any)
	for _, key := range []string{"imdb", "tmdb", "tvdb"} {
		v, present := ids[key]
		if !present || v != nil {
			t.Errorf("ids.%s = %#v (present=%v); want an explicit null", key, v, present)
		}
	}
	if ids["slug"] != "no-external-ids" || ids["trakt"] == nil {
		t.Errorf("required ids wrong: %v", ids)
	}
}

// TestActivitiesDefaultsToLastSevenDays proves the activities feed honours the
// contract's "last 7 days" window and the start_date/end_date narrowing.
func TestActivitiesDefaultsToLastSevenDays(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	seedUser(t, db, "pub", false, "active")
	seedUser(t, db, "stranger", false, "active")
	if err := db.Create(&database.UserFollow{FollowerID: "pub", FollowingID: "target", CreatedAt: time.Now()}).Error; err != nil {
		t.Fatalf("seed follow: %v", err)
	}
	now := time.Now().UTC()
	movie := database.Movie{Title: "Activity Movie", Slug: "activity-movie", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	for _, watchedAt := range []time.Time{now.AddDate(0, 0, -10), now.Add(-time.Hour)} {
		if err := db.Create(&database.UserHistory{
			UserID: "pub", MediaType: "movie", MediaID: movie.ID, WatchedAt: watchedAt, Source: "api",
		}).Error; err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}

	app := newContractApp("stranger")
	status, body := doGET(t, app, "/v1/users/target/followers/activities")
	if status != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", status, body)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(items) != 1 {
		t.Fatalf("default 7-day window: want only the recent play, got %d (%s)", len(items), body)
	}

	// An explicit window that covers the old play returns both.
	from := now.AddDate(0, 0, -14).Format("2006-01-02")
	status, body = doGET(t, app, "/v1/users/target/followers/activities?start_date="+from)
	if status != http.StatusOK {
		t.Fatalf("start_date window: want 200, got %d (%s)", status, body)
	}
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(items) != 2 {
		t.Fatalf("explicit start_date: want both plays, got %d (%s)", len(items), body)
	}
}

// TestFavoritesCommentsServeCommentsOnFavoritedMedia proves the new
// /favorites/comments/{sort} route reads the real relation (top-level comments
// on the user's favorited media) and rejects a sort value it cannot order by.
func TestFavoritesCommentsServeCommentsOnFavoritedMedia(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	seedUser(t, db, "commenter", false, "active")
	now := time.Now().UTC()
	movie := database.Movie{Title: "Favorited Movie", Slug: "favorited-movie", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	mediaID := movie.ID
	if err := db.Create(&database.UserFavorite{
		UserID: "target", MediaType: "movie", MediaID: movie.ID, Rank: 1, FavoritedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed favorite: %v", err)
	}
	if err := db.Create(&database.Comment{
		UserID: "commenter", MediaType: "movie", MediaID: &mediaID, CommentType: "shout",
		Body: "Real favorite comment", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	app := newContractApp("stranger")
	status, body := doGET(t, app, "/v1/users/target/favorites/comments/newest")
	if status != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", status, body)
	}
	if !strings.Contains(body, "Real favorite comment") {
		t.Errorf("favorites comments must contain the stored comment, got %s", body)
	}

	// Invalid sort is a 400, not a silent default.
	status, _ = doGET(t, app, "/v1/users/target/favorites/comments/sideways")
	if status != http.StatusBadRequest {
		t.Errorf("invalid sort: want 400, got %d", status)
	}
}

// TestSmartListDraftsOwnerOnlyEmptyState proves the new route is owner-only and
// answers with the contract's documented empty state when no drafts exist.
func TestSmartListDraftsOwnerOnlyEmptyState(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "owner", false, "active")
	seedUser(t, db, "stranger", false, "active")

	status, body := doGET(t, newContractApp("owner"), "/v1/users/owner/smart-list-drafts")
	if status != http.StatusOK {
		t.Fatalf("owner: want 200, got %d (%s)", status, body)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if got["convertible_count"] != float64(0) || got["best_confidence"] != nil {
		t.Errorf("empty state wrong: %s", body)
	}
	if lists, ok := got["lists"].([]any); !ok || len(lists) != 0 {
		t.Errorf("lists must be an empty array, got %#v", got["lists"])
	}

	status, _ = doGET(t, newContractApp("stranger"), "/v1/users/owner/smart-list-drafts")
	if status != http.StatusForbidden {
		t.Errorf("stranger: want 403, got %d", status)
	}
	status, _ = doGET(t, newContractApp(""), "/v1/users/owner/smart-list-drafts")
	if status != http.StatusUnauthorized {
		t.Errorf("anonymous: want 401, got %d", status)
	}
}
