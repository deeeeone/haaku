package users

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// newContractApp mounts the user routes the way httpapi.RegisterRoutes does:
// the caller-owned statics first, then the profile read surface.
func newContractApp(callerID string) *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1")
	v1.Use(func(c *fiber.Ctx) error {
		if callerID != "" {
			c.Locals("user_id", callerID)
		}
		return c.Next()
	})
	RegisterRoutes(v1)
	RegisterProfileReadRoutes(v1)
	RegisterContractRoutes(v1)
	return app
}

func contractTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := profileTestDB(t)
	if err := db.AutoMigrate(
		&database.Episode{}, &database.Season{}, &database.CommentReaction{},
		&database.SavedFilter{}, &database.UserReport{}, &database.UserFavorite{},
		&database.Person{},
	); err != nil {
		t.Fatalf("migrate contract test tables: %v", err)
	}
	return db
}

// contractProfilePaths are the per-profile contract reads registered by this
// lane. Item reads are included in the privacy case only: they need a real id
// to answer 200.
var contractProfilePaths = []string{
	"/v1/users/target/history/movies",
	"/v1/users/target/history/shows",
	"/v1/users/target/history/episodes",
	"/v1/users/target/ratings/movies",
	"/v1/users/target/ratings/shows",
	"/v1/users/target/ratings/episodes",
	"/v1/users/target/ratings/movies/5",
	"/v1/users/target/watched/episodes/",
	"/v1/users/target/watched/seasons/",
	"/v1/users/target/favorites/movies/rank",
	"/v1/users/target/favorites/shows/rank",
	"/v1/users/target/favorites/media/rank",
	"/v1/users/target/favorites/movies/rank/asc",
	"/v1/users/target/watchlist/movies/rank",
	"/v1/users/target/watchlist/shows/rank",
	"/v1/users/target/watchlist/movie,show/rank",
	"/v1/users/target/watchlist/comments/newest",
	"/v1/users/target/friends/activities",
}

// Item reads need a real media row to answer 200, so they join only the privacy
// assertion (the gate runs before resolution).
var contractProfileItemPaths = []string{
	"/v1/users/target/history/movies/1",
	"/v1/users/target/history/shows/1",
	"/v1/users/target/history/episodes/1",
}

// TestContractProfilePrivateIs403NotEmpty is the lane's central distinction: a
// stranger on a private profile gets 403, while a public profile with no rows
// gets 200 + an empty array. An empty body is exactly what a silent privacy
// bypass would look like, so the test asserts the status codes differ.
func TestContractProfilePrivateIs403NotEmpty(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	seedUser(t, db, "stranger", false, "active")

	public := newContractApp("stranger")
	for _, path := range contractProfilePaths {
		status, body := doGET(t, public, path)
		if status != http.StatusOK {
			t.Errorf("%s: public profile with no rows: want 200, got %d (%s)", path, status, body)
			continue
		}
		var arr []any
		if err := json.Unmarshal([]byte(body), &arr); err != nil {
			t.Errorf("%s: empty read must be a JSON array, got %s", path, body)
		}
	}

	// Flip the same profile private; every path must now be 403, not 200.
	if err := db.Model(&database.User{}).Where("id = ?", "target").Update("is_private", true).Error; err != nil {
		t.Fatalf("make target private: %v", err)
	}
	for _, path := range append(contractProfilePaths, contractProfileItemPaths...) {
		status, body := doGET(t, public, path)
		if status != http.StatusForbidden {
			t.Errorf("%s: private profile to stranger: want 403, got %d (%s)", path, status, body)
		}
	}
}

