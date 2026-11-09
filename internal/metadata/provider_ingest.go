package metadata

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/external/tvdb"
	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// The provider split, in one place.
//
// TMDB is PRIMARY: the tmdb id is the identity, and the title, original title,
// overview, tagline, genres, status, runtime, certification, trailer, artwork,
// seasons, episodes and (via IngestPerson / IngestCredits) people all come from
// TMDB. It is the provider with the deeper catalogue: it carries people, watch
// providers, credits and release dates that the other one does not.
//
// TVDB is SUPPLEMENTARY: it supplies the tvdb id (the link every TVDB-keyed
// media server and client speaks), a certification and trailer fallback for the
// titles TMDB leaves blank, the slug when it has one, and its raw record under
// `metadata.tvdb` — its `score` is not a 0-10 rating, so it is kept as raw
// metadata rather than written over TMDB's vote average.
//
// A TMDB record with no TVDB twin ingests fine: tvdb_id stays 0 and every
// TVDB-sourced field keeps its zero or empty value. The reverse is not
// expressible — with tmdb_id as the identity there is no row to key a
// TVDB-only title on, which is the deliberate cost of the flip.
//
// The two directions of the link are not symmetric, and that is a measured
// property of the APIs rather than a choice: TMDB's TV external-ids payload
// names a show's tvdb_id, and its MOVIE payload does not (GET
// /movie/603/external_ids returns imdb_id and the social handles only). A
// movie's link is therefore recovered through TVDB's own remote-id search
// (see ResolveMovieByTMDBID in resolver.go).

