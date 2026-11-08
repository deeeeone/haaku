package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/external/tvdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/metadata"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTVDBTestDB builds the media schema the TVDB ingest writes to.
func setupTVDBTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Movie{}, &database.Show{}, &database.Season{},
		&database.Episode{}, &database.ExternalID{},
	); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	return db
}

func sampleMovie() *tvdb.MovieExtended {
	return &tvdb.MovieExtended{
		ID:               169,
		Name:             "The Matrix",
		Slug:             "the-matrix",
		YearString:       "1999",
		Runtime:          136,
		OriginalLanguage: "eng",
		FirstRelease:     &tvdb.Release{Country: "global", Date: "1999-03-31"},
		Status:           tvdb.Status{ID: 5, Name: "Released"},
		Genres: []tvdb.Genre{
			{ID: 2, Name: "Action"},
			{ID: 878, Name: "Science Fiction"},
		},
		RemoteIDs: []tvdb.RemoteID{
			{ID: "tt0133093", SourceName: "IMDB"},
			{ID: "603", SourceName: "TheMovieDB.com"},
		},
		ContentRatings: []tvdb.ContentRating{{Name: "R", Country: "usa"}},
		Artworks: []tvdb.Artwork{
			{Type: tvdb.ArtworkMoviePoster, Language: "eng", Image: "https://artworks.thetvdb.com/movies/169/p.jpg", Score: 10},
			{Type: tvdb.ArtworkMovieBackground, Language: "eng", Image: "https://artworks.thetvdb.com/movies/169/b.jpg", Score: 10},
		},
		OverviewText: "A hacker learns the truth.",
		TaglineText:  "Welcome to the real world.",
	}
}

func sampleShow() *tvdb.SeriesExtended {
	return &tvdb.SeriesExtended{
		ID:               81189,
		Name:             "Breaking Bad",
		Slug:             "breaking-bad",
		YearString:       "2008",
		Overview:         "A chemistry teacher turns to crime.",
		FirstAired:       "2008-01-20",
		LastAired:        "2013-09-29",
		AverageRuntime:   48,
		OriginalLanguage: "eng",
		Status:           tvdb.Status{ID: 2, Name: "Ended"},
		LatestNetwork:    &tvdb.Company{ID: 20, Name: "AMC"},
		Genres:           []tvdb.Genre{{ID: 12, Name: "Drama"}},
		RemoteIDs: []tvdb.RemoteID{
			{ID: "tt0903747", SourceName: "IMDB"},
			{ID: "1396", SourceName: "TheMovieDB.com"},
		},
		Artworks: []tvdb.Artwork{
			{Type: tvdb.ArtworkSeriesPoster, Language: "eng", Image: "https://artworks.thetvdb.com/posters/81189.jpg", Score: 10},
			{Type: tvdb.ArtworkSeriesBackground, Language: "eng", Image: "https://artworks.thetvdb.com/backdrops/81189.jpg", Score: 10},
			{Type: tvdb.ArtworkSeriesIcon, Language: "eng", Image: "https://artworks.thetvdb.com/logos/81189.png", Score: 10},
		},
	}
}

// sampleEpisodes spans two real seasons plus a special (season 0), which must be
// stored but must not count towards the show's totals.
func sampleEpisodes() []tvdb.Episode {
	eps := []tvdb.Episode{
		{ID: 1, SeriesID: 81189, Name: "Pilot", Aired: "2008-01-20", Runtime: 58, Number: 1, AbsoluteNumber: 1, SeasonNumber: 1, Image: "/banners/episodes/81189/1.jpg"},
		{ID: 2, SeriesID: 81189, Name: "Cat's in the Bag...", Aired: "2008-01-27", Runtime: 48, Number: 2, AbsoluteNumber: 2, SeasonNumber: 1},
		{ID: 3, SeriesID: 81189, Name: "No Más", Aired: "2010-03-21", Runtime: 47, Number: 1, AbsoluteNumber: 21, SeasonNumber: 3},
		{ID: 4, SeriesID: 81189, Name: "Good Cop / Bad Cop", Aired: "2009-02-17", Runtime: 3, Number: 1, SeasonNumber: 0},
	}
	return eps
}

