package users

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// seedContractMedia seeds one show with two seasons and three episodes plus the
// given history rows, so the watched reads have real rows to aggregate.
func seedContractMedia(t *testing.T, db *gorm.DB) (show database.Show, seasons map[int]database.Season, episodes []database.Episode) {
	t.Helper()
	now := time.Now().UTC()
	show = database.Show{TmdbID: 701, Title: "Parity Show", Slug: "parity-show", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	seasons = map[int]database.Season{}
	for _, number := range []int{1, 2} {
		s := database.Season{ShowID: show.ID, SeasonNumber: number, EpisodeCount: 2, CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&s).Error; err != nil {
			t.Fatalf("seed season %d: %v", number, err)
		}
		seasons[number] = s
	}
	for _, spec := range []struct {
		season int
		number int
	}{{1, 1}, {1, 2}, {2, 1}} {
		ep := database.Episode{
			ShowID: show.ID, SeasonID: seasons[spec.season].ID, EpisodeNumber: spec.number,
			Title: "Episode", CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&ep).Error; err != nil {
			t.Fatalf("seed episode s%de%d: %v", spec.season, spec.number, err)
		}
		episodes = append(episodes, ep)
	}
	return show, seasons, episodes
}

func seedEpisodeHistory(t *testing.T, db *gorm.DB, userID string, ep database.Episode, watchedAt time.Time, source string) {
	t.Helper()
	epID, seasonID := ep.ID, ep.SeasonID
	if err := db.Create(&database.UserHistory{
		UserID: userID, MediaType: "episode", MediaID: ep.ID,
		EpisodeID: &epID, SeasonID: &seasonID, WatchedAt: watchedAt, Source: source,
	}).Error; err != nil {
		t.Fatalf("seed episode history: %v", err)
	}
}

// TestContractWatchedShowsServesNestedPlays proves the batched watched/shows
// read still renders show -> season -> episode -> dates from real rows.
func TestContractWatchedShowsServesNestedPlays(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	_, _, episodes := seedContractMedia(t, db)
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	seedEpisodeHistory(t, db, "target", episodes[0], base, "api")
	seedEpisodeHistory(t, db, "target", episodes[0], base.Add(2*time.Hour), "scrobble")
	seedEpisodeHistory(t, db, "target", episodes[1], base.Add(time.Hour), "api")
	seedEpisodeHistory(t, db, "target", episodes[2], base.Add(3*time.Hour), "api")

	status, body := doGET(t, newContractApp("stranger"), "/v1/users/target/watched/shows")
	if status != http.StatusOK {
		t.Fatalf("watched/shows: want 200, got %d (%s)", status, body)
	}
	var got map[string]map[string]map[string][]string
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode watched/shows: %v (%s)", err, body)
	}
	seasonOne := got["parity-show"]["1"]
	if len(seasonOne["1"]) != 2 {
		t.Errorf("s1e1: want 2 plays, got %v (%s)", seasonOne["1"], body)
	}
	if len(seasonOne["2"]) != 1 {
		t.Errorf("s1e2: want 1 play, got %v (%s)", seasonOne["2"], body)
	}
	if len(got["parity-show"]["2"]["1"]) != 1 {
		t.Errorf("s2e1: want 1 play, got %v (%s)", got["parity-show"]["2"]["1"], body)
	}
}

