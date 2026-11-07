package media

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// setupCommentsDB reuses the shows DB helper and adds the comments table.
func setupCommentsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupShowsDB(t)
	if err := db.AutoMigrate(&database.Comment{}); err != nil {
		t.Fatalf("migrate comments table: %v", err)
	}
	return db
}

// seedComment creates a comment on mediaType/mediaID with explicit counters and
// timestamps so the sort order is deterministic.
func seedComment(t *testing.T, db *gorm.DB, mediaType string, mediaID int64, createdAt time.Time, likes, replies int) int64 {
	t.Helper()
	id := mediaID
	c := database.Comment{
		UserID:      "author",
		MediaType:   mediaType,
		MediaID:     &id,
		CommentType: "shout",
		Body:        "body",
		LikeCount:   likes,
		ReplyCount:  replies,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
	if err := db.Create(&c).Error; err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	return c.ID
}

// decodeComments decodes the bare JSON array body and captures headers.
func decodeComments(t *testing.T, resp *http.Response) []map[string]any {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode comments body: %v", err)
	}
	return out
}

func commentIDs(t *testing.T, resp *http.Response) []int64 {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out []struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode comments body: %v", err)
	}
	ids := make([]int64, 0, len(out))
	for _, c := range out {
		ids = append(ids, c.ID)
	}
	return ids
}

