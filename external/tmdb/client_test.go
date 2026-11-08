package tmdb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestClientAddressesV3 is the regression test for the bug that made the whole
// TMDB path dead: the client addressed api.themoviedb.org/4, where every detail
// route this package uses answers 404 (measured: /4/movie/603, /4/tv/1396,
// /4/search/multi, /4/configuration) while /3 answers 200 in each case.
//
// It fails if the base URL ever moves back to a version whose routes this
// package needs, and it pins the paths the methods build.
func TestClientAddressesV3(t *testing.T) {
	if !strings.HasSuffix(v3BaseURL, "/3") {
		t.Fatalf("v3BaseURL = %q, want the /3 root", v3BaseURL)
	}

	// New() reads the token from the environment.
	t.Setenv("TMDB_API_KEY", "test-token")
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.baseURL != v3BaseURL {
		t.Fatalf("client base URL = %q, want %q", c.baseURL, v3BaseURL)
	}
}

// TestNewTokenEnv pins that New reads TMDB_API_KEY and nothing else.
func TestNewTokenEnv(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		t.Setenv("TMDB_API_KEY", "canonical-key")
		c, err := New()
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if c.token != "canonical-key" {
			t.Fatalf("token = %q, want the key", c.token)
		}
	})

	t.Run("missing", func(t *testing.T) {
		t.Setenv("TMDB_API_KEY", "")
		if _, err := New(); err == nil {
			t.Fatal("New without a token must fail")
		}
	})
}

// TestRequestPaths pins the endpoints the ingest depends on. A wrong path here
// is a silent metadata hole rather than a visible failure, which is exactly how
// the /4 bug survived: the caller logged a debug line and carried on.
func TestRequestPaths(t *testing.T) {
	var (
		mu    sync.Mutex
		paths []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/movie/603"):
			_, _ = w.Write([]byte(`{"id":603,"title":"The Matrix","vote_average":8.2,"vote_count":25000}`))
		case strings.HasPrefix(r.URL.Path, "/tv/1396"):
			_, _ = w.Write([]byte(`{"id":1396,"name":"Breaking Bad","vote_average":8.9,"vote_count":14000}`))
		case strings.HasPrefix(r.URL.Path, "/search/multi"):
			_, _ = w.Write([]byte(`{"page":1,"results":[{"id":603,"media_type":"movie"}]}`))
		case strings.HasPrefix(r.URL.Path, "/configuration"):
			_, _ = w.Write([]byte(`{"images":{"secure_base_url":"https://image.tmdb.org/t/p/"}}`))
		case strings.HasPrefix(r.URL.Path, "/tv/popular"):
			_, _ = w.Write([]byte(`{"page":1,"results":[]}`))
		case strings.HasPrefix(r.URL.Path, "/find/81189"):
			_, _ = w.Write([]byte(`{"tv_results":[{"id":1396,"name":"Breaking Bad"}]}`))
		default:
			t.Logf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status_code":34,"status_message":"The resource you requested could not be found."}`))
		}
	}))
	defer srv.Close()

	t.Setenv("TMDB_API_KEY", "test-token")
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetBaseURL(srv.URL)

	movie, err := c.GetMovie(603)
	if err != nil || movie.ID != 603 || movie.VoteAverage != 8.2 {
		t.Fatalf("GetMovie: %+v, %v", movie, err)
	}
	show, err := c.GetShow(1396)
	if err != nil || show.ID != 1396 || show.VoteAverage != 8.9 {
		t.Fatalf("GetShow: %+v, %v", show, err)
	}
	if _, err := c.SearchMulti("matrix", 1); err != nil {
		t.Fatalf("SearchMulti: %v", err)
	}
	if _, err := c.GetConfiguration(); err != nil {
		t.Fatalf("GetConfiguration: %v", err)
	}
	if _, err := c.TVPopular(1); err != nil {
		t.Fatalf("TVPopular: %v", err)
	}
	tmdbID, err := c.ShowIDByTVDBID(81189)
	if err != nil || tmdbID != 1396 {
		t.Fatalf("ShowIDByTVDBID = %d, %v", tmdbID, err)
	}

	mu.Lock()
	defer mu.Unlock()
	wantPaths := []string{
		"/movie/603", "/tv/1396", "/search/multi", "/configuration", "/tv/popular", "/find/81189",
	}
	if len(paths) != len(wantPaths) {
		t.Fatalf("requested %d paths, want %d: %v", len(paths), len(wantPaths), paths)
	}
	for i, want := range wantPaths {
		if paths[i] != want {
			t.Errorf("request %d hit %q, want %q", i, paths[i], want)
		}
	}
}

// TestImageURL covers the joining rule the ingest stores in images.poster: a
// TMDB path becomes an absolute CDN URL, and anything already absolute (a TVDB
// artwork URL) passes through untouched.
func TestImageURL(t *testing.T) {
	cases := map[string]string{
		"/abc.jpg":                           "https://image.tmdb.org/t/p/w500/abc.jpg",
		"abc.jpg":                            "https://image.tmdb.org/t/p/w500/abc.jpg",
		"https://artworks.thetvdb.com/x.jpg": "https://artworks.thetvdb.com/x.jpg",
		"":                                   "",
	}
	for in, want := range cases {
		if got := ImageURL(in, ImageSizePoster); got != want {
			t.Errorf("ImageURL(%q) = %q, want %q", in, got, want)
		}
	}
	if got := ImageURL("/abc.jpg", ImageSizeBackdrop); got != "https://image.tmdb.org/t/p/w1280/abc.jpg" {
		t.Errorf("backdrop size not applied: %q", got)
	}
	if got := ImageURL("/abc.jpg", ""); got != "https://image.tmdb.org/t/p/w500/abc.jpg" {
		t.Errorf("empty size must default to the poster size: %q", got)
	}
}