// TestContractWatchedTypeAggregatesInSQL proves /watched/{type} counts plays and
// pages distinct items in the database: page 2 holds the third group, not a
// repeat of page 1.
func TestContractWatchedTypeAggregatesInSQL(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	_, _, episodes := seedContractMedia(t, db)
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	seedEpisodeHistory(t, db, "target", episodes[0], base, "api")
	seedEpisodeHistory(t, db, "target", episodes[0], base.Add(2*time.Hour), "scrobble")
	seedEpisodeHistory(t, db, "target", episodes[1], base.Add(time.Hour), "api")
	seedEpisodeHistory(t, db, "target", episodes[2], base.Add(3*time.Hour), "api")

	app := newContractApp("stranger")

	type episodePage struct {
		Plays   int `json:"plays"`
		Episode struct {
			Season int `json:"season"`
			Number int `json:"number"`
		} `json:"episode"`
	}
	var page1, page2 []episodePage
	for page, dst := range map[string]any{
		"?limit=2&page=1": &page1,
		"?limit=2&page=2": &page2,
	} {
		status, body := doGET(t, app, "/v1/users/target/watched/episodes"+page)
		if status != http.StatusOK {
			t.Fatalf("watched/episodes%s: want 200, got %d (%s)", page, status, body)
		}
		if err := json.Unmarshal([]byte(body), dst); err != nil {
			t.Fatalf("decode watched/episodes%s: %v (%s)", page, err, body)
		}
	}
	if len(page1) != 2 || len(page2) != 1 {
		t.Fatalf("pages must split 3 groups 2/1, got %d/%d", len(page1), len(page2))
	}
	// newest first: s2e1, then s1e1 (two plays), then s1e2.
	if page1[0].Episode.Season != 2 || page1[0].Episode.Number != 1 || page1[0].Plays != 1 {
		t.Errorf("page 1 item 1: want s2e1 x1, got %+v", page1[0])
	}
	if page1[1].Episode.Season != 1 || page1[1].Episode.Number != 1 || page1[1].Plays != 2 {
		t.Errorf("page 1 item 2: want s1e1 x2, got %+v", page1[1])
	}
	if page2[0].Episode.Season != 1 || page2[0].Episode.Number != 2 || page2[0].Plays != 1 {
		t.Errorf("page 2 item 1: want s1e2 x1 (rank continues), got %+v", page2[0])
	}

	// Season grouping: two seasons, the newest page holds season 2, page 2
	// holds season 1 with its three plays.
	var seasonPage []struct {
		Plays  int `json:"plays"`
		Season struct {
			Number int `json:"number"`
		} `json:"season"`
	}
	status, body := doGET(t, app, "/v1/users/target/watched/seasons?limit=1&page=2")
	if status != http.StatusOK {
		t.Fatalf("watched/seasons page 2: want 200, got %d (%s)", status, body)
	}
	if err := json.Unmarshal([]byte(body), &seasonPage); err != nil {
		t.Fatalf("decode watched/seasons: %v (%s)", err, body)
	}
	if len(seasonPage) != 1 || seasonPage[0].Season.Number != 1 || seasonPage[0].Plays != 3 {
		t.Fatalf("watched/seasons page 2: want season 1 with 3 plays, got %+v (%s)", seasonPage, body)
	}
}

