package media

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// statsPayload is the Trakt movie/show stats body. collected_episodes is a
// pointer so the test can tell "absent" (movies) from "computed zero" (shows).
type statsPayload struct {
	Watchers          int64  `json:"watchers"`
	Plays             int64  `json:"plays"`
	Collectors        int64  `json:"collectors"`
	CollectedEpisodes *int64 `json:"collected_episodes"`
	Comments          int64  `json:"comments"`
	Lists             int64  `json:"lists"`
	Votes             int64  `json:"votes"`
	Favorited         int64  `json:"favorited"`
	Recommended       int64  `json:"recommended"`
}

func setupStatsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupShowsDB(t)
	models := []interface{}{
		&database.UserHistory{}, &database.UserCollection{}, &database.UserRating{},
		&database.UserFavorite{}, &database.Comment{}, &database.List{}, &database.ListItem{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate stats tables: %v", err)
	}
	return db
}

func seedStatsMovie(t *testing.T, db *gorm.DB, tmdbID int, title string) int64 {
	t.Helper()
	m := database.Movie{Title: title, TmdbID: tmdbID}
	if err := db.Create(&m).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	return m.ID
}

func seedStatsShow(t *testing.T, db *gorm.DB, tmdbID int, title string) int64 {
	t.Helper()
	s := database.Show{Title: title, TmdbID: tmdbID}
	if err := db.Create(&s).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	return s.ID
}

func seedHistory(t *testing.T, db *gorm.DB, userID, mediaType string, mediaID int64, source string) {
	t.Helper()
	h := database.UserHistory{
		UserID: userID, MediaType: mediaType, MediaID: mediaID,
		WatchedAt: time.Now().UTC(), Source: source,
	}
	if err := db.Create(&h).Error; err != nil {
		t.Fatalf("seed history: %v", err)
	}
}

func seedCollection(t *testing.T, db *gorm.DB, userID, mediaType string, mediaID int64) {
	t.Helper()
	c := database.UserCollection{
		UserID: userID, MediaType: mediaType, MediaID: mediaID, CollectedAt: time.Now().UTC(),
	}
	if err := db.Create(&c).Error; err != nil {
		t.Fatalf("seed collection: %v", err)
	}
}

func seedRating(t *testing.T, db *gorm.DB, userID, mediaType string, mediaID int64, rating int) {
	t.Helper()
	r := database.UserRating{
		UserID: userID, MediaType: mediaType, MediaID: mediaID,
		Rating: rating, RatedAt: time.Now().UTC(),
	}
	if err := db.Create(&r).Error; err != nil {
		t.Fatalf("seed rating: %v", err)
	}
}

func seedFavorite(t *testing.T, db *gorm.DB, userID, mediaType string, mediaID int64) {
	t.Helper()
	f := database.UserFavorite{
		UserID: userID, MediaType: mediaType, MediaID: mediaID, FavoritedAt: time.Now().UTC(),
	}
	if err := db.Create(&f).Error; err != nil {
		t.Fatalf("seed favorite: %v", err)
	}
}

func seedStatsComment(t *testing.T, db *gorm.DB, mediaType string, mediaID int64) {
	t.Helper()
	c := database.Comment{
		UserID: "user-1", MediaType: mediaType, MediaID: &mediaID,
		CommentType: "shout", Body: "stats comment",
	}
	if err := db.Create(&c).Error; err != nil {
		t.Fatalf("seed comment: %v", err)
	}
}

func seedListItem(t *testing.T, db *gorm.DB, mediaType string, mediaID int64) {
	t.Helper()
	list := database.List{UserID: "user-1", Name: "Stats", Slug: "stats-" + mediaType, Privacy: "public"}
	if err := db.Create(&list).Error; err != nil {
		t.Fatalf("seed list: %v", err)
	}
	item := database.ListItem{
		ListID: list.ID, MediaType: mediaType, MediaID: mediaID,
		AddedBy: "user-1", AddedAt: time.Now().UTC(),
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("seed list item: %v", err)
	}
}

func decodeStats(t *testing.T, resp *http.Response) statsPayload {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out statsPayload
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode stats body: %v", err)
	}
	return out
}

func TestMovieStats(t *testing.T) {
	db := setupStatsDB(t)
	movieID := seedStatsMovie(t, db, 8001, "Stats Movie")

	// 2 watchers / 3 plays (the second user has two play sources).
	seedHistory(t, db, "user-1", "movie", movieID, "api")
	seedHistory(t, db, "user-2", "movie", movieID, "api")
	seedHistory(t, db, "user-2", "movie", movieID, "scrobble")
	seedCollection(t, db, "user-1", "movie", movieID)
	seedRating(t, db, "user-1", "movie", movieID, 8)
	seedFavorite(t, db, "user-1", "movie", movieID)
	seedStatsComment(t, db, "movie", movieID)
	seedListItem(t, db, "movie", movieID)

	resp := doShowsGet(t, showsApp(), "/v1/movies/"+itoa(movieID)+"/stats")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", resp.StatusCode, errorMessage(t, resp))
	}

	want := statsPayload{
		Watchers: 2, Plays: 3, Collectors: 1, Comments: 1,
		Lists: 1, Votes: 1, Favorited: 1, Recommended: 1,
	}
	if got := decodeStats(t, resp); !reflect.DeepEqual(got, want) {
		t.Errorf("stats = %+v, want %+v", got, want)
	}
}

