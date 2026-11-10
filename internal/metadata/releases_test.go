package metadata

import (
	"context"
	"testing"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestBackfillMovieReleasesFromStoredMetadata proves the calendar rows are
// rebuilt from what ingest already stored, with no provider client in sight.
func TestBackfillMovieReleasesFromStoredMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Movie{}, &database.MovieRelease{}); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}

	withReleases := database.Movie{
		TmdbID: 401,
		Title:  "Stored Releases",
		Metadata: database.JSONMap{
			KeyReleases: []any{
				map[string]any{"country": "us", "release_date": "2031-06-02", "release_type": "physical", "certification": "PG", "note": ""},
				map[string]any{"country": "gb", "release_date": "2031-06-06", "release_type": "physical", "certification": "12", "note": ""},
				map[string]any{"country": "us", "release_date": "2031-07-01", "release_type": "digital", "certification": "", "note": ""},
				map[string]any{"country": "us", "release_date": "not-a-date", "release_type": "physical"},
				map[string]any{"country": "", "release_date": "2031-06-03", "release_type": "physical"},
			},
		},
	}
	withoutKey := database.Movie{TmdbID: 402, Title: "No Release Metadata", Metadata: database.JSONMap{}}
	for _, movie := range []*database.Movie{&withReleases, &withoutKey} {
		if err := db.Create(movie).Error; err != nil {
			t.Fatalf("seed movie: %v", err)
		}
	}

	written, err := BackfillMovieReleases(context.Background(), db, 100)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if written != 1 {
		t.Fatalf("expected one populated movie, got %d", written)
	}

	var rows []database.MovieRelease
	if err := db.Order("country ASC, release_type ASC, release_date ASC").Find(&rows).Error; err != nil {
		t.Fatalf("read releases: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 valid rows (invalid date/country skipped), got %d: %+v", len(rows), rows)
	}
	if rows[0].Country != "gb" || rows[0].ReleaseType != ReleaseTypePhysical || rows[0].ReleaseDate.String() != "2031-06-06" {
		t.Errorf("gb row wrong: %+v", rows[0])
	}
	// The remaining two are both US, ordered by release_type: digital < physical.
	if rows[1].Country != "us" || rows[1].ReleaseType != ReleaseTypeDigital {
		t.Errorf("us digital row wrong: %+v", rows[1])
	}
	if rows[2].Country != "us" || rows[2].ReleaseDate.String() != "2031-06-02" || rows[2].Certification != "PG" {
		t.Errorf("us physical row wrong: %+v", rows[2])
	}

	// Idempotent: a second run rewrites the same three rows, not six.
	if _, err := BackfillMovieReleases(context.Background(), db, 100); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	var count int64
	if err := db.Model(&database.MovieRelease{}).Count(&count).Error; err != nil {
		t.Fatalf("count releases: %v", err)
	}
	if count != 3 {
		t.Fatalf("backfill must be idempotent, got %d rows", count)
	}
}

func TestReplaceMovieReleasesDeduplicates(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Movie{}, &database.MovieRelease{}); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	movie := database.Movie{TmdbID: 403, Title: "Duplicate Releases"}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		return ReplaceMovieReleases(tx, movie.ID, []contractshape.Release{
			{Country: "US", ReleaseDate: "2031-06-02", ReleaseType: "physical"},
			{Country: "us", ReleaseDate: "2031-06-02", ReleaseType: "physical"},
			{Country: "US", ReleaseDate: "2031-06-02", ReleaseType: "theatrical"},
		})
	})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}

	var rows []database.MovieRelease
	if err := db.Find(&rows).Error; err != nil {
		t.Fatalf("read releases: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected one physical and one theatrical row, got %d: %+v", len(rows), rows)
	}
	for _, row := range rows {
		if row.Country != "us" {
			t.Errorf("country must normalise to lower case: %+v", row)
		}
	}
}

// TestMovieReleaseRowsAcceptsTMDBDateTime pins the fix for TMDB's release_dates
// shape: that endpoint returns a date-time ("1999-03-31T00:00:00.000Z"), and a
// bare "2006-01-02" parse silently dropped every row, leaving movie_releases
// empty for real data and the DVD/streaming calendars reading nothing.
func TestMovieReleaseRowsAcceptsTMDBDateTime(t *testing.T) {
	rows := movieReleaseRows(7, []contractshape.Release{
		{Country: "US", ReleaseDate: "1999-03-31T00:00:00.000Z", ReleaseType: ReleaseTypeTheatrical},
		{Country: "GB", ReleaseDate: "2031-06-06", ReleaseType: ReleaseTypePhysical},
	})
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (a date-time release must not be dropped): %+v", len(rows), rows)
	}
	if got := rows[0].ReleaseDate.String(); got != "1999-03-31" {
		t.Errorf("date-time release stored as %q, want 1999-03-31", got)
	}
	if got := rows[1].ReleaseDate.String(); got != "2031-06-06" {
		t.Errorf("bare-date release stored as %q, want 2031-06-06", got)
	}
}
