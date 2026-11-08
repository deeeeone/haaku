package tvdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// newTestServer serves the handful of TVDB routes the client uses, counting
// logins so the token cache is observable.
func newTestServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var logins int32

	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&logins, 1)
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "body", http.StatusBadRequest)
			return
		}
		if body["apikey"] == "" {
			http.Error(w, `{"status":"failure"}`, http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"token":"test-token"}}`))
	})

	// Everything below requires the bearer token, so an unauthenticated call
	// fails loudly instead of silently serving.
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"status":"failure","message":"unauthorized"}`))
				return
			}
			next(w, r)
		}
	}

	mux.HandleFunc("/search", auth(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("query") == "boom" {
			http.Error(w, `{"status":"failure"}`, http.StatusInternalServerError)
			return
		}
		// Echo the query back so the test can assert the translation.
		resp := fmt.Sprintf(`{"status":"success","data":[{
			"objectID":"series-81189","id":"series-81189","tvdb_id":"81189",
			"name":"%s","type":"%s","primary_type":"%s","year":"2008",
			"slug":"breaking-bad","image_url":"https://artworks.thetvdb.com/banners/posters/81189-10.jpg",
			"overview":"overview","remote_ids":[
				{"id":"tt0903747","type":2,"sourceName":"IMDB"},
				{"id":"1396","type":10,"sourceName":"TheMovieDB.com"}
			]},{
			"objectID":"series-99","id":"series-99","tvdb_id":"99",
			"name":"Person hit","type":"person","year":"",
			"slug":"person","remote_ids":[]
		}],"links":{"prev":null,"self":"x","next":null,"total_items":140,"page_size":25}}`,
			q.Get("query"), q.Get("type"), q.Get("type"))
		_, _ = w.Write([]byte(resp))
	}))

	mux.HandleFunc("/series/81189/extended", auth(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{
			"id":81189,"name":"Breaking Bad","slug":"breaking-bad","year":"2008",
			"firstAired":"2008-01-20","lastAired":"2013-09-29","averageRuntime":48,
			"originalLanguage":"eng","status":{"id":2,"name":"Ended"},
			"latestNetwork":{"id":20,"name":"AMC","slug":"amc"},
			"genres":[{"id":12,"name":"Drama","slug":"drama"},{"id":14,"name":"Crime","slug":"crime"}],
			"image":"https://artworks.thetvdb.com/banners/posters/81189-1.jpg",
			"remoteIds":[{"id":"tt0903747","type":2,"sourceName":"IMDB"},{"id":"1396","type":10,"sourceName":"TheMovieDB.com"}],
			"contentRatings":[{"name":"18","country":"gbr"},{"name":"TV-MA","country":"usa"}],
			"trailers":[{"id":1,"name":"FR","url":"https://youtube.com/fr","language":"fra"},
			            {"id":2,"name":"Official","url":"https://youtube.com/en","language":"eng"}],
			"artworks":[
				{"id":1,"type":3,"language":"eng","image":"https://artworks.thetvdb.com/backdrops/81189-3.jpg","score":10},
				{"id":2,"type":3,"language":"deu","image":"https://artworks.thetvdb.com/backdrops/81189-9.jpg","score":99},
				{"id":3,"type":2,"language":"eng","image":"https://artworks.thetvdb.com/posters/81189-a.jpg","score":5},
				{"id":4,"type":2,"language":"eng","image":"https://artworks.thetvdb.com/posters/81189-b.jpg","score":50},
				{"id":5,"type":5,"language":"eng","image":"https://artworks.thetvdb.com/logos/81189.png","score":1}
			]}}`))
	}))

	mux.HandleFunc("/series/404/extended", auth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":"failure","message":"not found"}`))
	}))

	// Two pages of episodes, so pagination is exercised: page 0 links to 1.
	mux.HandleFunc("/series/81189/episodes/default/eng", auth(func(w http.ResponseWriter, r *http.Request) {
		next := "null"
		episodes := `{"id":3859781,"seriesId":81189,"name":"Pilot","aired":"2008-01-20","runtime":58,
			"overview":"o","image":"/banners/episodes/81189/3859781.jpg","number":1,"absoluteNumber":1,"seasonNumber":1,"year":"2008"}`
		if r.URL.Query().Get("page") == "0" {
			next = `"https://api4.thetvdb.com/v4/series/81189/episodes/default/eng?page=1"`
		}
		_, _ = fmt.Fprintf(w, `{"status":"success","data":{"id":81189,"episodes":[%s]},
			"links":{"prev":null,"self":"x","next":%s,"total_items":2,"page_size":500}}`, episodes, next)
	}))

	mux.HandleFunc("/series/999/episodes/default/eng", auth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":"failure","message":"Season not found"}`))
	}))

	mux.HandleFunc("/movies/169/extended", auth(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{
			"id":169,"name":"The Matrix","slug":"the-matrix","year":"1999","runtime":136,
			"first_release":{"country":"global","date":"1999-03-31","detail":null},
			"status":{"id":5,"name":"Released"},
			"genres":[{"id":2,"name":"Action","slug":"action"}],
			"remoteIds":[{"id":"tt0133093","type":2,"sourceName":"IMDB"},{"id":"603","type":10,"sourceName":"TheMovieDB.com"}],
			"contentRatings":[{"name":"R","country":"usa"}],
			"artworks":[{"id":1,"type":14,"language":"eng","image":"https://artworks.thetvdb.com/movies/169/p.jpg","score":3}]}}`))
	}))

	mux.HandleFunc("/movies/404/extended", auth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":"failure"}`))
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &logins
}