func equalIDs(a, b []int64) bool {
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

func TestMediaCommentsSortOrders(t *testing.T) {
	db := setupCommentsDB(t)

	movie := database.Movie{Title: "Sortable", TmdbID: 8001}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	a := seedComment(t, db, "movie", movie.ID, base, 5, 0)                  // oldest, most liked
	b := seedComment(t, db, "movie", movie.ID, base.Add(time.Hour), 1, 3)   // middle, most replies
	c := seedComment(t, db, "movie", movie.ID, base.Add(2*time.Hour), 2, 2) // newest
	// A reply must never appear in the media comment list.
	parent := a
	if err := db.Create(&database.Comment{
		UserID: "author", ParentID: &parent, MediaType: "movie", MediaID: &movie.ID,
		CommentType: "shout", Body: "reply", CreatedAt: base, UpdatedAt: base,
	}).Error; err != nil {
		t.Fatalf("seed reply: %v", err)
	}

	app := showsApp()

	cases := []struct {
		sort string
		want []int64
	}{
		{"newest", []int64{c, b, a}},
		{"oldest", []int64{a, b, c}},
		{"likes", []int64{a, c, b}},
		{"replies", []int64{b, c, a}},
		{"trending", []int64{a, c, b}}, // scores: a=5, c=4, b=4 (tie -> newer first)
	}
	for _, tc := range cases {
		t.Run(tc.sort, func(t *testing.T) {
			path := "/v1/movies/" + itoa(movie.ID) + "/comments/" + tc.sort
			resp := doShowsGet(t, app, path)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			got := commentIDs(t, resp)
			if !equalIDs(got, tc.want) {
				t.Errorf("order = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMediaCommentsInvalidSortIs400(t *testing.T) {
	db := setupCommentsDB(t)
	movie := database.Movie{Title: "Sortable", TmdbID: 8002}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	resp := doShowsGet(t, showsApp(), "/v1/movies/"+itoa(movie.ID)+"/comments/bogus")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestMediaCommentsEmptyArrayNotNull(t *testing.T) {
	db := setupCommentsDB(t)
	movie := database.Movie{Title: "Quiet", TmdbID: 8003}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	resp := doShowsGet(t, showsApp(), "/v1/movies/"+itoa(movie.ID)+"/comments/newest")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "[]" {
		t.Errorf("body = %s, want []", string(body))
	}
}

func TestMediaCommentsPaginationEdges(t *testing.T) {
	db := setupCommentsDB(t)
	movie := database.Movie{Title: "Paged", TmdbID: 8004}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	ids := []int64{
		seedComment(t, db, "movie", movie.ID, base, 0, 0),
		seedComment(t, db, "movie", movie.ID, base.Add(time.Hour), 0, 0),
		seedComment(t, db, "movie", movie.ID, base.Add(2*time.Hour), 0, 0),
	}

	app := showsApp()

	t.Run("first page sets headers and Link rel=next", func(t *testing.T) {
		resp := doShowsGet(t, app, "/v1/movies/"+itoa(movie.ID)+"/comments/newest?page=1&limit=2")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if got := resp.Header.Get("X-Pagination-Total-Items"); got != "3" {
			t.Errorf("total-items = %q, want 3", got)
		}
		if got := resp.Header.Get("X-Pagination-Total-Pages"); got != "2" {
			t.Errorf("total-pages = %q, want 2", got)
		}
		if got := resp.Header.Get("X-Pagination-Items-Count"); got != "2" {
			t.Errorf("items-count = %q, want 2", got)
		}
		if link := resp.Header.Get("Link"); link == "" || !strings.Contains(link, "page=2") || !strings.Contains(link, `rel="next"`) {
			t.Errorf("Link = %q, want a rel=next link to page 2", link)
		}
		got := commentIDs(t, resp)
		if !equalIDs(got, []int64{ids[2], ids[1]}) {
			t.Errorf("page 1 = %v, want newest two", got)
		}
	})

	t.Run("last page has no Link header", func(t *testing.T) {
		resp := doShowsGet(t, app, "/v1/movies/"+itoa(movie.ID)+"/comments/newest?page=2&limit=2")
		defer func() { _ = resp.Body.Close() }()
		if got := resp.Header.Get("Link"); got != "" {
			t.Errorf("Link = %q, want empty on the last page", got)
		}
		if got := commentIDs(t, resp); !equalIDs(got, []int64{ids[0]}) {
			t.Errorf("page 2 = %v, want the oldest", got)
		}
	})

	t.Run("page beyond the end is an empty array", func(t *testing.T) {
		resp := doShowsGet(t, app, "/v1/movies/"+itoa(movie.ID)+"/comments/newest?page=9&limit=2")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if got := resp.Header.Get("X-Pagination-Items-Count"); got != "0" {
			t.Errorf("items-count = %q, want 0", got)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "[]" {
			t.Errorf("body = %s, want []", string(body))
		}
	})

	t.Run("limit is clamped to 100", func(t *testing.T) {
		resp := doShowsGet(t, app, "/v1/movies/"+itoa(movie.ID)+"/comments/newest?limit=1000")
		defer func() { _ = resp.Body.Close() }()
		if got := resp.Header.Get("X-Pagination-Limit"); got != "100" {
			t.Errorf("limit = %q, want 100", got)
		}
	})
}

func TestMediaCommentsNestedParentResolution(t *testing.T) {
	db := setupCommentsDB(t)

	show := database.Show{Title: "Nested", TmdbID: 8005}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	season := database.Season{ShowID: show.ID, SeasonNumber: 1}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	episode := database.Episode{SeasonID: season.ID, ShowID: show.ID, EpisodeNumber: 1, Title: "Pilot"}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatalf("seed episode: %v", err)
	}

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	seasonID := season.ID
	episodeID := episode.ID
	showID := show.ID
	seedComment(t, db, "season", seasonID, base, 0, 0)
	seedComment(t, db, "episode", episodeID, base, 0, 0)
	seedComment(t, db, "show", showID, base, 0, 0)

	app := showsApp()
	showPath := "/v1/shows/" + itoa(showID)

	t.Run("season comments resolve the show then the season", func(t *testing.T) {
		resp := doShowsGet(t, app, showPath+"/seasons/1/comments/newest")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if got := commentIDs(t, resp); len(got) != 1 {
			t.Errorf("season comments = %v, want 1", got)
		}
	})

	t.Run("episode comments resolve the whole chain", func(t *testing.T) {
		resp := doShowsGet(t, app, showPath+"/seasons/1/episodes/1/comments/newest")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if got := commentIDs(t, resp); len(got) != 1 {
			t.Errorf("episode comments = %v, want 1", got)
		}
	})

	t.Run("missing show is 404", func(t *testing.T) {
		resp := doShowsGet(t, app, "/v1/shows/999999/comments/newest")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("missing season is 404", func(t *testing.T) {
		resp := doShowsGet(t, app, showPath+"/seasons/9/comments/newest")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("missing episode is 404", func(t *testing.T) {
		resp := doShowsGet(t, app, showPath+"/seasons/1/episodes/9/comments/newest")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("missing movie is 404", func(t *testing.T) {
		resp := doShowsGet(t, app, "/v1/movies/999999/comments/newest")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("invalid season number is 400", func(t *testing.T) {
		resp := doShowsGet(t, app, showPath+"/seasons/abc/comments/newest")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})
}

func TestMediaCommentsUseSharedBuilderShape(t *testing.T) {
	db := setupCommentsDB(t)
	movie := database.Movie{Title: "Shaped", TmdbID: 8006}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	id := movie.ID
	edited := time.Date(2026, 4, 2, 3, 4, 5, 0, time.UTC)
	created := time.Date(2026, 4, 1, 3, 4, 5, 0, time.UTC)
	if err := db.Create(&database.Comment{
		UserID: "author", MediaType: "movie", MediaID: &id, CommentType: "review",
		Body: "a review", Spoiler: true, LikeCount: 4, ReplyCount: 2,
		CreatedAt: created, UpdatedAt: edited, EditedAt: &edited,
	}).Error; err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	resp := doShowsGet(t, showsApp(), "/v1/movies/"+itoa(id)+"/comments/newest")
	items := decodeComments(t, resp)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	c := items[0]
	// The comments package's builder is the one that emits updated_at/edited_at;
	// the episodes package's local copy omitted them. Assert the full shape is
	// present, i.e. the media routes reuse the comment builder rather than
	// re-declaring a truncated one.
	for _, field := range []string{"id", "user_id", "comment_type", "body", "spoiler", "like_count", "reply_count", "created_at", "updated_at", "edited_at", "media_type", "media_id"} {
		if _, ok := c[field]; !ok {
			t.Errorf("comment payload missing field %q: %v", field, c)
		}
	}
	if c["comment_type"] != "review" || c["spoiler"] != true {
		t.Errorf("comment_type/spoiler = %v/%v, want review/true", c["comment_type"], c["spoiler"])
	}
}