// TestContractActivitiesHideInvisibleAccounts is the privacy proof for the
// activities feed: a follower's watch history appears only when the caller may
// read that follower's history route — a private, blocked or deleted account
// must not leak through the feed. Removing the VisibleUserIDs filter fails this.
func TestContractActivitiesHideInvisibleAccounts(t *testing.T) {
	db := contractTestDB(t)
	now := time.Now().UTC()
	seedUser(t, db, "target", false, "active")
	seedUser(t, db, "pub", false, "active")
	seedUser(t, db, "peeper", true, "active")
	seedUser(t, db, "blockedguy", false, "active")
	seedUser(t, db, "gone", false, "deleted")

	admin := database.User{
		ID: "admin", Username: "u-admin", Name: "User admin", IsAdmin: true,
		Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	for _, id := range []string{"fan", "stranger"} {
		seedUser(t, db, id, false, "active")
	}

	// Each account follows the profile whose activity feed we read.
	for _, follower := range []string{"pub", "peeper", "blockedguy", "gone"} {
		if err := db.Create(&database.UserFollow{
			FollowerID: follower, FollowingID: "target", CreatedAt: now,
		}).Error; err != nil {
			t.Fatalf("seed follow %s: %v", follower, err)
		}
	}
	// fan follows the private account directly, so its history is readable by
	// fan through /users/peeper/history — and therefore through the feed.
	if err := db.Create(&database.UserFollow{
		FollowerID: "fan", FollowingID: "peeper", CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed follow fan->peeper: %v", err)
	}
	// The blocked account blocks the stranger; neither may see the other.
	if err := db.Create(&database.UserBlock{BlockerID: "blockedguy", BlockedID: "stranger"}).Error; err != nil {
		t.Fatalf("seed block: %v", err)
	}

	movies := map[string]string{
		"pub": "Public Movie", "peeper": "Private Movie",
		"blockedguy": "Blocked Movie", "gone": "Deleted Movie",
	}
	for i, user := range []string{"pub", "peeper", "blockedguy", "gone"} {
		title := movies[user]
		m := database.Movie{TmdbID: 900 + i, Title: title, Slug: strings.ToLower(strings.ReplaceAll(title, " ", "-")), CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("seed movie %s: %v", title, err)
		}
		if err := db.Create(&database.UserHistory{
			UserID: user, MediaType: "movie", MediaID: m.ID, WatchedAt: now, Source: "api",
		}).Error; err != nil {
			t.Fatalf("seed history %s: %v", user, err)
		}
	}

	cases := []struct {
		caller  string
		present []string
		absent  []string
	}{
		{
			caller:  "stranger",
			present: []string{"Public Movie"},
			absent:  []string{"Private Movie", "Blocked Movie", "Deleted Movie"},
		},
		{
			// Admin: private accounts are readable; a deleted account never is.
			// The block is between blockedguy and stranger only, so it does not
			// hide blockedguy from the admin.
			caller:  "admin",
			present: []string{"Public Movie", "Private Movie", "Blocked Movie"},
			absent:  []string{"Deleted Movie"},
		},
		{
			// Approved follower of the private account.
			caller:  "fan",
			present: []string{"Public Movie", "Private Movie", "Blocked Movie"},
			absent:  []string{"Deleted Movie"},
		},
	}

	for _, tc := range cases {
		status, body := doGET(t, newContractApp(tc.caller), "/v1/users/target/followers/activities?limit=50")
		if status != http.StatusOK {
			t.Fatalf("%s: want 200, got %d (%s)", tc.caller, status, body)
		}
		for _, want := range tc.present {
			if !strings.Contains(body, want) {
				t.Errorf("%s: feed must contain %q, got %s", tc.caller, want, body)
			}
		}
		for _, hidden := range tc.absent {
			if strings.Contains(body, hidden) {
				t.Errorf("%s: feed leaked %q, got %s", tc.caller, hidden, body)
			}
		}
	}
}

// TestContractSavedFilterRankIsAbsoluteAcrossPages proves rank counts the whole
// collection, not the page: page 2 with limit 2 starts at rank 3.
func TestContractSavedFilterRankIsAbsoluteAcrossPages(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "owner", false, "active")
	base := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	for i, name := range []string{"first", "second", "third"} {
		if err := db.Create(&database.SavedFilter{
			UserID: "owner", Section: "movies", Name: name,
			Filter:    database.JSONMap{"path": "/movies/" + name},
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}).Error; err != nil {
			t.Fatalf("seed saved filter %s: %v", name, err)
		}
	}

	app := newContractApp("owner")
	ranks := func(query string) []float64 {
		t.Helper()
		status, body := doGET(t, app, "/v1/users/saved_filters/movies"+query)
		if status != http.StatusOK {
			t.Fatalf("saved filters%s: want 200, got %d (%s)", query, status, body)
		}
		var items []map[string]any
		if err := json.Unmarshal([]byte(body), &items); err != nil {
			t.Fatalf("decode saved filters%s: %v (%s)", query, err, body)
		}
		out := make([]float64, 0, len(items))
		for _, item := range items {
			rank, ok := item["rank"].(float64)
			if !ok {
				t.Fatalf("item has no numeric rank: %s", body)
			}
			out = append(out, rank)
		}
		return out
	}

	if got := ranks("?page=1&limit=2"); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("page 1: want ranks [1 2], got %v", got)
	}
	if got := ranks("?page=2&limit=2"); len(got) != 1 || got[0] != 3 {
		t.Errorf("page 2: want ranks [3] (absolute), got %v", got)
	}
}

// TestContractReportUserTwiceIsNotAnError proves a repeat report upserts instead
// of answering 500 from the (reporter_id, reported_user_id) unique index, and
// that the second report's reason wins.
func TestContractReportUserTwiceIsNotAnError(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	seedUser(t, db, "reporter", false, "active")

	app := newContractApp("reporter")
	post := func(payload string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, "/v1/users/target/report", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("build report request: %v", err)
		}
		req.Host = "example.com"
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("report request: %v", err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	if status := post(`{"reason":"spam","message":"noise"}`); status < 200 || status > 299 {
		t.Fatalf("first report: want 2xx, got %d", status)
	}
	if status := post(`{"reason":"other","message":"second thought"}`); status < 200 || status > 299 {
		t.Fatalf("second report: want 2xx, got %d", status)
	}

	var rows []database.UserReport
	if err := db.Where("reporter_id = ? AND reported_user_id = ?", "reporter", "target").Find(&rows).Error; err != nil {
		t.Fatalf("load reports: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want exactly 1 stored report, got %d", len(rows))
	}
	if rows[0].Reason != "other" || rows[0].Message != "second thought" {
		t.Errorf("re-report must update reason/message, got %q/%q", rows[0].Reason, rows[0].Message)
	}
}