func newTestClient(t *testing.T, base string) *Client {
	t.Helper()
	c := NewWithKey("test-key", "")
	c.SetBaseURL(base)
	return c
}

func TestSearch_TranslatesTypesAndCachesToken(t *testing.T) {
	srv, logins := newTestServer(t)
	c := newTestClient(t, srv.URL)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		res, err := c.Search(ctx, "Breaking Bad", []string{"show"}, 1, 25)
		if err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
		if len(res.Data) != 2 {
			t.Fatalf("search %d returned %d results, want 2", i, len(res.Data))
		}
		if got := res.Data[0].Name; got != "Breaking Bad" {
			t.Fatalf("server saw query %q, want the type translated to %q", got, "Breaking Bad")
		}
		if res.Data[0].Type != "series" {
			t.Fatalf("server saw type %q, want it translated to series", res.Data[0].Type)
		}
	}

	if got := atomic.LoadInt32(logins); got != 1 {
		t.Fatalf("three searches logged in %d times, want 1 (token must be cached)", got)
	}

	first := res2(t, c).Data[0]
	if got := first.TVDBID(); got != 81189 {
		t.Errorf("TVDBID() = %d, want 81189", got)
	}
	if got := first.MediaType(); got != "show" {
		t.Errorf("MediaType() = %q, want show", got)
	}
	if got := first.TMDBID(); got != 1396 {
		t.Errorf("TMDBID() = %d, want 1396", got)
	}
	if got := first.IMDBID(); got != "tt0903747" {
		t.Errorf("IMDBID() = %q, want tt0903747", got)
	}
	if got := first.Year(); got != 2008 {
		t.Errorf("Year() = %d, want 2008", got)
	}
	if got := res2(t, c).Data[1].MediaType(); got != "" {
		t.Errorf("a person hit must not map to a media type, got %q", got)
	}
}

func res2(t *testing.T, c *Client) *SearchResponse {
	t.Helper()
	res, err := c.Search(context.Background(), "Breaking Bad", []string{"show"}, 1, 25)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	return res
}

func TestSearch_EmptyQuerySkipsTheNetwork(t *testing.T) {
	srv, logins := newTestServer(t)
	c := newTestClient(t, srv.URL)

	res, err := c.Search(context.Background(), "   ", []string{"show"}, 1, 25)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Data) != 0 {
		t.Fatalf("empty query returned %d results, want none", len(res.Data))
	}
	if atomic.LoadInt32(logins) != 0 {
		t.Fatal("an empty query must not hit the API")
	}
}

func TestSearch_ServerErrorIsReported(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newTestClient(t, srv.URL)

	_, err := c.Search(context.Background(), "boom", nil, 1, 25)
	if err == nil {
		t.Fatal("want an error for a 500 response")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not an *APIError", err)
	}
	if apiErr.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", apiErr.Status)
	}
	if IsNotFound(err) {
		t.Fatal("a 500 must not be reported as not-found")
	}
}

