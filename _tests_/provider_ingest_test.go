package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/external/tvdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/metadata"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupProviderTestDB builds the media schema the provider ingest writes to.
func setupProviderTestDB(t *testing.T) *gorm.DB {
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

// --- fixtures ---

// sampleMovie is a TMDB movie as GetMovie returns it, appended sub-resources
// included. Its external_ids deliberately carry NO tvdb_id, which is what the
// live API does for movies — the TVDB link has to come from somewhere else.
func sampleMovie() *tmdb.Movie {
	return &tmdb.Movie{
		ID:            603,
		IMDBID:        "tt0133093",
		Title:         "The Matrix",
		OriginalTitle: "The Matrix",
		Tagline:       "Welcome to the real world.",
		Overview:      "A hacker learns the truth.",
		ReleaseDate:   "1999-03-31",
		Runtime:       136,
		Status:        "Released",
		Popularity:    45.5,
		VoteAverage:   8.2,
		VoteCount:     25000,
		Genres: []tmdb.Genre{
			{ID: 28, Name: "Action"},
			{ID: 878, Name: "Science Fiction"},
		},
		Language:     "en",
		PosterPath:   "/matrix.jpg",
		BackdropPath: "/matrix-bd.jpg",
		ExternalIDs:  &tmdb.ExternalIDs{IMDBID: "tt0133093"},
		ReleaseDates: &tmdb.ReleaseDates{Results: []tmdb.CountryReleaseDates{
			{ISO3166: "US", ReleaseDates: []tmdb.ReleaseDate{{Certification: "R", Type: 3}}},
		}},
		Videos: &tmdb.Videos{Results: []tmdb.Video{
			{Site: "YouTube", Type: "Trailer", Key: "FVI84Dfx2-I", Official: true},
		}},
	}
}

// sampleShow is a TMDB show as GetShow returns it. Its external_ids DO name a
// tvdb_id, which is the cheap half of the supplementary link.
func sampleShow() *tmdb.Show {
	return &tmdb.Show{
		ID:            1396,
		IMDBID:        "tt0903747",
		Name:          "Breaking Bad",
		OriginalName:  "Breaking Bad",
		Overview:      "A chemistry teacher turns to crime.",
		FirstAirDate:  "2008-01-20",
		LastAirDate:   "2013-09-29",
		Status:        "Ended",
		Popularity:    120,
		VoteAverage:   8.9,
		VoteCount:     14000,
		Genres:        []tmdb.Genre{{ID: 18, Name: "Drama"}},
		Language:      "en",
		PosterPath:    "/bb.jpg",
		BackdropPath:  "/bb-bd.jpg",
		Networks:      []tmdb.Network{{ID: 174, Name: "AMC"}},
		SeasonsCount:  5,
		EpisodesCount: 62,
		Seasons: []tmdb.SeasonSummary{
			{ID: 1, SeasonNumber: 0, EpisodeCount: 1, AirDate: "2009-02-17"},
			{ID: 2, SeasonNumber: 1, EpisodeCount: 2, AirDate: "2008-01-20", PosterPath: "/bb-s1.jpg"},
			{ID: 3, SeasonNumber: 3, EpisodeCount: 1, AirDate: "2010-03-21"},
		},
		ExternalIDs:    &tmdb.ExternalIDs{IMDBID: "tt0903747", TVDBID: 81189},
		ContentRatings: &tmdb.ContentRatings{Results: []tmdb.ContentRating{{ISO3166: "US", Rating: "TV-MA"}}},
		Videos:         &tmdb.Videos{Results: []tmdb.Video{{Site: "YouTube", Type: "Trailer", Key: "HhesaQXLuRY", Official: true}}},
	}
}

// sampleSeasons is what the resolver's season walk gets back: two real seasons
// plus a special (season 0), which must be stored but must not count towards
// the show's totals.
func sampleSeasons() []tmdb.Season {
	return []tmdb.Season{
		{SeasonNumber: 0, AirDate: "2009-02-17", Episodes: []tmdb.EpisodeDetail{
			{EpisodeNumber: 1, Name: "Good Cop / Bad Cop", AirDate: "2009-02-17", Runtime: 3, StillPath: "/stills/bb-s0e1.jpg"},
		}},
		{SeasonNumber: 1, AirDate: "2008-01-20", PosterPath: "/bb-s1.jpg", Episodes: []tmdb.EpisodeDetail{
			{EpisodeNumber: 1, Name: "Pilot", Overview: "The first one.", AirDate: "2008-01-20", Runtime: 58, StillPath: "/stills/bb-s1e1.jpg"},
			{EpisodeNumber: 2, Name: "Cat's in the Bag...", AirDate: "2008-01-27", Runtime: 48},
		}},
		{SeasonNumber: 3, AirDate: "2010-03-21", Episodes: []tmdb.EpisodeDetail{
			{EpisodeNumber: 1, Name: "No Más", AirDate: "2010-03-21", Runtime: 47},
		}},
	}
}

// sampleMovieSupplement is a TVDB movie record, the SUPPLEMENTARY provider.
func sampleMovieSupplement() *tvdb.MovieExtended {
	return &tvdb.MovieExtended{
		ID:               169,
		Name:             "The Matrix",
		Slug:             "the-matrix",
		YearString:       "1999",
		Runtime:          136,
		OriginalLanguage: "eng",
		FirstRelease:     &tvdb.Release{Country: "global", Date: "1999-03-31"},
		Status:           tvdb.Status{ID: 5, Name: "Released"},
		Genres:           []tvdb.Genre{{ID: 2, Name: "Action"}},
		RemoteIDs: []tvdb.RemoteID{
			{ID: "tt0133093", SourceName: "IMDB"},
			{ID: "603", SourceName: "TheMovieDB.com"},
		},
		ContentRatings: []tvdb.ContentRating{{Name: "R", Country: "usa"}},
		Artworks: []tvdb.Artwork{
			{Type: tvdb.ArtworkMoviePoster, Language: "eng", Image: "https://artworks.thetvdb.com/movies/169/p.jpg", Score: 10},
			{Type: tvdb.ArtworkMovieBackground, Language: "eng", Image: "https://artworks.thetvdb.com/movies/169/b.jpg", Score: 10},
		},
		OverviewText: "TVDB's overview.",
		TaglineText:  "TVDB's tagline.",
	}
}

// sampleShowSupplement is a TVDB series record.
func sampleShowSupplement() *tvdb.SeriesExtended {
	return &tvdb.SeriesExtended{
		ID:               81189,
		Name:             "Breaking Bad",
		Slug:             "breaking-bad",
		YearString:       "2008",
		Overview:         "TVDB's overview.",
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

// --- ingest ---

func TestIngestMovie_TMDBIsTheRecordAndTVDBSupplementsIt(t *testing.T) {
	db := setupProviderTestDB(t)

	movie, err := metadata.IngestMovie(db, sampleMovie(), sampleMovieSupplement())
	if err != nil {
		t.Fatalf("IngestMovie: %v", err)
	}

	if movie.TmdbID != 603 || movie.TvdbID != 169 {
		t.Fatalf("ids = tmdb %d tvdb %d, want 603/169", movie.TmdbID, movie.TvdbID)
	}
	if movie.Title != "The Matrix" || movie.Slug != "the-matrix" {
		t.Fatalf("identity = %q / %q (the slug comes from the TVDB supplement)", movie.Title, movie.Slug)
	}
	// The record's text is TMDB's, even though the supplement carries different
	// wording.
	if movie.Overview != "A hacker learns the truth." {
		t.Errorf("overview = %q, want the TMDB text (primary source)", movie.Overview)
	}
	if movie.Tagline != "Welcome to the real world." {
		t.Errorf("tagline = %q, want the TMDB tagline", movie.Tagline)
	}
	if movie.Runtime != 136 || movie.Certification != "R" || movie.Status != "Released" {
		t.Errorf("runtime/cert/status = %d/%q/%q", movie.Runtime, movie.Certification, movie.Status)
	}
	if movie.TrailerURL != "https://www.youtube.com/watch?v=FVI84Dfx2-I" {
		t.Errorf("trailer = %q, want the TMDB trailer", movie.TrailerURL)
	}
	if got := strings.Join(movie.Genres.Values(), ","); got != "Action,Science Fiction" {
		t.Errorf("genres = %q, want TMDB's two genres", got)
	}
	// The numbers are TMDB's, because TMDB is primary.
	if movie.RatingAvg != 8.2 || movie.RatingCount != 25000 || movie.Popularity != 45.5 {
		t.Errorf("numbers = rating %v (%d) popularity %v", movie.RatingAvg, movie.RatingCount, movie.Popularity)
	}
	if movie.Released == nil || movie.Released.Year() != 1999 {
		t.Errorf("released = %v, want 1999", movie.Released)
	}

	// TMDB artwork is what clients read; the TVDB URLs are kept alongside and
	// are NOT promoted into `poster`, because TMDB had one.
	if movie.Images["poster"] != "https://image.tmdb.org/t/p/w500/matrix.jpg" {
		t.Errorf("poster = %v, want the joined TMDB poster", movie.Images["poster"])
	}
	if movie.Images["poster_path"] != "/matrix.jpg" {
		t.Errorf("poster_path = %v, want the raw TMDB path", movie.Images["poster_path"])
	}
	if movie.Images["tvdb_poster"] != "https://artworks.thetvdb.com/movies/169/p.jpg" {
		t.Errorf("tvdb_poster = %v, want the TVDB artwork kept alongside", movie.Images["tvdb_poster"])
	}

	// Both raw records are auditable from the row.
	if _, ok := movie.Metadata["tmdb"]; !ok {
		t.Error("metadata is missing the tmdb record")
	}
	if _, ok := movie.Metadata["tvdb"]; !ok {
		t.Error("metadata is missing the tvdb record")
	}

	for source, want := range map[string]string{"tmdb": "603", "tvdb": "169", "imdb": "tt0133093", "slug": "the-matrix"} {
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

// TestIngestMovie_FallsBackToTVDBArtwork: TMDB has no poster for this title, so
// the TVDB poster is what clients see. That is the whole point of keeping the
// supplement's artwork.
func TestIngestMovie_FallsBackToTVDBArtwork(t *testing.T) {
	db := setupProviderTestDB(t)

	record := sampleMovie()
	record.PosterPath = ""
	record.BackdropPath = ""

	movie, err := metadata.IngestMovie(db, record, sampleMovieSupplement())
	if err != nil {
		t.Fatalf("IngestMovie: %v", err)
	}
	if movie.Images["poster"] != "https://artworks.thetvdb.com/movies/169/p.jpg" {
		t.Errorf("poster = %v, want the TVDB artwork promoted (TMDB had none)", movie.Images["poster"])
	}
	if movie.Images["backdrop"] != "https://artworks.thetvdb.com/movies/169/b.jpg" {
		t.Errorf("backdrop = %v, want the TVDB artwork promoted", movie.Images["backdrop"])
	}
}

// TestIngestMovie_NoTVDBClientIsComplete pins that TVDB is genuinely optional: a
// TMDB record alone ingests, tvdb_id stays 0, and everything TMDB supplied is
// present.
func TestIngestMovie_NoTVDBClientIsComplete(t *testing.T) {
	db := setupProviderTestDB(t)

	movie, err := metadata.IngestMovie(db, sampleMovie(), nil)
	if err != nil {
		t.Fatalf("IngestMovie: %v", err)
	}
	if movie.TvdbID != 0 {
		t.Errorf("tvdb_id = %d, want 0 with no supplement", movie.TvdbID)
	}
	if movie.Title != "The Matrix" || movie.Certification != "R" {
		t.Errorf("movie = %+v, want a complete TMDB record", movie)
	}
	var extCount int64
	if err := db.Model(&database.ExternalID{}).Count(&extCount).Error; err != nil {
		t.Fatalf("count external ids: %v", err)
	}
	if extCount != 3 {
		t.Errorf("external ids = %d, want 3 (tmdb, imdb, slug)", extCount)
	}
}

// TestIngestMovie_IsIdempotentAndMergesWithALegacyRow: a row the earlier,
// TVDB-primary ingest (or the TMDB backfill) created is UPDATED, not
// duplicated, and a re-ingest changes nothing.
func TestIngestMovie_IsIdempotentAndMergesWithALegacyRow(t *testing.T) {
	db := setupProviderTestDB(t)

	legacy := database.Movie{TmdbID: 603, ImdbID: "tt0133093", Title: "The Matrix"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	first, err := metadata.IngestMovie(db, sampleMovie(), nil)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if first.ID != legacy.ID {
		t.Fatalf("ingest created a duplicate (id %d) instead of merging into %d", first.ID, legacy.ID)
	}

	second, err := metadata.IngestMovie(db, sampleMovie(), nil)
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
	if extCount != 3 {
		t.Fatalf("external ids = %d, want 3 (tmdb, imdb, slug — not duplicated)", extCount)
	}
}

// TestIngestMovie_DerivesASlugWhenTVDBSuppliesNone: TMDB names no slug, so with
// no supplement the row's slug comes from the title. Two titles that slugify the
// same must not steal each other's slug — external_ids has a unique key on
// (media_type, source, external_id) and the ingest upserts it.
func TestIngestMovie_DerivesASlugWhenTVDBSuppliesNone(t *testing.T) {
	db := setupProviderTestDB(t)

	first, err := metadata.IngestMovie(db, sampleMovie(), nil)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if first.Slug != "the-matrix" {
		t.Fatalf("slug = %q, want a slug derived from the title", first.Slug)
	}

	// A different movie with the same title and year.
	clash := sampleMovie()
	clash.ID = 604
	clash.IMDBID = "tt0000002"
	clash.Title = "The Matrix"
	clash.ReleaseDate = "1999-01-01"

	second, err := metadata.IngestMovie(db, clash, nil)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if second.Slug == first.Slug || second.Slug == "" {
		t.Fatalf("second slug = %q, want a distinct non-empty slug (first owns %q)", second.Slug, first.Slug)
	}

	// The first row still owns its slug.
	var ext database.ExternalID
	if err := db.Where("media_type = ? AND source = ? AND external_id = ?", "movie", "slug", "the-matrix").First(&ext).Error; err != nil {
		t.Fatalf("first slug lookup gone: %v", err)
	}
	if ext.MediaID != first.ID {
		t.Fatalf("slug 'the-matrix' now points at %d, want %d", ext.MediaID, first.ID)
	}

	// A re-ingest keeps the row's own derived slug rather than deriving again.
	again, err := metadata.IngestMovie(db, clash, nil)
	if err != nil {
		t.Fatalf("third ingest: %v", err)
	}
	if again.Slug != second.Slug {
		t.Fatalf("re-ingest changed the slug: %q -> %q", second.Slug, again.Slug)
	}
}

func TestIngestShow_SeasonsAndEpisodes(t *testing.T) {
	db := setupProviderTestDB(t)

	show, err := metadata.IngestShow(db, sampleShow(), sampleSeasons(), sampleShowSupplement())
	if err != nil {
		t.Fatalf("IngestShow: %v", err)
	}

	if show.TmdbID != 1396 || show.TvdbID != 81189 {
		t.Fatalf("ids = tmdb %d tvdb %d, want 1396/81189", show.TmdbID, show.TvdbID)
	}
	if show.Network != "AMC" || show.Status != "Ended" {
		t.Fatalf("network/status = %q/%q", show.Network, show.Status)
	}
	if show.Certification != "TV-MA" {
		t.Errorf("certification = %q, want TMDB's content rating", show.Certification)
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
	if byNumber[1].Images["poster"] != "https://image.tmdb.org/t/p/w500/bb-s1.jpg" {
		t.Errorf("season 1 poster = %v, want the joined TMDB poster", byNumber[1].Images["poster"])
	}

	var episodes []database.Episode
	if err := db.Where("show_id = ?", show.ID).Order("id").Find(&episodes).Error; err != nil {
		t.Fatalf("load episodes: %v", err)
	}
	if len(episodes) != 4 {
		t.Fatalf("episode rows = %d, want 4", len(episodes))
	}
	var pilot database.Episode
	for _, episode := range episodes {
		if episode.Title == "Pilot" {
			pilot = episode
		}
	}
	if pilot.ID == 0 {
		t.Fatalf("no Pilot episode among %d rows: %+v", len(episodes), episodes)
	}
	if pilot.EpisodeNumber != 1 || pilot.SeasonID != byNumber[1].ID {
		t.Fatalf("pilot = %+v", pilot)
	}
	if pilot.Images["still"] != "https://image.tmdb.org/t/p/w300/stills/bb-s1e1.jpg" {
		t.Errorf("still = %v, want the joined TMDB still", pilot.Images["still"])
	}
	if pilot.Images["poster"] != pilot.Images["still"] {
		t.Errorf("episode poster = %v, want the still (the web app reads images.poster)", pilot.Images["poster"])
	}
	if pilot.AiredAt == nil || pilot.AiredAt.Year() != 2008 {
		t.Errorf("aired = %v, want 2008", pilot.AiredAt)
	}

	// Re-ingesting with a changed episode title updates the row in place.
	updated := sampleSeasons()
	updated[1].Episodes[0].Name = "Pilot (updated)"
	updated[1].Episodes[0].Runtime = 59
	if _, err := metadata.IngestShow(db, sampleShow(), updated, sampleShowSupplement()); err != nil {
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

// TestIngestShow_CountsFallBackToTMCBbutNeverLie: with no season detail (the
// change-feed path) the counts are TMDB's own totals; with a partial season list
// they are what was actually stored.
func TestIngestShow_CountsFallBackAndThenDerive(t *testing.T) {
	db := setupProviderTestDB(t)

	show, err := metadata.IngestShow(db, sampleShow(), nil, nil)
	if err != nil {
		t.Fatalf("IngestShow: %v", err)
	}
	if show.EpisodeCount != 62 || show.SeasonCount != 5 {
		t.Fatalf("counts = %d/%d, want TMDB's own totals 62/5 when no season detail was fetched",
			show.EpisodeCount, show.SeasonCount)
	}

	show, err = metadata.IngestShow(db, sampleShow(), sampleSeasons(), nil)
	if err != nil {
		t.Fatalf("IngestShow with seasons: %v", err)
	}
	if show.EpisodeCount != 3 || show.SeasonCount != 2 {
		t.Fatalf("counts = %d/%d, want the stored 3/2 so the totals agree with the season rows",
			show.EpisodeCount, show.SeasonCount)
	}
}

// TestIngestShow_ReingestKeepsSeasonArtwork pins the branch that db.Save makes
// dangerous: a season whose incoming summary carries no poster must keep the one
// already stored instead of blanking it.
func TestIngestShow_ReingestKeepsSeasonArtwork(t *testing.T) {
	db := setupProviderTestDB(t)

	show, err := metadata.IngestShow(db, sampleShow(), sampleSeasons(), nil)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", show.ID, 3).First(&season).Error; err != nil {
		t.Fatalf("load season 3: %v", err)
	}
	aired := database.Date{Time: time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)}
	season.Images = database.JSONMap{"poster": "https://example.test/season3.jpg"}
	season.AiredAt = &aired
	if err := db.Save(&season).Error; err != nil {
		t.Fatalf("seed season artwork: %v", err)
	}

	// A re-ingest whose season 3 comes back with no poster and no air date.
	thin := sampleSeasons()
	thin[2].PosterPath = ""
	thin[2].AirDate = ""
	if _, err := metadata.IngestShow(db, sampleShow(), thin, nil); err != nil {
		t.Fatalf("re-ingest: %v", err)
	}

	var reloaded database.Season
	if err := db.Where("show_id = ? AND season_number = ?", show.ID, 3).First(&reloaded).Error; err != nil {
		t.Fatalf("reload season 3: %v", err)
	}
	if got, _ := reloaded.Images["poster"].(string); got != "https://example.test/season3.jpg" {
		t.Errorf("season poster = %v, want it kept across re-ingest", reloaded.Images["poster"])
	}
	if reloaded.AiredAt == nil || !reloaded.AiredAt.Equal(aired.Time) {
		t.Errorf("season aired_at = %v, want %v kept", reloaded.AiredAt, aired.Time)
	}
}

// TestIngestMovie_ReingestKeepsTVDBTextAndMetadata pins the merge rules: db.Save
// writes every column, so a TMDB refresh that came back with an empty tagline
// would blank what an earlier ingest stored, and the namespaced metadata blob
// would lose its tvdb half.
func TestIngestMovie_ReingestKeepsTVDBTextAndMetadata(t *testing.T) {
	db := setupProviderTestDB(t)

	legacy := database.Movie{
		TmdbID:        603,
		TvdbID:        169,
		ImdbID:        "tt0133093",
		Title:         "The Matrix",
		Overview:      "TVDB's overview",
		Tagline:       "TVDB's tagline",
		OriginalTitle: "The Matrix (original)",
		Certification: "R",
		Metadata:      database.JSONMap{"tvdb": map[string]any{"id": 169}},
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	record := sampleMovie()
	record.Overview = ""
	record.Tagline = ""
	record.OriginalTitle = ""
	record.ReleaseDates = nil

	ingested, err := metadata.IngestMovie(db, record, nil)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if ingested.ID != legacy.ID {
		t.Fatalf("ingest did not merge into the legacy row (%d != %d)", ingested.ID, legacy.ID)
	}

	var stored database.Movie
	if err := db.First(&stored, legacy.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.Overview != "TVDB's overview" {
		t.Errorf("overview = %q, want the earlier value kept", stored.Overview)
	}
	if stored.Tagline != "TVDB's tagline" {
		t.Errorf("tagline = %q, want the earlier value kept", stored.Tagline)
	}
	if stored.OriginalTitle != "The Matrix (original)" {
		t.Errorf("original title = %q, want the earlier value kept", stored.OriginalTitle)
	}
	if stored.Certification != "R" {
		t.Errorf("certification = %q, want the earlier value kept", stored.Certification)
	}
	if _, ok := stored.Metadata["tvdb"]; !ok {
		t.Error("metadata lost its tvdb half on re-ingest")
	}
	if _, ok := stored.Metadata["tmdb"]; !ok {
		t.Error("metadata is missing the tmdb record")
	}
}

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
