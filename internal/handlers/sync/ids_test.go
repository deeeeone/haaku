package sync

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// The reported defect this file guards: GET /v1/shows/{id} answers with
//
//	"ids":{"id":1,"slug":"severance","tmdb":95396,"imdb":"tt11280740","tvdb":"371980"}
//
// — `tvdb` as a JSON string — and every write endpoint in this package then
// rejected that exact object with `400 invalid JSON body`, because idsObj
// declared tvdb as an int. The web UI keeps the ids it was served and posts
// them straight back, so adding a title to the watchlist from the search page
// failed with an error that blamed the JSON syntax, which was valid.

// TestIDsObjAcceptsStringOrNumericIDs covers the decoder both forms have to
// pass: the one haaku serves, and the one a Trakt-shaped client sends.
func TestIDsObjAcceptsStringOrNumericIDs(t *testing.T) {
	cases := []struct {
		name string
		body string
		want idsObj
	}{
		{
			name: "numbers, as a Trakt-shaped client sends them",
			body: `{"tmdb":95396,"tvdb":371980,"imdb":"tt11280740"}`,
			want: idsObj{TMDB: 95396, TVDB: 371980, IMDB: "tt11280740"},
		},
		{
			name: "the ids block GET /v1/shows/{id} serves, echoed verbatim",
			body: `{"id":1,"slug":"severance","tmdb":95396,"imdb":"tt11280740","tvdb":"371980"}`,
			want: idsObj{TMDB: 95396, TVDB: 371980, IMDB: "tt11280740"},
		},
		{
			name: "mixed: string tvdb next to a numeric tmdb",
			body: `{"tmdb":95396,"tvdb":"371980"}`,
			want: idsObj{TMDB: 95396, TVDB: 371980},
		},
		{
			name: "both ids as strings",
			body: `{"tmdb":"95396","tvdb":"371980"}`,
			want: idsObj{TMDB: 95396, TVDB: 371980},
		},
		{
			name: "a string with surrounding whitespace",
			body: `{"tvdb":" 371980 "}`,
			want: idsObj{TVDB: 371980},
		},
		{
			name: "tvdb alone, the TVDB-first client",
			body: `{"tvdb":"371980"}`,
			want: idsObj{TVDB: 371980},
		},
		{
			name: "tvdb absent",
			body: `{"tmdb":603}`,
			want: idsObj{TMDB: 603},
		},
		{
			name: "tvdb empty string means not supplied, not an error",
			body: `{"tvdb":""}`,
			want: idsObj{},
		},
		{
			name: "tvdb null",
			body: `{"tvdb":null}`,
			want: idsObj{},
		},
		{
			name: "empty object",
			body: `{}`,
			want: idsObj{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got idsObj
			if err := json.Unmarshal([]byte(tc.body), &got); err != nil {
				t.Fatalf("decode %s: %v", tc.body, err)
			}
			if got != tc.want {
				t.Errorf("decode %s: got %+v, want %+v", tc.body, got, tc.want)
			}
		})
	}
}

// TestIDsObjRejectsNonNumericIDValues pins what is still refused: accepting a
// string id must not turn "tvdb":"banana" into a silent zero, which would be a
// wrong answer (the item lands in not_found with a mystery id) instead of a 400.
func TestIDsObjRejectsNonNumericIDValues(t *testing.T) {
	bad := []string{
		`{"tvdb":"banana"}`,
		`{"tvdb":"371980x"}`,
		`{"tvdb":"371980.5"}`,
		`{"tvdb":{"id":371980}}`,
		`{"tvdb":[371980]}`,
		`{"tvdb":true}`,
		`{"tmdb":"95x96"}`,
	}
	for _, body := range bad {
		var got idsObj
		if err := json.Unmarshal([]byte(body), &got); err == nil {
			t.Errorf("decode %s: expected an error, got %+v", body, got)
		}
	}

	// The error has to name the field: it is the only thing separating "tvdb is
	// not an id" from "the body is not JSON".
	var got idsObj
	err := json.Unmarshal([]byte(`{"tvdb":"banana"}`), &got)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "ids.tvdb") {
		t.Errorf("error %q does not name ids.tvdb", err.Error())
	}
}