func TestIngestTVDBMovie_CreatesWithIDsAndSupplement(t *testing.T) {
	db := setupTVDBTestDB(t)

	supplement := &tmdb.Movie{
		ID: 603, IMDBID: "tt0133093", Title: "The Matrix",
		VoteAverage: 8.2, VoteCount: 25000, Popularity: 45.5,
		PosterPath: "/matrix.jpg", BackdropPath: "/matrix-bd.jpg",
	}
	movie, err := metadata.IngestTVDBMovie(db, sampleMovie(), supplement)
	if err != nil {
		t.Fatalf("IngestTVDBMovie: %v", err)
	}

	if movie.TvdbID != 169 || movie.TmdbID != 603 {
		t.Fatalf("ids = tvdb %d tmdb %d, want 169/603", movie.TvdbID, movie.TmdbID)
	}
	if movie.Title != "The Matrix" || movie.Slug != "the-matrix" {
		t.Fatalf("identity = %q / %q", movie.Title, movie.Slug)
	}
	if movie.Overview != "A hacker learns the truth." {
		t.Errorf("overview = %q, want the TVDB text (primary source)", movie.Overview)
	}
	if movie.Tagline != "Welcome to the real world." {
		t.Errorf("tagline = %q, want the TVDB tagline", movie.Tagline)
	}
	if movie.Runtime != 136 || movie.Certification != "R" || movie.Status != "Released" {
		t.Errorf("runtime/cert/status = %d/%q/%q", movie.Runtime, movie.Certification, movie.Status)
	}
	if got := strings.Join(movie.Genres.Values(), ","); got != "Action,Science Fiction" {
		t.Errorf("genres = %q", got)
	}
	// Supplementary numbers come from TMDB.
	if movie.RatingAvg != 8.2 || movie.RatingCount != 25000 || movie.Popularity != 45.5 {
		t.Errorf("supplement = rating %v (%d) popularity %v", movie.RatingAvg, movie.RatingCount, movie.Popularity)
	}
	if movie.Released == nil || movie.Released.Year() != 1999 {
		t.Errorf("released = %v, want 1999", movie.Released)
	}
	// TVDB artwork is primary; the TMDB path is carried alongside it.
	if movie.Images["poster"] != "https://artworks.thetvdb.com/movies/169/p.jpg" {
		t.Errorf("poster = %v, want the TVDB artwork URL", movie.Images["poster"])
	}
	if movie.Images["poster_path"] != "/matrix.jpg" {
		t.Errorf("poster_path = %v, want the raw TMDB path", movie.Images["poster_path"])
	}

	// Every id the record carries is resolvable from the lookup table.
	for source, want := range map[string]string{"tvdb": "169", "tmdb": "603", "imdb": "tt0133093", "slug": "the-matrix"} {
		var ext database.ExternalID
		if err := db.Where("media_type = ? AND source = ? AND external_id = ?", "movie", source, want).First(&ext).Error; err != nil {
			t.Errorf("external id %s:%s missing: %v", source, want, err)
			continue
		}
		if ext.MediaID != movie.ID {
			t.Errorf("external id %s:%s points at %d, want %d", source, want, ext.MediaID, movie.ID)
		}
	}
}