func TestSeriesExtended_ArtworkAndLinks(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newTestClient(t, srv.URL)

	s, err := c.GetSeriesExtended(context.Background(), 81189)
	if err != nil {
		t.Fatalf("GetSeriesExtended: %v", err)
	}
	if s.Name != "Breaking Bad" || s.Year() != 2008 {
		t.Fatalf("series = %q year %d", s.Name, s.Year())
	}
	if got := s.Network(); got != "AMC" {
		t.Errorf("Network() = %q, want AMC", got)
	}
	if got := s.Status.Name; got != "Ended" {
		t.Errorf("Status = %q, want Ended", got)
	}
	if got := strings.Join(s.GenreNames(), ","); got != "Drama,Crime" {
		t.Errorf("GenreNames() = %q", got)
	}
	// English-first, then highest score: the German backdrop scored 99 but the
	// English one is the one served.
	if got := s.Poster(); got != "https://artworks.thetvdb.com/posters/81189-b.jpg" {
		t.Errorf("Poster() = %q, want the top-scored English poster", got)
	}
	if got := s.Backdrop(); got != "https://artworks.thetvdb.com/backdrops/81189-3.jpg" {
		t.Errorf("Backdrop() = %q, want the English backdrop", got)
	}
	if got := s.Logo(); got != "https://artworks.thetvdb.com/logos/81189.png" {
		t.Errorf("Logo() = %q", got)
	}
	if got := s.Certification(); got != "TV-MA" {
		t.Errorf("Certification() = %q, want the US rating", got)
	}
	if got := s.TrailerURL(); got != "https://youtube.com/en" {
		t.Errorf("TrailerURL() = %q, want the English trailer", got)
	}
	if got := s.TMDBID(); got != 1396 {
		t.Errorf("TMDBID() = %d, want 1396", got)
	}
	if got := s.IMDBID(); got != "tt0903747" {
		t.Errorf("IMDBID() = %q", got)
	}
}

func TestSeriesExtended_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newTestClient(t, srv.URL)

	_, err := c.GetSeriesExtended(context.Background(), 404)
	if !IsNotFound(err) {
		t.Fatalf("error = %v, want a NotFoundError", err)
	}
}

func TestSeriesEpisodes_PaginatesAndJoinsStills(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newTestClient(t, srv.URL)

	eps, err := c.GetSeriesEpisodes(context.Background(), 81189, "", "")
	if err != nil {
		t.Fatalf("GetSeriesEpisodes: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("got %d episodes, want 2 (both pages)", len(eps))
	}
	if got := eps[0].ImageURL(); got != "https://artworks.thetvdb.com/banners/episodes/81189/3859781.jpg" {
		t.Errorf("ImageURL() = %q, want the joined absolute URL", got)
	}
	if eps[0].SeasonNumber != 1 || eps[0].Number != 1 {
		t.Errorf("episode numbering = s%d e%d, want s1 e1", eps[0].SeasonNumber, eps[0].Number)
	}
}

func TestSeriesEpisodes_MissingOrderIsNotAnError(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newTestClient(t, srv.URL)

	eps, err := c.GetSeriesEpisodes(context.Background(), 999, "dvd", "eng")
	if err != nil {
		t.Fatalf("a 404 on an episodes path must be an answer, not an error: %v", err)
	}
	if len(eps) != 0 {
		t.Fatalf("got %d episodes, want none", len(eps))
	}
}

func TestMovieExtended(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newTestClient(t, srv.URL)

	m, err := c.GetMovieExtended(context.Background(), 169)
	if err != nil {
		t.Fatalf("GetMovieExtended: %v", err)
	}
	if m.Name != "The Matrix" || m.Year() != 1999 || m.Runtime != 136 {
		t.Fatalf("movie = %q year %d runtime %d", m.Name, m.Year(), m.Runtime)
	}
	if got := m.ReleasedDate(); got != "1999-03-31" {
		t.Errorf("ReleasedDate() = %q", got)
	}
	if got := m.Poster(); got != "https://artworks.thetvdb.com/movies/169/p.jpg" {
		t.Errorf("Poster() = %q", got)
	}
	if got := m.Certification(); got != "R" {
		t.Errorf("Certification() = %q", got)
	}
	if m.TMDBID() != 603 || m.IMDBID() != "tt0133093" {
		t.Errorf("links = tmdb %d imdb %q", m.TMDBID(), m.IMDBID())
	}

	if _, err := c.GetMovieExtended(context.Background(), 404); !IsNotFound(err) {
		t.Fatalf("error = %v, want a NotFoundError", err)
	}
}

func TestExpiredTokenIsRefreshedOnce(t *testing.T) {
	var logins int32
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&logins, 1)
		_, _ = w.Write([]byte(`{"status":"success","data":{"token":"second-token"}}`))
	})
	mux.HandleFunc("/movies/169/extended", func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"status":"failure"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer second-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"id":169,"name":"The Matrix"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	m, err := c.GetMovieExtended(context.Background(), 169)
	if err != nil {
		t.Fatalf("a 401 must be retried after a fresh login: %v", err)
	}
	if m.Name != "The Matrix" {
		t.Fatalf("movie = %q", m.Name)
	}
	if atomic.LoadInt32(&logins) != 2 {
		t.Fatalf("logged in %d times, want 2 (initial + refresh)", atomic.LoadInt32(&logins))
	}
}

