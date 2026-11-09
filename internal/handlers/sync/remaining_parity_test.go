package sync

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// ============================================================================
// progress sort ordering (both directions)
// ============================================================================

// seedProgressOrder seeds four shows whose last-watched times are ordered and
// distinct, plus one show whose only history row points at an unaired episode
// (so its computed last_watched_at is nil). It returns nothing; the caller
// reads /sync/progress/watched.

func TestProgressSortOrdersBothDirections(t *testing.T) {
	db := setupRemainingDB(t)
	now := time.Now().UTC()
	aired := func(daysAgo int) *database.Date { return &database.Date{Time: now.AddDate(0, 0, -daysAgo)} }

	// show 40 watched most recently, 41 in the middle, 42 longest ago; 43 has
	// history for an episode that has not aired, so it has never effectively
	// been watched and sorts as nil.
	seed := []struct {
		show    int64
		season  int64
		episode int64
		watched *time.Time
	}{
		{show: 40, season: 40, episode: 400, watched: timePtr(now.Add(-1 * time.Hour))},
		{show: 41, season: 41, episode: 410, watched: timePtr(now.Add(-2 * time.Hour))},
		{show: 42, season: 42, episode: 420, watched: timePtr(now.Add(-3 * time.Hour))},
		{show: 43, season: 43, episode: 430, watched: nil},
	}
	for _, s := range seed {
		if err := db.Create(&database.Show{ID: s.show, TmdbID: int(s.show), Slug: "show-" + itoa(s.show), Title: "Show " + itoa(s.show), FirstAired: aired(100), Metadata: database.JSONMap{}}).Error; err != nil {
			t.Fatalf("seed show %d: %v", s.show, err)
		}
		if err := db.Create(&database.Season{ID: s.season, ShowID: s.show, SeasonNumber: 1, EpisodeCount: 1}).Error; err != nil {
			t.Fatalf("seed season %d: %v", s.season, err)
		}
		ep := &database.Episode{ID: s.episode, SeasonID: s.season, ShowID: s.show, EpisodeNumber: 1, Title: "Ep", Runtime: 30}
		if s.watched == nil {
			// Unaired: history exists but the episode cannot count as watched.
			ep.AiredAt = aired(-30)
		} else {
			ep.AiredAt = aired(10)
		}
		if err := db.Create(ep).Error; err != nil {
			t.Fatalf("seed episode %d: %v", s.episode, err)
		}
		watchedAt := time.Time{}
		if s.watched != nil {
			watchedAt = *s.watched
		}
		seasonID, epID := s.season, s.episode
		if err := db.Create(&database.UserHistory{
			UserID: remUser, MediaType: "episode", MediaID: s.episode,
			SeasonID: &seasonID, EpisodeID: &epID, WatchedAt: watchedAt, Source: "api",
		}).Error; err != nil {
			t.Fatalf("seed history %d: %v", s.episode, err)
		}
	}

	app := newRemainingApp(remUser)
	order := func(query string) []int64 {
		t.Helper()
		status, raw := doJSON(t, app, http.MethodGet, "/v1/sync/progress/watched?"+query, "")
		if status != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d (%s)", query, status, raw)
		}
		items := decodeArray(t, raw)
		ids := make([]int64, 0, len(items))
		for _, it := range items {
			show, _ := it["show"].(map[string]any)
			idsObj, _ := show["ids"].(map[string]any)
			id, _ := idsObj["trakt"].(float64)
			ids = append(ids, int64(id))
		}
		return ids
	}

	// Ascending: nil and older first.
	asc := order("sort_by=last_watched_at&sort_how=asc")
	wantAsc := []int64{43, 42, 41, 40}
	if !equalInt64(asc, wantAsc) {
		t.Errorf("asc order = %v, want %v", asc, wantAsc)
	}

	// Descending (the default): the most recent watch first, nil last.
	desc := order("sort_by=last_watched_at")
	wantDesc := []int64{40, 41, 42, 43}
	if !equalInt64(desc, wantDesc) {
		t.Errorf("default (desc) order = %v, want %v", desc, wantDesc)
	}
	explicit := order("sort_by=last_watched_at&sort_how=desc")
	if !equalInt64(explicit, wantDesc) {
		t.Errorf("explicit desc order = %v, want %v", explicit, wantDesc)
	}
}

