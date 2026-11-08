package database

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupLookupDB builds an in-memory schema for the resolution tests: the media
// tables plus the lookup table they are joined through.
func setupLookupDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&Movie{}, &Show{}, &Season{}, &ExternalID{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestResolveIDRefs_ReadsMediaColumnsNotOnlyExternalIDs pins the bug this
// resolution was written to fix: a title ingested by the TMDB path carries its
// ids ON THE ROW and has no external_ids entry, so a lookup that only read
// external_ids could not resolve it (a watchlist add answered not_found).
func TestResolveIDRefs_ReadsMediaColumnsNotOnlyExternalIDs(t *testing.T) {
	db := setupLookupDB(t)

	movie := Movie{TmdbID: 27205, ImdbID: "tt1375666", Title: "Inception"}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	show := Show{TmdbID: 1396, TvdbID: 81189, ImdbID: "tt0903747", Title: "Breaking Bad"}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}

	refs := []IDRef{
		{TMDB: 27205},             // only on the movie row
		{TVDB: 81189},             // only on the show row
		{IMDB: "tt0903747"},       // show, by imdb
		{TVDB: 999999},            // unknown
		{},                        // empty
		{TVDB: 81189, TMDB: 1396}, // both, same row
	}
	got, err := ResolveIDRefs(db, refs)
	if err != nil {
		t.Fatalf("ResolveIDRefs: %v", err)
	}

	want := map[IDRef]MediaRef{
		{TMDB: 27205}:             {MediaType: "movie", MediaID: movie.ID},
		{TVDB: 81189}:             {MediaType: "show", MediaID: show.ID},
		{IMDB: "tt0903747"}:       {MediaType: "show", MediaID: show.ID},
		{TVDB: 81189, TMDB: 1396}: {MediaType: "show", MediaID: show.ID},
	}
	if len(got) != len(want) {
		t.Fatalf("resolved %d refs, want %d (%v)", len(got), len(want), got)
	}
	for ref, expected := range want {
		if got[ref] != expected {
			t.Errorf("ref %+v resolved to %+v, want %+v", ref, got[ref], expected)
		}
	}
	if _, ok := got[IDRef{TVDB: 999999}]; ok {
		t.Error("an unknown id must not resolve")
	}
}

// TestResolveIDRefs_PrefersTVDBIDWhenIdsDisagree pins the documented precedence:
// TVDB is the primary identity, so a ref whose tvdb and tmdb ids name different
// rows resolves through the TVDB one.
func TestResolveIDRefs_PrefersTVDBIDWhenIdsDisagree(t *testing.T) {
	db := setupLookupDB(t)

	byTVDB := Movie{TvdbID: 169, TmdbID: 700, Title: "By TVDB"}
	byTMDB := Movie{TmdbID: 603, Title: "By TMDB"}
	if err := db.Create(&byTVDB).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Create(&byTMDB).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := ResolveIDRefs(db, []IDRef{{TVDB: 169, TMDB: 603}})
	if err != nil {
		t.Fatalf("ResolveIDRefs: %v", err)
	}
	ref := IDRef{TVDB: 169, TMDB: 603}
	if got[ref].MediaID != byTVDB.ID {
		t.Fatalf("resolved to media %d, want the tvdb row %d", got[ref].MediaID, byTVDB.ID)
	}
}

// TestResolveIDRefs_UsesTheLookupTable covers the other half: ids that live only
// in external_ids (what the TVDB ingest writes, and what a slug resolution
// reads).
func TestResolveIDRefs_UsesTheLookupTable(t *testing.T) {
	db := setupLookupDB(t)

	show := Show{Title: "Only In Lookup"}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	rows := []ExternalID{
		{MediaType: "show", MediaID: show.ID, Source: "tvdb", ExternalID: "81189"},
		{MediaType: "show", MediaID: show.ID, Source: "slug", ExternalID: "breaking-bad"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed external ids: %v", err)
	}

	got, err := ResolveIDRefs(db, []IDRef{{TVDB: 81189}})
	if err != nil {
		t.Fatalf("ResolveIDRefs: %v", err)
	}
	if got[IDRef{TVDB: 81189}].MediaID != show.ID {
		t.Fatalf("tvdb id only in external_ids did not resolve: %v", got)
	}
}

// TestResolveIDRefs_DropsStaleLookupRows: external_ids has no foreign key, so a
// row for a deleted title must not resolve.
func TestResolveIDRefs_DropsStaleLookupRows(t *testing.T) {
	db := setupLookupDB(t)

	if err := db.Create(&ExternalID{
		MediaType: "movie", MediaID: 424242, Source: "tvdb", ExternalID: "555",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := ResolveIDRefs(db, []IDRef{{TVDB: 555}})
	if err != nil {
		t.Fatalf("ResolveIDRefs: %v", err)
	}
	if _, ok := got[IDRef{TVDB: 555}]; ok {
		t.Fatal("a lookup row pointing at a missing media row must not resolve")
	}
}

// TestLookupMediaByTVDBIDs is the membership check behind "already in your
// library" on a live search result.
func TestLookupMediaByTVDBIDs(t *testing.T) {
	db := setupLookupDB(t)

	show := Show{TvdbID: 81189, Title: "Breaking Bad"}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	movie := Movie{Title: "Legacy Movie"}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	// A row ingested before the tvdb_id column: findable only via external_ids.
	if err := db.Create(&ExternalID{
		MediaType: "movie", MediaID: movie.ID, Source: "tvdb", ExternalID: "169",
	}).Error; err != nil {
		t.Fatalf("seed external id: %v", err)
	}

	shows, err := LookupMediaByTVDBIDs(db, "show", []int{81189, 999})
	if err != nil {
		t.Fatalf("LookupMediaByTVDBIDs(show): %v", err)
	}
	if shows[81189] != show.ID || len(shows) != 1 {
		t.Fatalf("show lookup = %v, want {%d: %d}", shows, 81189, show.ID)
	}

	movies, err := LookupMediaByTVDBIDs(db, "movie", []int{169})
	if err != nil {
		t.Fatalf("LookupMediaByTVDBIDs(movie): %v", err)
	}
	if movies[169] != movie.ID {
		t.Fatalf("movie lookup via external_ids = %v, want {%d: %d}", movies, 169, movie.ID)
	}

	// A type with no rows and an unknown type are both empty, not errors.
	if got, err := LookupMediaByTVDBIDs(db, "movie", []int{999}); err != nil || len(got) != 0 {
		t.Fatalf("unknown id: got %v, err %v", got, err)
	}
	if got, err := LookupMediaByTVDBIDs(db, "person", []int{1}); err != nil || len(got) != 0 {
		t.Fatalf("unsupported type: got %v, err %v", got, err)
	}
}