func TestIngestTVDBMovie_IsIdempotentAndMergesWithTheTMDBRow(t *testing.T) {
	db := setupTVDBTestDB(t)

	// A row the TMDB backfill created first: same movie, tmdb id only.
	legacy := database.Movie{TmdbID: 603, ImdbID: "tt0133093", Title: "The Matrix"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	first, err := metadata.IngestTVDBMovie(db, sampleMovie(), nil)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if first.ID != legacy.ID {
		t.Fatalf("ingest created a duplicate (id %d) instead of merging into %d", first.ID, legacy.ID)
	}

	second, err := metadata.IngestTVDBMovie(db, sampleMovie(), nil)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-ingest changed the row: %d != %d", second.ID, first.ID)
	}

	var count int64
	if err := db.Model(&database.Movie{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("movies in table = %d, want 1", count)
	}
	var extCount int64
	if err := db.Model(&database.ExternalID{}).Count(&extCount).Error; err != nil {
		t.Fatalf("count external ids: %v", err)
	}
	if extCount != 4 {
		t.Fatalf("external ids = %d, want 4 (tvdb, tmdb, imdb, slug — not duplicated)", extCount)
	}
}

func TestIngestTVDBShow_SeasonsAndEpisodes(t *testing.T) {
	db := setupTVDBTestDB(t)

	show, err := metadata.IngestTVDBShow(db, sampleShow(), sampleEpisodes(), nil)
	if err != nil {
		t.Fatalf("IngestTVDBShow: %v", err)
	}

	if show.TvdbID != 81189 || show.Network != "AMC" || show.Status != "Ended" {
		t.Fatalf("show = tvdb %d network %q status %q", show.TvdbID, show.Network, show.Status)
	}
	// Specials (season 0) are stored but excluded from the totals.
	if show.EpisodeCount != 3 || show.SeasonCount != 2 {
		t.Fatalf("counts = %d episodes / %d seasons, want 3/2 (season 0 excluded)", show.EpisodeCount, show.SeasonCount)
	}

	var seasons []database.Season
	if err := db.Where("show_id = ?", show.ID).Order("season_number").Find(&seasons).Error; err != nil {
		t.Fatalf("load seasons: %v", err)
	}
	if len(seasons) != 3 {
		t.Fatalf("season rows = %d, want 3 (0, 1, 3)", len(seasons))
	}
	byNumber := map[int]database.Season{}
	for _, s := range seasons {
		byNumber[s.SeasonNumber] = s
	}
	if byNumber[1].EpisodeCount != 2 || byNumber[3].EpisodeCount != 1 || byNumber[0].EpisodeCount != 1 {
		t.Fatalf("episode counts per season = %d/%d/%d, want 2/1/1",
			byNumber[1].EpisodeCount, byNumber[3].EpisodeCount, byNumber[0].EpisodeCount)
	}

	var episodes []database.Episode
	if err := db.Where("show_id = ?", show.ID).Order("id").Find(&episodes).Error; err != nil {
		t.Fatalf("load episodes: %v", err)
	}
	if len(episodes) != 4 {
		t.Fatalf("episode rows = %d, want 4", len(episodes))
	}
	pilot := episodes[0]
	if pilot.Title != "Pilot" || pilot.EpisodeNumber != 1 || pilot.SeasonID != byNumber[1].ID {
		t.Fatalf("pilot = %+v", pilot)
	}
	if pilot.Images["still"] != tvdb.ArtworkBaseURL+"/banners/episodes/81189/1.jpg" {
		t.Errorf("still = %v, want the absolute artwork URL", pilot.Images["still"])
	}
	if pilot.AbsoluteNumber == nil || *pilot.AbsoluteNumber != 1 {
		t.Errorf("absolute number = %v", pilot.AbsoluteNumber)
	}
	if pilot.AiredAt == nil || pilot.AiredAt.Year() != 2008 {
		t.Errorf("aired = %v, want 2008", pilot.AiredAt)
	}

	// Re-ingesting with a changed episode title updates the row in place.
	updated := sampleEpisodes()
	updated[0].Name = "Pilot (updated)"
	updated[0].Runtime = 59
	if _, err := metadata.IngestTVDBShow(db, sampleShow(), updated, nil); err != nil {
		t.Fatalf("re-ingest: %v", err)
	}
	var reloaded database.Episode
	if err := db.Where("id = ?", pilot.ID).First(&reloaded).Error; err != nil {
		t.Fatalf("reload episode: %v", err)
	}
	if reloaded.Title != "Pilot (updated)" || reloaded.Runtime != 59 {
		t.Fatalf("episode not updated in place: %+v", reloaded)
	}
	var episodeCount int64
	if err := db.Model(&database.Episode{}).Count(&episodeCount).Error; err != nil {
		t.Fatalf("count episodes: %v", err)
	}
	if episodeCount != 4 {
		t.Fatalf("episodes after re-ingest = %d, want 4 (no duplicates)", episodeCount)
	}
}

// --- resolver (ingest on demand, the door the search page uses) ---

// newFakeTVDB serves the TVDB routes the resolver calls and counts them, so a
// test can prove the second resolve of the same title makes no provider call.
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
		payload, _ := json.Marshal(map[string]any{"status": "success", "data": sampleShow()})
		_, _ = w.Write(payload)
	}))
	mux.HandleFunc("/series/81189/episodes/default/eng", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		payload, _ := json.Marshal(map[string]any{
			"status": "success",
			"data":   map[string]any{"episodes": sampleEpisodes()},
			"links":  map[string]any{"next": nil, "total_items": len(sampleEpisodes()), "page_size": 500},
		})
		_, _ = w.Write(payload)
	}))
	mux.HandleFunc("/movies/169/extended", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		payload, _ := json.Marshal(map[string]any{"status": "success", "data": sampleMovie()})
		_, _ = w.Write(payload)
	}))
	mux.HandleFunc("/movies/169/translations/eng", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"name":"The Matrix","overview":"o","tagline":"t","language":"eng"}}`))
	}))
	// TVDB's search type filter is single-valued: a "movie" request and a
	// "series" request are separate calls. The fake answers each one in its own
	// type's terms, the way the real API does.
	mux.HandleFunc("/search", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "series" {
			_, _ = w.Write([]byte(`{"status":"success","data":[],"links":{"next":null,"total_items":0,"page_size":25}}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":[{
			"objectID":"series-81189","id":"series-81189","tvdb_id":"81189","name":"Breaking Bad",
			"type":"series","year":"2008","slug":"breaking-bad",
			"image_url":"https://artworks.thetvdb.com/posters/81189.jpg",
			"overview":"A chemistry teacher turns to crime.",
			"remote_ids":[{"id":"tt0903747","sourceName":"IMDB"},{"id":"1396","sourceName":"TheMovieDB.com"}]},
			{"objectID":"series-1","id":"series-1","tvdb_id":"1","name":"A Person","type":"person","year":""}],
			"links":{"next":null,"total_items":2,"page_size":25}}`))
	}))
	mux.HandleFunc("/series/404/extended", requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":"failure","message":"not found"}`))
	}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := tvdb.NewWithKey("test-key", "")
	client.SetBaseURL(srv.URL)
	return client, &calls
}

