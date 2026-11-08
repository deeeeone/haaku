package tests

// The provider fakes and the resolver/search tests: the doors the search page
// uses. Split out of provider_ingest_test.go so each file stays inside the
// repo's size ratchet.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/external/tvdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/metadata"
)

// --- the fake providers ---

// fakeTMDB serves the TMDB v3 routes the resolver walks, and counts the calls
// so a test can prove a title already in the catalogue makes none.
type fakeTMDB struct {
	client *tmdb.Client
	calls  *int32
}

func newFakeTMDBResolver(t *testing.T) fakeTMDB {
	t.Helper()
	var calls int32

	mux := http.NewServeMux()
	requireAuth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			atomic.AddInt32(&calls, 1)
			next(w, r)
		}
	}
	write := func(w http.ResponseWriter, payload any) {
		b, _ := json.Marshal(payload)
		_, _ = w.Write(b)
	}

	// http.ServeMux matches on the path only, so the append_to_response query
	// these handlers receive does not affect the match.
	mux.HandleFunc("/movie/603", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, sampleMovie())
	}))
	mux.HandleFunc("/movie/999", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status_code":34,"status_message":"not found"}`))
	}))
	mux.HandleFunc("/tv/1396", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, sampleShow())
	}))
	for _, season := range sampleSeasons() {
		mux.HandleFunc(fmt.Sprintf("/tv/1396/season/%d", season.SeasonNumber), requireAuth(func(w http.ResponseWriter, _ *http.Request) {
			write(w, season)
		}))
	}
	mux.HandleFunc("/search/multi", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{
			"page": 1, "total_pages": 1, "total_results": 3,
			"results": []map[string]any{
				{"id": 1396, "media_type": "tv", "name": "Breaking Bad", "first_air_date": "2008-01-20", "overview": "A chemistry teacher turns to crime.", "poster_path": "/bb.jpg"},
				{"id": 603, "media_type": "movie", "title": "The Matrix", "release_date": "1999-03-31", "poster_path": "/matrix.jpg"},
				{"id": 1234, "media_type": "person", "name": "A Person"},
			},
		})
	}))
	mux.HandleFunc("/search/tv", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{
			"page": 1, "total_pages": 1, "total_results": 1,
			"results": []map[string]any{
				{"id": 1396, "name": "Breaking Bad", "first_air_date": "2008-01-20", "poster_path": "/bb.jpg"},
			},
		})
	}))
	mux.HandleFunc("/find/tt0133093", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"movie_results": []map[string]any{{"id": 603, "title": "The Matrix"}}, "tv_results": []any{}})
	}))
	mux.HandleFunc("/find/81189", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"movie_results": []any{}, "tv_results": []map[string]any{{"id": 1396, "name": "Breaking Bad"}}})
	}))
	mux.HandleFunc("/", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("unexpected TMDB path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Setenv("TMDB_API_KEY", "test-token")
	client, err := tmdb.New()
	if err != nil {
		t.Fatalf("tmdb.New: %v", err)
	}
	client.SetBaseURL(srv.URL)
	return fakeTMDB{client: client, calls: &calls}
}

// newFakeTVDB serves the TVDB routes the SUPPLEMENT path calls.
func newFakeTVDB(t *testing.T) (*tvdb.Client, *int32) {
	t.Helper()
	var calls int32

	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"token":"tok"}}`))
	})
	requireAuth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			atomic.AddInt32(&calls, 1)
			next(w, r)
		}
	}

	mux.HandleFunc("/series/81189/extended", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		payload, _ := json.Marshal(map[string]any{"status": "success", "data": sampleShowSupplement()})
		_, _ = w.Write(payload)
	}))
	mux.HandleFunc("/movies/169/extended", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		payload, _ := json.Marshal(map[string]any{"status": "success", "data": sampleMovieSupplement()})
		_, _ = w.Write(payload)
	}))
	mux.HandleFunc("/movies/169/translations/eng", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"name":"The Matrix","overview":"o","tagline":"t","language":"eng"}}`))
	}))
	// The reverse link for a MOVIE: TMDB's movie external-ids payload carries
	// no tvdb_id, so the resolver asks TVDB which of its records has the imdb
	// id (and, in the second case, the tmdb id).
	mux.HandleFunc("/search/remoteid/tt0133093", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":[{"movie":{"id":169,"name":"The Matrix","slug":"the-matrix","year":"1999"}}]}`))
	}))
	mux.HandleFunc("/search/remoteid/603", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":[{"movie":{"id":169,"name":"The Matrix","slug":"the-matrix","year":"1999"}}]}`))
	}))
	mux.HandleFunc("/", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("unexpected TVDB path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":"failure","message":"not found"}`))
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := tvdb.NewWithKey("test-key", "")
	client.SetBaseURL(srv.URL)
	return client, &calls
}

// --- resolver (ingest on demand, the door the search page uses) ---

func TestResolver_ResolveShowIngestsThenServesFromTheCatalogue(t *testing.T) {
	db := setupProviderTestDB(t)
	tmdbServer := newFakeTMDBResolver(t)
	tvdbClient, _ := newFakeTVDB(t)
	resolver := metadata.NewResolver(db, tmdbServer.client, tvdbClient)

	ctx := context.Background()
	show, err := resolver.ResolveShowByTMDBID(ctx, 1396)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if show.TmdbID != 1396 || show.EpisodeCount != 3 {
		t.Fatalf("resolved show = %+v", show)
	}
	// The TVDB supplement supplied the link and the tvdb-only bits.
	if show.TvdbID != 81189 {
		t.Errorf("tvdb_id = %d, want 81189 from the supplement", show.TvdbID)
	}
	if show.Slug != "breaking-bad" {
		t.Errorf("slug = %q, want the TVDB slug", show.Slug)
	}
	firstCalls := atomic.LoadInt32(tmdbServer.calls)
	if firstCalls == 0 {
		t.Fatal("the first resolve must call the provider")
	}

	again, err := resolver.ResolveShowByTMDBID(ctx, 1396)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if again.ID != show.ID {
		t.Fatalf("second resolve returned media %d, want %d", again.ID, show.ID)
	}
	if got := atomic.LoadInt32(tmdbServer.calls); got != firstCalls {
		t.Fatalf("a title already in the catalogue made %d more provider calls", got-firstCalls)
	}
}

func TestResolver_ResolveMovieAttachesTheTVDBSupplement(t *testing.T) {
	db := setupProviderTestDB(t)
	tmdbServer := newFakeTMDBResolver(t)
	tvdbClient, tvdbCalls := newFakeTVDB(t)
	resolver := metadata.NewResolver(db, tmdbServer.client, tvdbClient)

	movie, err := resolver.ResolveMovieByTMDBID(context.Background(), 603)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if movie.TmdbID != 603 || movie.TvdbID != 169 {
		t.Fatalf("ids = %d/%d, want 603/169 (the link comes from TVDB's remote-id search)", movie.TmdbID, movie.TvdbID)
	}
	if atomic.LoadInt32(tvdbCalls) == 0 {
		t.Fatal("the movie resolve should have consulted TVDB for the supplement")
	}
	if movie.Slug != "the-matrix" {
		t.Errorf("slug = %q, want the TVDB slug", movie.Slug)
	}
	// TMDB's own values still win where both providers answered.
	if movie.TrailerURL != "https://www.youtube.com/watch?v=FVI84Dfx2-I" {
		t.Errorf("trailer = %q, want TMDB's trailer over TVDB's", movie.TrailerURL)
	}
}

// TestResolver_SupplementFailureIsNotAFailure: TVDB is supplementary, so a TVDB
// outage (or a project without a TVDB key at all) must not stop a resolve.
func TestResolver_SupplementFailureIsNotAFailure(t *testing.T) {
	db := setupProviderTestDB(t)
	tmdbServer := newFakeTMDBResolver(t)

	// A TVDB client pointed at a server that answers nothing.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(dead.Close)
	deadTVDB := tvdb.NewWithKey("test-key", "")
	deadTVDB.SetBaseURL(dead.URL)

	resolver := metadata.NewResolver(db, tmdbServer.client, deadTVDB)
	movie, err := resolver.ResolveMovieByTMDBID(context.Background(), 603)
	if err != nil {
		t.Fatalf("resolve must survive a TVDB failure: %v", err)
	}
	if movie.TmdbID != 603 || movie.TvdbID != 0 {
		t.Fatalf("movie = tmdb %d tvdb %d, want 603/0", movie.TmdbID, movie.TvdbID)
	}

	// And a resolver with no TVDB client at all behaves the same way.
	bare := metadata.NewResolver(db, tmdbServer.client, nil)
	if _, err := bare.ResolveMovieByTMDBID(context.Background(), 603); err != nil {
		t.Fatalf("resolve with no TVDB client: %v", err)
	}
}

func TestResolver_ResolveByExternalIDAndNotFound(t *testing.T) {
	db := setupProviderTestDB(t)
	tmdbServer := newFakeTMDBResolver(t)
	tvdbClient, _ := newFakeTVDB(t)
	resolver := metadata.NewResolver(db, tmdbServer.client, tvdbClient)
	ctx := context.Background()

	// A tmdb id, the default.
	if mediaType, id, err := resolver.ResolveByExternalID(ctx, "movie", "tmdb", "603"); err != nil || mediaType != "movie" || id == 0 {
		t.Fatalf("resolve by tmdb id = %s/%d, %v", mediaType, id, err)
	}
	// An imdb id goes through TMDB's /find.
	if mediaType, id, err := resolver.ResolveByExternalID(ctx, "movie", "imdb", "tt0133093"); err != nil || mediaType != "movie" || id == 0 {
		t.Fatalf("resolve by imdb id = %s/%d, %v", mediaType, id, err)
	}
	// And a tvdb id is still accepted — media servers speak them.
	if mediaType, id, err := resolver.ResolveByExternalID(ctx, "show", "tvdb", "81189"); err != nil || mediaType != "show" || id == 0 {
		t.Fatalf("resolve by tvdb id = %s/%d, %v", mediaType, id, err)
	}

	// A missing record is the provider's answer, not a provider failure.
	if _, _, err := resolver.ResolveByExternalID(ctx, "movie", "tmdb", "999"); !metadata.IsProviderNotFound(err) {
		t.Fatalf("404 must map to ErrMediaNotFound, got %v", err)
	}
	// A malformed id is a bad request the caller sees as not-found too.
	if _, _, err := resolver.ResolveByExternalID(ctx, "movie", "tmdb", "not-a-number"); !metadata.IsProviderNotFound(err) {
		t.Fatalf("non-numeric tmdb id: got %v", err)
	}
	// An id no provider carries is not-found as well.
	if _, _, err := resolver.ResolveByExternalID(ctx, "movie", "imdb", "tt0000000"); !metadata.IsProviderNotFound(err) {
		t.Fatalf("unknown imdb id: got %v", err)
	}
}

func TestResolver_SearchProviderMarksCatalogueMembers(t *testing.T) {
	db := setupProviderTestDB(t)
	tmdbServer := newFakeTMDBResolver(t)
	resolver := metadata.NewResolver(db, tmdbServer.client, nil)
	ctx := context.Background()

	hits, total, err := resolver.SearchProvider(ctx, "breaking", []string{"show", "movie"}, 1, 25)
	if err != nil {
		t.Fatalf("SearchProvider: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want the provider's total_results (3)", total)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2 (the person result is dropped)", len(hits))
	}
	if hits[0].InCatalog {
		t.Fatal("nothing is in the catalogue yet")
	}
	if hits[0].TMDBID != 1396 || hits[0].MediaType != "show" {
		t.Fatalf("show hit = %+v", hits[0])
	}
	if hits[0].PosterURL != "https://image.tmdb.org/t/p/w500/bb.jpg" {
		t.Errorf("poster = %q, want the joined TMDB poster", hits[0].PosterURL)
	}
	if hits[1].TMDBID != 603 || hits[1].MediaType != "movie" {
		t.Fatalf("movie hit = %+v", hits[1])
	}

	// Once the title is ingested the same search marks it as held.
	if _, err := resolver.ResolveShowByTMDBID(ctx, 1396); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	hits, _, err = resolver.SearchProvider(ctx, "breaking", []string{"show"}, 1, 25)
	if err != nil {
		t.Fatalf("SearchProvider (after ingest): %v", err)
	}
	if len(hits) == 0 || !hits[0].InCatalog || hits[0].MediaID == 0 {
		t.Fatalf("hits after ingest = %+v, want in_catalog with a media id", hits)
	}
}

// TestResolver_SingleTypeSearchUsesTheTypeScopedEndpoint: /search/multi takes no
// type filter, so a one-type search must not be answered from it (a client would
// be told a short page was full).
func TestResolver_SingleTypeSearchUsesTheTypeScopedEndpoint(t *testing.T) {
	db := setupProviderTestDB(t)
	tmdbServer := newFakeTMDBResolver(t)
	resolver := metadata.NewResolver(db, tmdbServer.client, nil)

	hits, total, err := resolver.SearchProvider(context.Background(), "breaking", []string{"show"}, 1, 25)
	if err != nil {
		t.Fatalf("SearchProvider: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want the /search/tv total (1)", total)
	}
	if len(hits) != 1 || hits[0].MediaType != "show" {
		t.Fatalf("hits = %+v, want one show", hits)
	}
}

// TestResolver_NoProviderIsUnavailableNotACrash: a resolver without a TMDB
// client must report the absence, which is what the search endpoint degrades on.
func TestResolver_NoProviderIsUnavailable(t *testing.T) {
	resolver := metadata.NewResolver(nil, nil, nil)
	if resolver.Ready() {
		t.Fatal("a resolver without a TMDB client is not ready")
	}
	if _, err := resolver.ResolveShowByTMDBID(context.Background(), 1); !metadata.IsProviderUnavailable(err) {
		t.Fatalf("error = %v, want ErrProviderUnavailable", err)
	}
	if _, _, err := resolver.SearchProvider(context.Background(), "x", []string{"show"}, 1, 10); !metadata.IsProviderUnavailable(err) {
		t.Fatalf("error = %v, want ErrProviderUnavailable", err)
	}
}

// TestResolver_APrimaryProviderOutageIsReported: TMDB unreachable is a provider
// failure, not a 404 — the two map to different status codes.
func TestResolver_APrimaryProviderOutageIsReported(t *testing.T) {
	db := setupProviderTestDB(t)
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(dead.Close)

	t.Setenv("TMDB_API_KEY", "test-token")
	client, err := tmdb.New()
	if err != nil {
		t.Fatalf("tmdb.New: %v", err)
	}
	client.SetBaseURL(dead.URL)

	resolver := metadata.NewResolver(db, client, nil)
	if _, err := resolver.ResolveMovieByTMDBID(context.Background(), 603); !metadata.IsProviderUnavailable(err) {
		t.Fatalf("error = %v, want ErrProviderUnavailable", err)
	}
}

// TestNormalizeMediaTypes: types TMDB's search is not wired for here (person,
// list, episode) must not turn into a provider call.
func TestNormalizeMediaTypes(t *testing.T) {
	cases := []struct {
		in        []string
		want      []string
		supported bool
	}{
		{[]string{"movie", "show"}, []string{"movie", "show"}, true},
		{[]string{"movie", "person"}, []string{"movie"}, true},
		{[]string{"person", "list"}, nil, false},
		{[]string{"episode"}, nil, false},
		{nil, nil, false},
	}
	for _, c := range cases {
		got, ok := metadata.NormalizeMediaTypes(c.in)
		if ok != c.supported || fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("NormalizeMediaTypes(%v) = %v/%v, want %v/%v", c.in, got, ok, c.want, c.supported)
		}
	}
}

// TestIngestMovie_ExternalIDsAreResolvableByEverySource is the paired half of
// the upsert: an external id written for a title must resolve back to that
// title through database.ResolveIDRefs, whichever id a client sends.
func TestIngestMovie_ExternalIDsAreResolvableByEverySource(t *testing.T) {
	db := setupProviderTestDB(t)

	movie, err := metadata.IngestMovie(db, sampleMovie(), sampleMovieSupplement())
	if err != nil {
		t.Fatalf("IngestMovie: %v", err)
	}

	refs := []database.IDRef{
		{TMDB: 603, MediaType: "movie"},
		{TVDB: 169, MediaType: "movie"},
		{IMDB: "tt0133093", MediaType: "movie"},
	}
	resolved, err := database.ResolveIDRefs(db, refs)
	if err != nil {
		t.Fatalf("ResolveIDRefs: %v", err)
	}
	for _, ref := range refs {
		got, ok := resolved[ref]
		if !ok {
			t.Errorf("ref %+v did not resolve", ref)
			continue
		}
		if got.MediaID != movie.ID {
			t.Errorf("ref %+v resolved to %d, want %d", ref, got.MediaID, movie.ID)
		}
	}
}

// TestLookupMediaByTMDBIDsIsTheSearchMembershipCheck covers the helper the live
// search annotates its page with.
func TestLookupMediaByTMDBIDsIsTheSearchMembershipCheck(t *testing.T) {
	db := setupProviderTestDB(t)

	show, err := metadata.IngestShow(db, sampleShow(), nil, nil)
	if err != nil {
		t.Fatalf("IngestShow: %v", err)
	}

	found, err := database.LookupMediaByTMDBIDs(db, "show", []int{1396, 999})
	if err != nil {
		t.Fatalf("LookupMediaByTMDBIDs: %v", err)
	}
	if found[1396] != show.ID {
		t.Errorf("lookup = %v, want 1396 -> %d", found, show.ID)
	}
	if _, ok := found[999]; ok {
		t.Errorf("lookup returned an id for a title that is not held: %v", found)
	}
	if got, err := database.LookupMediaByTMDBIDs(db, "person", []int{1}); err != nil || len(got) != 0 {
		t.Errorf("a type with no media table = %v, %v; want empty and no error", got, err)
	}
}

// TestSlugIsStableAcrossASupplementArriving: the first ingest has no TVDB slug,
// so the row derives one; a later ingest WITH the supplement must not rewrite
// the URL a client may already hold.
func TestSlugIsStableWhenTheSupplementArrivesLater(t *testing.T) {
	db := setupProviderTestDB(t)

	first, err := metadata.IngestMovie(db, sampleMovie(), nil)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if first.Slug != "the-matrix" {
		t.Fatalf("derived slug = %q", first.Slug)
	}

	// The TVDB supplement supplies the same slug here, so nothing changes; the
	// assertion that matters is that the row is not re-pointed or duplicated.
	second, err := metadata.IngestMovie(db, sampleMovie(), sampleMovieSupplement())
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if second.ID != first.ID || second.Slug != "the-matrix" {
		t.Fatalf("row = %d/%q, want %d/the-matrix", second.ID, second.Slug, first.ID)
	}

	var rows []database.ExternalID
	if err := db.Where("media_type = ? AND source = ?", "movie", "slug").Find(&rows).Error; err != nil {
		t.Fatalf("load slug rows: %v", err)
	}
	if len(rows) != 1 || rows[0].ExternalID != "the-matrix" {
		t.Fatalf("slug rows = %+v, want exactly one 'the-matrix'", rows)
	}
	if n, _ := strconv.Atoi(rows[0].ExternalID); n != 0 {
		t.Fatalf("slug row is not a slug: %+v", rows[0])
	}
}