// TestSyncWriteBodiesAcceptTheIDsBlockTheAPIServes decodes the full body the
// web UI posts into the real request struct, nested episode references
// included: every ids object in a sync write goes through idsObj.
func TestSyncWriteBodiesAcceptTheIDsBlockTheAPIServes(t *testing.T) {
	body := `{"movies":[{"ids":{"tmdb":603,"tvdb":"1850"},"rating":8}],` +
		`"shows":[{"ids":{"id":1,"slug":"severance","tmdb":95396,"imdb":"tt11280740","tvdb":"371980"},` +
		`"seasons":[{"number":1,"episodes":[{"ids":{"tvdb":"371981"},"number":1,"watched_at":"released"}]}]}]}`

	var req historyAddRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode the served ids block: %v", err)
	}
	if len(req.Movies) != 1 || req.Movies[0].IDs.TVDB != 1850 {
		t.Errorf("movies: got %+v, want one movie with tvdb 1850", req.Movies)
	}
	if len(req.Shows) != 1 || req.Shows[0].IDs.TVDB != 371980 || req.Shows[0].IDs.IMDB != "tt11280740" {
		t.Errorf("shows: got %+v, want one show with tvdb 371980 and imdb tt11280740", req.Shows)
	}
	episodes := req.Shows[0].Seasons[0].Episodes
	if len(episodes) != 1 || episodes[0].IDs.TVDB != 371981 {
		t.Errorf("nested episodes: got %+v, want one episode with tvdb 371981", episodes)
	}
}

// seedTVDBShow inserts a show identified the way the TVDB ingest writes it:
// the tvdb id on the row. Returns the row id.
func seedTVDBShow(t *testing.T, db *gorm.DB, tvdbID, tmdbID int) int64 {
	t.Helper()
	now := time.Now()
	show := database.Show{
		TmdbID: tmdbID, TvdbID: tvdbID, ImdbID: "tt11280740",
		Title: "Severance", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	return show.ID
}

// TestAddRatingResolvesAStringTVDBID is the handler-level regression test: the
// string id must not merely decode, it must resolve to the right row. A decoded
// zero would answer 200 with added.shows=0 and the title in not_found, so both
// the count and the written row are asserted. Ratings is the endpoint to use
// here because watchlist and favorites adds take a PostgreSQL-only advisory
// lock and cannot run in the sqlite suite — the decode they share is idsObj.
func TestAddRatingResolvesAStringTVDBID(t *testing.T) {
	db := setupRatingsDB(t)
	showID := seedTVDBShow(t, db, 371980, 95396)
	app := newRatingsApp()

	cases := []struct {
		name string
		body string
	}{
		{
			name: "string id, as GET /v1/shows/{id} serves it",
			body: `{"shows":[{"ids":{"id":1,"slug":"severance","tmdb":95396,"imdb":"tt11280740","tvdb":"371980"},"rating":9}]}`,
		},
		{
			name: "numeric id, as a Trakt-shaped client sends it",
			body: `{"shows":[{"ids":{"tmdb":95396,"tvdb":371980},"rating":9}]}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, raw := doPOST(t, app, "/v1/sync/ratings", tc.body)
			if status != http.StatusOK {
				t.Fatalf("expected 200, got %d (%s)", status, raw)
			}
			var body struct {
				Added struct {
					Shows int `json:"shows"`
				} `json:"added"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode response: %v (%s)", err, raw)
			}
			if body.Added.Shows != 1 {
				t.Errorf("expected added.shows=1 (the id resolved), got %d (%s)", body.Added.Shows, raw)
			}

			var rating database.UserRating
			if err := db.Where("user_id = ? AND media_type = ? AND media_id = ?",
				ratingsTestUser, "show", showID).First(&rating).Error; err != nil {
				t.Fatalf("expected a rating on show %d: %v", showID, err)
			}
			if rating.Rating != 9 {
				t.Errorf("expected rating 9, got %d", rating.Rating)
			}
		})
	}
}