// IngestMovie creates or updates a movie from a TMDB record, with an optional
// TVDB supplement. Idempotent: it keys on tmdb_id first, then falls back to the
// tvdb_id/imdb_id/slug the row may already carry, so a title that arrived
// earlier through the TVDB-primary ingest is UPDATED rather than duplicated.
func IngestMovie(db *gorm.DB, m *tmdb.Movie, supplement *tvdb.MovieExtended) (*database.Movie, error) {
	if db == nil {
		return nil, errors.New("metadata: a database is required")
	}
	if m == nil || m.ID <= 0 {
		return nil, errors.New("metadata: a tmdb movie record with an id is required")
	}

	now := time.Now()
	record := database.Movie{
		TmdbID:        m.ID,
		TvdbID:        m.TVDBID(),
		ImdbID:        m.IMDBID,
		Title:         m.Title,
		OriginalTitle: m.OriginalTitle,
		Tagline:       m.Tagline,
		Overview:      m.Overview,
		Runtime:       m.Runtime,
		Status:        m.Status,
		Popularity:    m.Popularity,
		RatingAvg:     m.VoteAverage,
		RatingCount:   m.VoteCount,
		Genres:        database.StringSlice(genreNames(m.Genres)),
		Language:      m.Language,
		Certification: m.Certification(),
		TrailerURL:    m.TrailerURL(),
		Images:        movieImages(m, supplement),
		Metadata:      movieMetadata(m, supplement),
		Adult:         m.Adult,
		LastSyncedAt:  &now,
	}
	record.Released = parseDate(m.ReleaseDate)

	if supplement != nil {
		applyTVDBMovieSupplement(&record, supplement)
	}

	existing, err := findExistingMovie(db, &record)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		preserveExistingMovie(&record, existing)
	}
	record.Slug = ensureSlug(db, "movie", record.Slug, record.Title, record.TmdbID, yearOf(record.Released), idOfMovie(existing))

	// The row, its external-id lookup and its dated-release rows are ONE
	// write: a movie whose metadata carries releases but whose movie_releases
	// rows are missing would silently vanish from the DVD/streaming
	// calendars. The transaction keeps the two views of the same TMDB payload
	// atomic.
	if err := db.Transaction(func(tx *gorm.DB) error {
		if existing == nil {
			if err := tx.Create(&record).Error; err != nil {
				return fmt.Errorf("insert movie tmdb=%d (%s): %w", m.ID, m.Title, err)
			}
			slog.Debug("[ingest] created movie", "tmdb_id", m.ID, "title", m.Title)
		} else {
			record.ID = existing.ID
			record.CreatedAt = existing.CreatedAt
			if err := tx.Save(&record).Error; err != nil {
				return fmt.Errorf("update movie tmdb=%d (%s): %w", m.ID, m.Title, err)
			}
			slog.Debug("[ingest] updated movie", "tmdb_id", m.ID, "id", record.ID, "title", m.Title)
		}

		if err := upsertExternalIDs(tx, "movie", record.ID, map[string]string{
			"tmdb": strconv.Itoa(record.TmdbID),
			"tvdb": optionalID(record.TvdbID),
			"imdb": record.ImdbID,
			"slug": record.Slug,
		}); err != nil {
			return err
		}

		// A nil pointer means this payload did not carry release_dates (the
		// synthetic change-feed record); leave the stored rows alone rather
		// than replacing them with nothing.
		if m.ReleaseDates != nil {
			if err := ReplaceMovieReleases(tx, record.ID, ReleasesFromTMDB(m.ReleaseDates)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return &record, nil
}

// IngestShow creates or updates a show, its seasons and its episodes from a
// TMDB record, with an optional TVDB supplement. Idempotent in the same way as
// IngestMovie: keyed on tmdb_id, falling back to the other ids.
//
// Seasons arrive as full TMDB season records (the show detail lists the seasons
// and their counts; their episodes come from one call per season). When no
// season detail is supplied at all the show's episode and season counts fall
// back to TMDB's own totals — otherwise they are DERIVED from the seasons
// actually stored, excluding season 0, so the row's totals can never disagree
// with the episode rows beneath it.
func IngestShow(db *gorm.DB, s *tmdb.Show, seasons []tmdb.Season, supplement *tvdb.SeriesExtended) (*database.Show, error) {
	if db == nil {
		return nil, errors.New("metadata: a database is required")
	}
	if s == nil || s.ID <= 0 {
		return nil, errors.New("metadata: a tmdb show record with an id is required")
	}

	now := time.Now()
	record := database.Show{
		TmdbID:        s.ID,
		TvdbID:        s.TVDBID(),
		ImdbID:        s.IMDBID,
		Title:         s.Name,
		OriginalTitle: s.OriginalName,
		Overview:      s.Overview,
		Network:       networkName(s),
		Status:        s.Status,
		Popularity:    s.Popularity,
		RatingAvg:     s.VoteAverage,
		RatingCount:   s.VoteCount,
		Genres:        database.StringSlice(genreNames(s.Genres)),
		Language:      s.Language,
		Certification: s.Certification(),
		Images:        seriesImages(s, supplement),
		Metadata:      showMetadata(s, supplement),
		Adult:         s.Adult,
		LastSyncedAt:  &now,
	}
	record.FirstAired = parseDate(s.FirstAirDate)
	record.LastAired = parseDate(s.LastAirDate)
	record.EpisodeCount, record.SeasonCount = s.EpisodesCount, s.SeasonsCount
	if len(seasons) > 0 {
		record.EpisodeCount, record.SeasonCount = countTMDBSeasons(seasons)
	}

	if supplement != nil {
		applyTVDBShowSupplement(&record, supplement)
	}

	existing, err := findExistingShow(db, &record)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		preserveExistingShow(&record, existing)
	}
	record.Slug = ensureSlug(db, "show", record.Slug, record.Title, record.TmdbID, yearOf(record.FirstAired), idOfShow(existing))

	if existing == nil {
		if err := db.Create(&record).Error; err != nil {
			return nil, fmt.Errorf("insert show tmdb=%d (%s): %w", s.ID, s.Name, err)
		}
		slog.Debug("[ingest] created show", "tmdb_id", s.ID, "title", s.Name)
	} else {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
		if err := db.Save(&record).Error; err != nil {
			return nil, fmt.Errorf("update show tmdb=%d (%s): %w", s.ID, s.Name, err)
		}
		slog.Debug("[ingest] updated show", "tmdb_id", s.ID, "id", record.ID, "title", s.Name)
	}

	if err := upsertExternalIDs(db, "show", record.ID, map[string]string{
		"tmdb": strconv.Itoa(record.TmdbID),
		"tvdb": optionalID(record.TvdbID),
		"imdb": record.ImdbID,
		"slug": record.Slug,
	}); err != nil {
		return nil, err
	}

	if err := ingestTMDBSeasons(db, &record, seasons); err != nil {
		return nil, err
	}

	return &record, nil
}

// ingestTMDBSeasons upserts one season row per supplied TMDB season, then the
// episodes that came with it. A season whose detail fetch returned no episodes
// still gets its row (its count is honest: zero), so the season list matches
// the show's season count.
func ingestTMDBSeasons(db *gorm.DB, show *database.Show, seasons []tmdb.Season) error {
	for _, season := range seasons {
		summary := tmdb.SeasonSummary{
			SeasonNumber: season.SeasonNumber,
			EpisodeCount: len(season.Episodes),
			AirDate:      season.AirDate,
			PosterPath:   season.PosterPath,
		}

		row, err := IngestSeason(db, show.ID, show.TmdbID, summary)
		if err != nil {
			return err
		}
		if len(season.Episodes) == 0 {
			continue
		}
		if err := IngestEpisodesBatch(db, row.ID, show.ID, season.Episodes); err != nil {
			return err
		}
	}
	return nil
}

// countTMDBSeasons returns (episode count, season count) for the seasons
// actually stored, EXCLUDING season 0. Season 0 is TMDB's home for specials:
// it is stored (Trakt-shaped clients expect a season 0) but must not inflate a
// show's episode or season totals, which is what every UI shows as "5 seasons,
// 62 episodes" — and it is why the counts are derived here rather than copied,
// so the totals cannot claim episodes the season rows do not hold.
func countTMDBSeasons(seasons []tmdb.Season) (episodeCount, seasonCount int) {
	for _, season := range seasons {
		if season.SeasonNumber <= 0 {
			continue
		}
		seasonCount++
		episodeCount += len(season.Episodes)
	}
	return episodeCount, seasonCount
}

// networkName returns the first network TMDB lists for a show, or "".
func networkName(s *tmdb.Show) string {
	if s == nil || len(s.Networks) == 0 {
		return ""
	}
	return s.Networks[0].Name
}

// findExistingMovie looks for the movie row this record is an update of, first
// by tmdb id (the identity) and then by any other id the record carries. Ids
// are tried in the order that keeps a merge deterministic: tmdb, then tvdb,
// then imdb, then slug.
func findExistingMovie(db *gorm.DB, record *database.Movie) (*database.Movie, error) {
	queries := []struct {
		where string
		arg   any
	}{
		{"tmdb_id = ?", record.TmdbID},
		{"tvdb_id = ?", record.TvdbID},
		{"imdb_id = ?", record.ImdbID},
		{"slug = ?", record.Slug},
	}
	for _, q := range queries {
		if isEmptyID(q.arg) {
			continue
		}
		var existing database.Movie
		err := db.Where(q.where, q.arg).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lookup movie by %s: %w", q.where, err)
		}
		return &existing, nil
	}
	return nil, nil
}

func findExistingShow(db *gorm.DB, record *database.Show) (*database.Show, error) {
	queries := []struct {
		where string
		arg   any
	}{
		{"tmdb_id = ?", record.TmdbID},
		{"tvdb_id = ?", record.TvdbID},
		{"imdb_id = ?", record.ImdbID},
		{"slug = ?", record.Slug},
	}
	for _, q := range queries {
		if isEmptyID(q.arg) {
			continue
		}
		var existing database.Show
		err := db.Where(q.where, q.arg).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lookup show by %s: %w", q.where, err)
		}
		return &existing, nil
	}
	return nil, nil
}

// isEmptyID reports whether a lookup key carries nothing to match on: 0 for a
// numeric id, "" or "-" for a string one.
func isEmptyID(v any) bool {
	switch t := v.(type) {
	case int:
		return t <= 0
	case int64:
		return t <= 0
	case string:
		return t == "" || t == "0" || t == "-"
	default:
		return v == nil
	}
}
