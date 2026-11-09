package episodes

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// seedNestedFixture builds show (season 1) -> episodes 1 and 2 with the history,
// collection, comment, list and rating rows the contract stats/ratings routes
// aggregate. Returns the show id and the two episode ids.
func seedNestedFixture(t *testing.T, db *gorm.DB) (showID int64, epIDs []int64) {
	t.Helper()
	now := time.Now().UTC()

	show := database.Show{Title: "Nested Show", TmdbID: 9101, SeasonCount: 1, EpisodeCount: 2}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	season := database.Season{ShowID: show.ID, SeasonNumber: 1, EpisodeCount: 2, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	for i := 1; i <= 2; i++ {
		ep := database.Episode{SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: i, Title: "N" + strconv.Itoa(i), CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&ep).Error; err != nil {
			t.Fatalf("seed episode: %v", err)
		}
		epIDs = append(epIDs, ep.ID)
	}

	// Episode 1: 2 distinct watchers, 3 plays.
	for i, u := range []string{"user-a", "user-a", "user-b"} {
		seasonID := season.ID
		episodeID := epIDs[0]
		src := "play-" + strconv.Itoa(i)
		if err := db.Create(&database.UserHistory{
			UserID: u, MediaType: "episode", MediaID: episodeID,
			SeasonID: &seasonID, EpisodeID: &episodeID, WatchedAt: now, Source: "api", SourceID: &src, CreatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}
	if err := db.Create(&database.UserCollection{UserID: "user-a", MediaType: "episode", MediaID: epIDs[0], CollectedAt: now}).Error; err != nil {
		t.Fatalf("seed collection: %v", err)
	}
	mid := epIDs[0]
	if err := db.Create(&database.Comment{UserID: "user-a", MediaType: "episode", MediaID: &mid, CommentType: "shout", Body: "hi", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	l := database.List{UserID: "owner", Name: "Episodes", Slug: "nested-episodes", Privacy: "public", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&l).Error; err != nil {
		t.Fatalf("seed list: %v", err)
	}
	if err := db.Create(&database.ListItem{ListID: l.ID, MediaType: "episode", MediaID: epIDs[0], Rank: 1, AddedBy: "owner", AddedAt: now}).Error; err != nil {
		t.Fatalf("seed list item: %v", err)
	}
	for _, r := range []struct {
		user string
		val  int
	}{{"user-a", 7}, {"user-b", 9}} {
		if err := db.Create(&database.UserRating{
			UserID: r.user, MediaType: "episode", MediaID: epIDs[0], Rating: r.val, RatedAt: now, CreatedAt: now, UpdatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed rating: %v", err)
		}
	}
	return show.ID, epIDs
}

func TestContractEpisodeStats(t *testing.T) {
	db := setupTestDB(t)
	showID, _ := seedNestedFixture(t, db)
	app := makeEpisodesApp()

	t.Run("aggregates every contract field from stored rows", func(t *testing.T) {
		resp := doGET(t, app, "/v1/shows/"+strconv.FormatInt(showID, 10)+"/seasons/1/episodes/1/stats")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var body struct {
			Watchers   int64 `json:"watchers"`
			Plays      int64 `json:"plays"`
			Collectors int64 `json:"collectors"`
			Comments   int64 `json:"comments"`
			Lists      int64 `json:"lists"`
			Votes      int64 `json:"votes"`
		}
		decodeBody(t, resp, &body)
		want := struct {
			Watchers, Plays, Collectors, Comments, Lists, Votes int64
		}{2, 3, 1, 1, 1, 2}
		if body.Watchers != want.Watchers || body.Plays != want.Plays || body.Collectors != want.Collectors ||
			body.Comments != want.Comments || body.Lists != want.Lists || body.Votes != want.Votes {
			t.Errorf("stats = %+v, want %+v", body, want)
		}
	})

	t.Run("404 for unknown season and episode", func(t *testing.T) {
		for _, path := range []string{
			"/v1/shows/" + strconv.FormatInt(showID, 10) + "/seasons/9/episodes/1/stats",
			"/v1/shows/" + strconv.FormatInt(showID, 10) + "/seasons/1/episodes/9/stats",
		} {
			resp := doGET(t, app, path)
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s: expected 404, got %d", path, resp.StatusCode)
			}
		}
	})

	t.Run("400 for non-numeric season or episode", func(t *testing.T) {
		for _, path := range []string{
			"/v1/shows/" + strconv.FormatInt(showID, 10) + "/seasons/x/episodes/1/stats",
			"/v1/shows/" + strconv.FormatInt(showID, 10) + "/seasons/1/episodes/x/stats",
		} {
			resp := doGET(t, app, path)
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("%s: expected 400, got %d", path, resp.StatusCode)
			}
		}
	})
}

func TestContractEpisodeRatings(t *testing.T) {
	db := setupTestDB(t)
	showID, _ := seedNestedFixture(t, db)
	app := makeEpisodesApp()

	t.Run("distribution is percentages over all ten buckets", func(t *testing.T) {
		resp := doGET(t, app, "/v1/shows/"+strconv.FormatInt(showID, 10)+"/seasons/1/episodes/1/ratings")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var body struct {
			Rating       *float64           `json:"rating"`
			Votes        int64              `json:"votes"`
			Distribution map[string]float64 `json:"distribution"`
		}
		decodeBody(t, resp, &body)

		if body.Rating == nil || *body.Rating != 8 {
			t.Errorf("rating = %v, want 8", body.Rating)
		}
		if body.Votes != 2 {
			t.Errorf("votes = %d, want 2", body.Votes)
		}
		if len(body.Distribution) != 10 {
			t.Errorf("distribution has %d buckets, want 10", len(body.Distribution))
		}
		if body.Distribution["7"] != 50 || body.Distribution["9"] != 50 {
			t.Errorf("distribution = %v, want 7:50 9:50", body.Distribution)
		}
		if body.Distribution["1"] != 0 || body.Distribution["10"] != 0 {
			t.Errorf("empty buckets must be 0, got %v", body.Distribution)
		}
	})

	t.Run("no votes serves null rating, zero votes, zeroed buckets", func(t *testing.T) {
		resp := doGET(t, app, "/v1/shows/"+strconv.FormatInt(showID, 10)+"/seasons/1/episodes/2/ratings")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var raw map[string]json.RawMessage
		decodeBody(t, resp, &raw)
		if string(raw["rating"]) != "null" {
			t.Errorf("rating = %s, want null", raw["rating"])
		}
		var votes float64
		if err := json.Unmarshal(raw["votes"], &votes); err != nil || votes != 0 {
			t.Errorf("votes = %s, want 0", raw["votes"])
		}
	})
}