// TestContractProfileReadsReturnSeededRows proves the reads serve real rows, not
// a hard-coded empty array.
func TestContractProfileReadsReturnSeededRows(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	now := time.Now()
	movie := database.Movie{TmdbID: 501, Title: "Seed Movie", Slug: "seed-movie", CreatedAt: now, UpdatedAt: now}
	show := database.Show{TmdbID: 502, Title: "Seed Show", Slug: "seed-show", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	if err := db.Create(&database.UserHistory{
		UserID: "target", MediaType: "movie", MediaID: movie.ID, WatchedAt: now, Source: "api",
	}).Error; err != nil {
		t.Fatalf("seed history: %v", err)
	}
	if err := db.Create(&database.UserRating{
		UserID: "target", MediaType: "movie", MediaID: movie.ID, Rating: 8, RatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed rating: %v", err)
	}
	if err := db.Create(&database.UserFavorite{
		UserID: "target", MediaType: "movie", MediaID: movie.ID, Rank: 1, FavoritedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed favorite: %v", err)
	}
	if err := db.Create(&database.UserWatchlist{
		UserID: "target", MediaType: "show", MediaID: show.ID, Rank: 1, ListedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed watchlist: %v", err)
	}

	app := newContractApp("stranger")
	cases := []struct {
		path string
		want string
	}{
		{"/v1/users/target/history/movies", `"Seed Movie"`},
		{"/v1/users/target/ratings/movies", `"rating":8`},
		{"/v1/users/target/favorites/movies/rank", `"Seed Movie"`},
		{"/v1/users/target/watchlist/shows/rank", `"Seed Show"`},
		{"/v1/users/target/watched/movies", `seed-movie`},
		{"/v1/users/target/watched/shows", `{}`},
	}
	for _, tc := range cases {
		status, body := doGET(t, app, tc.path)
		if status != http.StatusOK {
			t.Errorf("%s: want 200, got %d (%s)", tc.path, status, body)
			continue
		}
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: want body to contain %s, got %s", tc.path, tc.want, body)
		}
	}
}

// TestContractProfileHeaderHasContractFields proves GET /users/{id} carries the
// contract's required profile properties.
func TestContractProfileHeaderHasContractFields(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")

	status, body := doGET(t, newContractApp("stranger"), "/v1/users/target")
	if status != http.StatusOK {
		t.Fatalf("profile header: want 200, got %d (%s)", status, body)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	for _, key := range []string{"username", "private", "deleted", "ids", "joined_at", "about", "images"} {
		if _, ok := got[key]; !ok {
			t.Errorf("profile header missing contract field %q: %s", key, body)
		}
	}
	ids, ok := got["ids"].(map[string]any)
	if !ok {
		t.Fatalf("ids is not an object: %s", body)
	}
	if _, ok := ids["trakt"].(float64); !ok {
		t.Errorf("ids.trakt must be an integer, got %v", ids["trakt"])
	}
}

// TestContractAccountReadsRequireAuth covers the caller-owned statics: they have
// no target profile to filter, so authentication is the gate.
func TestContractAccountReadsRequireAuth(t *testing.T) {
	contractTestDB(t)
	app := newContractApp("")
	for _, path := range []string{
		"/v1/users/blocked",
		"/v1/users/reactions/comments",
		"/v1/users/saved_filters/movies",
	} {
		status, body := doGET(t, app, path)
		if status != http.StatusUnauthorized {
			t.Errorf("%s: anonymous: want 401, got %d (%s)", path, status, body)
		}
	}
}

// TestContractReportUserPersists proves the report write stores a real row.
func TestContractReportUserPersists(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	seedUser(t, db, "reporter", false, "active")

	app := newContractApp("reporter")
	req, _ := http.NewRequest(http.MethodPost, "/v1/users/target/report", strings.NewReader(`{"reason":"spam","message":"noise"}`))
	req.Host = "example.com"
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("report request: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("report: want 201, got %d", resp.StatusCode)
	}
	var count int64
	if err := db.Model(&database.UserReport{}).
		Where("reporter_id = ? AND reported_user_id = ?", "reporter", "target").Count(&count).Error; err != nil {
		t.Fatalf("count reports: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1 stored report, got %d", count)
	}
}
