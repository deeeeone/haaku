package metadata

// Dated movie releases (issue #69).
//
// TMDB's release_dates payload is stored twice from one fetch: as the contract
// shape in movies.metadata["releases"] (what the read routes serve) and as rows
// in movie_releases (what the DVD/streaming calendars can range-query). Both
// writes happen in one transaction, so a title can never exist with release
// metadata but no calendar rows.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Release types the movie_releases.release_type column stores (Trakt's
// vocabulary). An unknown value is normalised rather than dropped, so a future
// TMDB type still has a row the calendars can ignore.
const (
	ReleaseTypeTheatrical = "theatrical"
	ReleaseTypeDigital    = "digital"
	ReleaseTypePhysical   = "physical"
	ReleaseTypePremiere   = "premiere"
	ReleaseTypeLimited    = "limited"
	ReleaseTypeTV         = "tv"
	ReleaseTypeUnknown    = "unknown"
)

// ReplaceMovieReleases makes the stored release rows match one movie's payload.
// It must be called inside the transaction that writes the movie row; the
// delete-then-insert is what removes a release TMDB no longer lists.
func ReplaceMovieReleases(db *gorm.DB, movieID int64, releases []contractshape.Release) error {
	if db == nil || movieID == 0 {
		return nil
	}
	if err := db.Where("movie_id = ?", movieID).Delete(&database.MovieRelease{}).Error; err != nil {
		return fmt.Errorf("clear movie releases for %d: %w", movieID, err)
	}
	rows := movieReleaseRows(movieID, releases)
	if len(rows) == 0 {
		return nil
	}
	// The rows are already deduplicated; DoNothing keeps a repeated (country,
	// type, date) from turning one TMDB quirk into a failed ingest.
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(rows, 100).Error; err != nil {
		return fmt.Errorf("write movie releases for %d: %w", movieID, err)
	}
	return nil
}

// movieReleaseRows flattens the contract release shape into rows. A release
// with no country or an unparseable date is skipped: it cannot answer a
// date-window query, and inventing either value is exactly what this repo
// forbids.
// releaseDateOnly reduces TMDB's release_dates value to the calendar date the
// movie_releases.release_date column stores. That endpoint returns a date-time
// ("1999-03-31T00:00:00.000Z"), and parseDate only knows "2006-01-02", so
// without this every real release parsed to nil and movie_releases stayed empty
// — the DVD/streaming calendars then read an empty table.
func releaseDateOnly(value string) string {
	value = strings.TrimSpace(value)
	if i := strings.IndexByte(value, 'T'); i > 0 {
		return value[:i]
	}
	if len(value) > 10 {
		return value[:10]
	}
	return value
}

func movieReleaseRows(movieID int64, releases []contractshape.Release) []database.MovieRelease {
	out := make([]database.MovieRelease, 0, len(releases))
	seen := make(map[string]bool, len(releases))
	for _, release := range releases {
		country := strings.ToLower(strings.TrimSpace(release.Country))
		date := parseDate(releaseDateOnly(release.ReleaseDate))
		if country == "" || date == nil {
			continue
		}
		releaseType := canonicalReleaseType(release.ReleaseType)
		key := country + "|" + releaseType + "|" + date.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, database.MovieRelease{
			MovieID:       movieID,
			Country:       country,
			ReleaseType:   releaseType,
			ReleaseDate:   *date,
			Certification: release.Certification,
			Note:          release.Note,
		})
	}
	return out
}

// canonicalReleaseType maps a stored release type onto the column's vocabulary.
func canonicalReleaseType(releaseType string) string {
	switch strings.ToLower(strings.TrimSpace(releaseType)) {
	case ReleaseTypeTheatrical:
		return ReleaseTypeTheatrical
	case ReleaseTypeDigital:
		return ReleaseTypeDigital
	case ReleaseTypePhysical:
		return ReleaseTypePhysical
	case ReleaseTypePremiere:
		return ReleaseTypePremiere
	case ReleaseTypeLimited:
		return ReleaseTypeLimited
	case ReleaseTypeTV:
		return ReleaseTypeTV
	default:
		return ReleaseTypeUnknown
	}
}

// BackfillMovieReleases builds movie_releases from the release metadata already
// stored on each movie row. It makes no provider call: the input is
// movies.metadata["releases"], which every title ingested since issue #68
// carries. Titles with no stored releases key are skipped (their release set
// was never fetched, which is different from "TMDB has none").
//
// Distinct from Backfill: that refreshes titles FROM TMDB. This one costs no
// API budget and is idempotent (ReplaceMovieReleases rewrites one movie's
// rows).
func BackfillMovieReleases(ctx context.Context, db *gorm.DB, batchSize int) (int, error) {
	if db == nil {
		return 0, nil
	}
	if batchSize <= 0 {
		batchSize = 500
	}

	written := 0
	var lastID int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		var movies []database.Movie
		if err := db.Select("id", "metadata").
			Where("id > ?", lastID).
			Order("id ASC").
			Limit(batchSize).
			Find(&movies).Error; err != nil {
			return written, fmt.Errorf("scan movies for release backfill: %w", err)
		}
		if len(movies) == 0 {
			break
		}
		for i := range movies {
			movie := &movies[i]
			lastID = movie.ID
			if !HasKey(movie.Metadata, KeyReleases) {
				continue
			}
			releases := storedReleases(movie.Metadata)
			if err := db.Transaction(func(tx *gorm.DB) error {
				return ReplaceMovieReleases(tx, movie.ID, releases)
			}); err != nil {
				return written, err
			}
			written++
		}
		if len(movies) < batchSize {
			break
		}
	}

	slog.Info("[releases] backfill complete", "movies", written)
	return written, nil
}

// storedReleases reads one movie's metadata blob back into the contract release
// shape, tolerating the []any a JSONB read produces.
func storedReleases(meta database.JSONMap) []contractshape.Release {
	objects := StoredObjects(meta, KeyReleases)
	out := make([]contractshape.Release, 0, len(objects))
	for _, object := range objects {
		out = append(out, contractshape.Release{
			Country:       storedString(object, "country"),
			Certification: storedString(object, "certification"),
			ReleaseDate:   storedString(object, "release_date"),
			ReleaseType:   storedString(object, "release_type"),
			Note:          storedString(object, "note"),
		})
	}
	return out
}

// storedString reads a string field from a stored JSON object, or "".
func storedString(object map[string]any, key string) string {
	if value, ok := object[key].(string); ok {
		return value
	}
	return ""
}