func TestNewRequiresAnAPIKey(t *testing.T) {
	t.Setenv("TVDB_API_KEY", "")
	if _, err := New(); err == nil {
		t.Fatal("New() without TVDB_API_KEY must fail")
	}
	t.Setenv("TVDB_API_KEY", "abc123")
	c, err := New()
	if err != nil {
		t.Fatalf("New() with a key: %v", err)
	}
	if c.baseURL != DefaultBaseURL {
		t.Fatalf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
}

// TestSearch_MultiTypeIssuesOneRequestPerType is the regression test for the
// dead-search bug: TVDB's `type` filter takes a single value, so a combined
// "movie,show" request matched nothing. A multi-type search must issue one
// request per type and merge the pages.
func TestSearch_MultiTypeIssuesOneRequestPerType(t *testing.T) {
	var mu sync.Mutex
	var seenTypes []string

	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"token":"test-token"}}`))
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		searchType := r.URL.Query().Get("type")
		mu.Lock()
		seenTypes = append(seenTypes, searchType)
		mu.Unlock()

		objectID, name, year := "series-81189", "Breaking Bad", "2008"
		if searchType == "movie" {
			objectID, name, year = "movie-169", "The Matrix", "1999"
		}
		_, _ = fmt.Fprintf(w, `{"status":"success","data":[{
			"objectID":%q,"id":%q,"tvdb_id":%q,"name":%q,"type":%q,"year":%q,
			"slug":"x","remote_ids":[]
		}],"links":{"prev":null,"self":"x","next":null,"total_items":1,"page_size":25}}`,
			objectID, objectID, strings.TrimPrefix(objectID, "series-"), name, searchType, year)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	res, err := c.Search(context.Background(), "matrix", []string{"movie", "show"}, 1, 25)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	mu.Lock()
	got := append([]string(nil), seenTypes...)
	mu.Unlock()

	if len(got) != 2 {
		t.Fatalf("a two-type search sent %d requests (%v), want one per type", len(got), got)
	}
	for _, typ := range got {
		if strings.Contains(typ, ",") {
			t.Fatalf("search requested type=%q — TVDB's type filter is single-valued and a list matches nothing", typ)
		}
	}
	if !contains(got, "movie") || !contains(got, "series") {
		t.Fatalf("requests covered %v, want movie and series", got)
	}
	if len(res.Data) != 2 {
		t.Fatalf("merged %d results, want 2 (one movie, one series)", len(res.Data))
	}
	if res.Data[0].MediaType() != "movie" || res.Data[1].MediaType() != "show" {
		t.Fatalf("merged media types = %q/%q, want movie/show", res.Data[0].MediaType(), res.Data[1].MediaType())
	}
	if res.Links.TotalItems != 2 {
		t.Fatalf("TotalItems = %d, want the sum of both pages (2)", res.Links.TotalItems)
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