func timePtr(t time.Time) *time.Time { return &t }

// watchlistRows returns the seeded movie and show watchlist row ids (the ids a
// read returns and a write accepts).
func watchlistRows(t *testing.T, db *gorm.DB) (movie, show int64) {
	t.Helper()
	var rows []database.UserWatchlist
	if err := db.Where("user_id = ?", remUser).Find(&rows).Error; err != nil {
		t.Fatalf("read watchlist rows: %v", err)
	}
	for _, r := range rows {
		switch r.MediaType {
		case "movie":
			movie = r.ID
		case "show":
			show = r.ID
		}
	}
	if movie == 0 || show == 0 {
		t.Fatalf("watchlist rows missing: movie=%d show=%d", movie, show)
	}
	return movie, show
}

// favoriteRows is watchlistRows for the favorites table.
func favoriteRows(t *testing.T, db *gorm.DB) (movie, show int64) {
	t.Helper()
	var rows []database.UserFavorite
	if err := db.Where("user_id = ?", remUser).Find(&rows).Error; err != nil {
		t.Fatalf("read favorite rows: %v", err)
	}
	for _, r := range rows {
		switch r.MediaType {
		case "movie":
			movie = r.ID
		case "show":
			show = r.ID
		}
	}
	if movie == 0 || show == 0 {
		t.Fatalf("favorite rows missing: movie=%d show=%d", movie, show)
	}
	return movie, show
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func equalInt64(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ============================================================================
// list item ids are per-row, not per-media
// ============================================================================

// TestListRowIDsSeparateMovieAndShowSharingMediaID covers a movie and a show
// that share a media id (they are separate tables whose sequences both start at
// 1). The contract read must return each row's own id, and the item update must
// touch only the row that id names.
func TestListRowIDsSeparateMovieAndShowSharingMediaID(t *testing.T) {
	db := setupRemainingDB(t)
	if err := db.Create(&database.Movie{ID: 7, TmdbID: 700, Slug: "shared-movie", Title: "Shared Movie", Metadata: database.JSONMap{}}).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	if err := db.Create(&database.Show{ID: 7, TmdbID: 701, Slug: "shared-show", Title: "Shared Show", Metadata: database.JSONMap{}}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	now := time.Now().UTC()
	if err := db.Create(&database.UserWatchlist{UserID: remUser, MediaType: "movie", MediaID: 7, Rank: 1, ListedAt: now}).Error; err != nil {
		t.Fatalf("seed movie row: %v", err)
	}
	if err := db.Create(&database.UserWatchlist{UserID: remUser, MediaType: "show", MediaID: 7, Rank: 2, ListedAt: now}).Error; err != nil {
		t.Fatalf("seed show row: %v", err)
	}

	app := newRemainingApp(remUser)

	// The typed contract read returns each row's own id, and they differ even
	// though the media id is the same.
	status, raw := doJSON(t, app, http.MethodGet, "/v1/sync/watchlist/movies/rank/asc", "")
	if status != http.StatusOK {
		t.Fatalf("contract read: expected 200, got %d (%s)", status, raw)
	}
	movieItems := decodeArray(t, raw)
	if len(movieItems) != 1 {
		t.Fatalf("movie read returned %d items, want 1 (%s)", len(movieItems), raw)
	}
	movieRowID, _ := movieItems[0]["id"].(float64)
	if movieRowID == 0 {
		t.Fatalf("movie read omitted the row id: %s", raw)
	}

	status, raw = doJSON(t, app, http.MethodGet, "/v1/sync/watchlist/shows/rank/asc", "")
	if status != http.StatusOK {
		t.Fatalf("contract read shows: expected 200, got %d (%s)", status, raw)
	}
	showItems := decodeArray(t, raw)
	if len(showItems) != 1 {
		t.Fatalf("show read returned %d items, want 1 (%s)", len(showItems), raw)
	}
	showRowID, _ := showItems[0]["id"].(float64)
	if showRowID == 0 {
		t.Fatalf("show read omitted the row id: %s", raw)
	}
	if movieRowID == showRowID {
		t.Fatalf("movie and show rows returned the same id %v; the id is not per-row", movieRowID)
	}

	// Updating the show row by its id must leave the movie row alone.
	status, raw = doJSON(t, app, http.MethodPut, "/v1/sync/watchlist/"+itoa(int64(showRowID)), `{"rank":9}`)
	if status != http.StatusNoContent {
		t.Fatalf("update show row: expected 204, got %d (%s)", status, raw)
	}
	var movieRow, showRow database.UserWatchlist
	db.Where("user_id = ? AND media_type = ?", remUser, "movie").First(&movieRow)
	db.Where("user_id = ? AND media_type = ?", remUser, "show").First(&showRow)
	if showRow.Rank != 9 {
		t.Errorf("show row rank = %d, want 9", showRow.Rank)
	}
	if movieRow.Rank != 1 {
		t.Errorf("movie row rank = %d, want 1 (untouched by the show update)", movieRow.Rank)
	}
	if movieRow.ID == showRow.ID {
		t.Fatalf("movie and show rows share a primary key %d", movieRow.ID)
	}

	// Reordering by the two row ids ranks them independently.
	status, raw = doJSON(t, app, http.MethodPost, "/v1/sync/watchlist/reorder",
		`{"rank":[`+itoa(showRow.ID)+`,`+itoa(movieRow.ID)+`]}`)
	if status != http.StatusOK {
		t.Fatalf("reorder: expected 200, got %d (%s)", status, raw)
	}
	var resp reorderResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode reorder: %v (%s)", err, raw)
	}
	if resp.Updated != 2 || len(resp.SkippedIDs) != 0 {
		t.Fatalf("reorder response = %+v, want 2 updated and no skipped ids", resp)
	}
	db.Where("user_id = ? AND media_type = ?", remUser, "movie").First(&movieRow)
	db.Where("user_id = ? AND media_type = ?", remUser, "show").First(&showRow)
	if showRow.Rank != 1 || movieRow.Rank != 2 {
		t.Errorf("ranks after reorder = show %d movie %d, want show 1 movie 2", showRow.Rank, movieRow.Rank)
	}
}

// ============================================================================
// playback delete: an id that spans media types is ambiguous
// ============================================================================

func TestDeletePlaybackRejectsAmbiguousMediaType(t *testing.T) {
	db := setupRemainingDB(t)
	now := time.Now().UTC()
	// A movie and an episode share media id 10.
	for _, mt := range []string{"movie", "episode"} {
		if err := db.Create(&database.UserPlayback{UserID: remUser, MediaType: mt, MediaID: 10, Progress: 5, PausedAt: now}).Error; err != nil {
			t.Fatalf("seed playback %s: %v", mt, err)
		}
	}
	// An unambiguous movie row at a different id.
	if err := db.Create(&database.UserPlayback{UserID: remUser, MediaType: "movie", MediaID: 11, Progress: 5, PausedAt: now}).Error; err != nil {
		t.Fatalf("seed playback movie 11: %v", err)
	}

	app := newRemainingApp(remUser)

	count := func(id int64) int64 {
		t.Helper()
		var n int64
		db.Model(&database.UserPlayback{}).Where("user_id = ? AND media_id = ?", remUser, id).Count(&n)
		return n
	}

	// Ambiguous: reject, delete nothing.
	status, raw := doJSON(t, app, http.MethodDelete, "/v1/sync/playback/10", "")
	if status != http.StatusBadRequest {
		t.Fatalf("ambiguous delete: expected 400, got %d (%s)", status, raw)
	}
	if got := count(10); got != 2 {
		t.Fatalf("ambiguous delete removed rows: %d remain, want 2", got)
	}

	// Unambiguous: still deletes, as before.
	status, raw = doJSON(t, app, http.MethodDelete, "/v1/sync/playback/11", "")
	if status != http.StatusNoContent {
		t.Fatalf("unambiguous delete: expected 204, got %d (%s)", status, raw)
	}
	if got := count(11); got != 0 {
		t.Fatalf("unambiguous delete left %d rows, want 0", got)
	}
	if got := count(10); got != 2 {
		t.Fatalf("unambiguous delete touched the ambiguous id: %d rows at 10, want 2", got)
	}

	// No match at all: 404.
	status, _ = doJSON(t, app, http.MethodDelete, "/v1/sync/playback/999", "")
	if status != http.StatusNotFound {
		t.Fatalf("missing id: expected 404, got %d", status)
	}
}