func TestResolver_ResolveShowIngestsThenServesFromTheCatalogue(t *testing.T) {
	db := setupTVDBTestDB(t)
	client, calls := newFakeTVDB(t)
	resolver := metadata.NewResolver(db, client, nil)

	ctx := context.Background()
	show, err := resolver.ResolveShowByTVDBID(ctx, 81189)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if show.TvdbID != 81189 || show.EpisodeCount != 3 {
		t.Fatalf("resolved show = %+v", show)
	}
	firstCalls := atomic.LoadInt32(calls)
	if firstCalls == 0 {
		t.Fatal("the first resolve must call the provider")
	}

	again, err := resolver.ResolveShowByTVDBID(ctx, 81189)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if again.ID != show.ID {
		t.Fatalf("second resolve returned media %d, want %d", again.ID, show.ID)
	}
	if got := atomic.LoadInt32(calls); got != firstCalls {
		t.Fatalf("a title already in the catalogue made %d more provider calls", got-firstCalls)
	}
}

func TestResolver_ResolveByExternalIDAndNotFound(t *testing.T) {
	db := setupTVDBTestDB(t)
	client, _ := newFakeTVDB(t)
	resolver := metadata.NewResolver(db, client, nil)
	ctx := context.Background()

	mediaType, id, err := resolver.ResolveByExternalID(ctx, "show", "tvdb", "81189")
	if err != nil {
		t.Fatalf("resolve by tvdb id: %v", err)
	}
	if mediaType != "show" || id == 0 {
		t.Fatalf("resolved %s/%d", mediaType, id)
	}

	// A missing record is TVDB's answer, not a provider failure.
	if _, _, err := resolver.ResolveByExternalID(ctx, "show", "tvdb", "404"); !metadata.IsProviderNotFound(err) {
		t.Fatalf("404 must map to ErrMediaNotFound, got %v", err)
	}
	// A malformed id is a bad request the caller sees as not-found too.
	if _, _, err := resolver.ResolveByExternalID(ctx, "movie", "tvdb", "not-a-number"); !metadata.IsProviderNotFound(err) {
		t.Fatalf("non-numeric tvdb id: got %v", err)
	}
}

func TestResolver_SearchProviderMarksCatalogueMembers(t *testing.T) {
	db := setupTVDBTestDB(t)
	client, _ := newFakeTVDB(t)
	resolver := metadata.NewResolver(db, client, nil)
	ctx := context.Background()

	hits, total, err := resolver.SearchProvider(ctx, "breaking", []string{"show", "movie"}, 1, 25)
	if err != nil {
		t.Fatalf("SearchProvider: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want the provider's total_items (2)", total)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1 (the person result is dropped)", len(hits))
	}
	if hits[0].InCatalog {
		t.Fatal("nothing is in the catalogue yet")
	}
	if hits[0].TVDBID != 81189 || hits[0].TMDBID != 1396 || hits[0].IMDBID != "tt0903747" {
		t.Fatalf("hit ids = %+v", hits[0])
	}

	// Once the title is ingested the same search marks it as held.
	if _, err := resolver.ResolveShowByTVDBID(ctx, 81189); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	hits, _, err = resolver.SearchProvider(ctx, "breaking", []string{"show"}, 1, 25)
	if err != nil {
		t.Fatalf("SearchProvider (after ingest): %v", err)
	}
	if !hits[0].InCatalog || hits[0].MediaID == 0 {
		t.Fatalf("hit after ingest = %+v, want in_catalog with a media id", hits[0])
	}
}

// TestResolver_NoProviderIsUnavailableNotACrash: a resolver without a TVDB
// client must report the absence, which is what the search endpoint degrades on.
func TestResolver_NoProviderIsUnavailable(t *testing.T) {
	resolver := metadata.NewResolver(nil, nil, nil)
	if resolver.Ready() {
		t.Fatal("a resolver without a TVDB client is not ready")
	}
	if _, err := resolver.ResolveShowByTVDBID(context.Background(), 1); !metadata.IsProviderUnavailable(err) {
		t.Fatalf("error = %v, want ErrProviderUnavailable", err)
	}
	if _, _, err := resolver.SearchProvider(context.Background(), "x", []string{"show"}, 1, 10); !metadata.IsProviderUnavailable(err) {
		t.Fatalf("error = %v, want ErrProviderUnavailable", err)
	}
}

// TestNormalizeMediaTypes: types the provider cannot serve (person, list,
// episode) must not turn into a provider call.
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
