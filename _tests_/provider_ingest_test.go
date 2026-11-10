package tests

import (
	"strings"
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
	// MediaModels() is the canonical media schema; the hand-rolled list here
	// went stale the moment ingest started writing movie_releases and
	// media_metadata, and every provider test then failed on a missing table.
	if err := db.AutoMigrate(database.MediaModels()...); err != nil {
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