func TestShowStats(t *testing.T) {
	db := setupStatsDB(t)
	now := time.Now().UTC()
	showID, episodes := seedShowWithEpisodes(t, db, 8002, "Stats Show", now, now)

	// Plays/watchers come from the show's episode history rows.
	seedHistory(t, db, "user-1", "episode", episodes[0], "api")
	seedHistory(t, db, "user-1", "episode", episodes[1], "api")
	seedHistory(t, db, "user-2", "episode", episodes[1], "api")

	// Show-level collection is a collector; episode-level rows are
	// collected_episodes.
	seedCollection(t, db, "user-1", "show", showID)
	seedCollection(t, db, "user-1", "episode", episodes[0])
	seedCollection(t, db, "user-2", "episode", episodes[1])

	seedRating(t, db, "user-1", "show", showID, 9)
	seedFavorite(t, db, "user-1", "show", showID)
	seedStatsComment(t, db, "show", showID)
	seedListItem(t, db, "show", showID)

	resp := doShowsGet(t, showsApp(), "/v1/shows/"+itoa(showID)+"/stats")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", resp.StatusCode, errorMessage(t, resp))
	}

	collected := int64(2)
	want := statsPayload{
		Watchers: 2, Plays: 3, Collectors: 1, CollectedEpisodes: &collected,
		Comments: 1, Lists: 1, Votes: 1, Favorited: 1, Recommended: 1,
	}
	if got := decodeStats(t, resp); !reflect.DeepEqual(got, want) {
		t.Errorf("stats = %+v, want %+v", got, want)
	}
}

func TestStatsZeroActivity(t *testing.T) {
	db := setupStatsDB(t)
	movieID := seedStatsMovie(t, db, 8003, "Quiet Movie")
	showID := seedStatsShow(t, db, 8004, "Quiet Show")
	app := showsApp()

	movieResp := doShowsGet(t, app, "/v1/movies/"+itoa(movieID)+"/stats")
	if movieResp.StatusCode != http.StatusOK {
		t.Fatalf("movie status = %d, want 200 (%s)", movieResp.StatusCode, errorMessage(t, movieResp))
	}
	if got := decodeStats(t, movieResp); !reflect.DeepEqual(got, statsPayload{}) {
		t.Errorf("movie stats = %+v, want all zeros", got)
	}

	showResp := doShowsGet(t, app, "/v1/shows/"+itoa(showID)+"/stats")
	if showResp.StatusCode != http.StatusOK {
		t.Fatalf("show status = %d, want 200 (%s)", showResp.StatusCode, errorMessage(t, showResp))
	}
	zero := int64(0)
	want := statsPayload{CollectedEpisodes: &zero}
	if got := decodeStats(t, showResp); !reflect.DeepEqual(got, want) {
		t.Errorf("show stats = %+v, want all zeros incl. collected_episodes", got)
	}
}

func TestStatsIDErrors(t *testing.T) {
	db := setupStatsDB(t)
	seedStatsMovie(t, db, 8005, "Exists Movie")
	seedStatsShow(t, db, 8006, "Exists Show")
	app := showsApp()

	cases := []struct {
		path string
		want int
	}{
		{"/v1/movies/999999/stats", http.StatusNotFound},
		{"/v1/shows/999999/stats", http.StatusNotFound},
		{"/v1/movies/not-an-int/stats", http.StatusBadRequest},
		{"/v1/shows/not-an-int/stats", http.StatusBadRequest},
	}
	for _, tc := range cases {
		resp := doShowsGet(t, app, tc.path)
		if resp.StatusCode != tc.want {
			t.Errorf("GET %s = %d, want %d", tc.path, resp.StatusCode, tc.want)
			continue
		}
		_ = errorMessage(t, resp) // asserts the error envelope is present
	}
}

func TestRatingDistributionHistogram(t *testing.T) {
	db := setupStatsDB(t)
	movieID := seedStatsMovie(t, db, 8007, "Rated Movie")
	showID := seedStatsShow(t, db, 8008, "Rated Show")

	for _, ref := range []struct {
		mediaType string
		mediaID   int64
	}{{"movie", movieID}, {"show", showID}} {
		seedRating(t, db, "user-1", ref.mediaType, ref.mediaID, 6)
		seedRating(t, db, "user-2", ref.mediaType, ref.mediaID, 8)
	}

	app := showsApp()
	for _, path := range []string{
		"/v1/movies/" + itoa(movieID) + "/ratings",
		"/v1/shows/" + itoa(showID) + "/ratings",
	} {
		resp := doShowsGet(t, app, path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200 (%s)", path, resp.StatusCode, errorMessage(t, resp))
		}
		var got RatingSummary
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			_ = resp.Body.Close()
			t.Fatalf("decode %s: %v", path, err)
		}
		_ = resp.Body.Close()

		want := map[string]int64{}
		for i := 1; i <= 10; i++ {
			want[strconv.Itoa(i)] = 0
		}
		want["6"] = 1
		want["8"] = 1
		if !reflect.DeepEqual(got.Distribution, want) {
			t.Errorf("GET %s distribution = %v, want %v", path, got.Distribution, want)
		}
	}
}
