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
	"gorm.io/gorm/clause"
)

// The provider split, in one place.
//
// TVDB is PRIMARY: the tvdb id is the identity, and the title, overview,
// genres, status, network, runtime, certification, artwork, seasons and
// episodes all come from TVDB (episodes in TVDB's default/official order — the
// order media servers configured against TVDB use, and the same order
// theintrodb-api addresses episodes in).
//
// TMDB is SUPPLEMENTARY: it supplies the link back to TMDB (tmdb_id, and the
// IMDb id when TVDB has none), the rating numbers (vote_average/vote_count —
// TVDB's `score` is not a 0-10 rating and is stored as raw metadata instead),
// popularity, the tagline, and a poster/backdrop fallback for the titles TVDB
// has no artwork for. A TVDB record with no TMDB twin ingests fine: TmdbID
// stays 0 and every TMDB-sourced number stays at its zero value.
//
// Both directions of the link are used: TVDB's remoteIds usually name the TMDB
// record, and when it does not, external/tmdb.Find resolves the TVDB id the
// other way (see ResolveMovieByTVDBID in resolver.go).

// IngestTVDBMovie creates or updates a movie from a TVDB record, with an
// optional TMDB supplement. Idempotent: it keys on tvdb_id first, then falls
// back to the tmdb_id/imdb_id/slug the row may already carry, so a title that
// arrived earlier through the TMDB backfill is UPDATED rather than duplicated.
func IngestTVDBMovie(db *gorm.DB, m *tvdb.MovieExtended, supplement *tmdb.Movie) (*database.Movie, error) {
	if db == nil {
		return nil, errors.New("metadata: a database is required")
	}
	if m == nil || m.ID <= 0 {
		return nil, errors.New("metadata: a tvdb movie record with an id is required")
	}

	now := time.Now()
	record := database.Movie{
		TvdbID:        m.ID,
		Slug:          m.Slug,
		ImdbID:        m.IMDBID(),
		Title:         m.Name,
		Overview:      m.Overview(),
		Tagline:       m.Tagline(),
		Runtime:       m.Runtime,
		Status:        m.Status.Name,
		Genres:        database.StringSlice(m.GenreNames()),
		Language:      m.OriginalLanguage,
		Certification: m.Certification(),
		TrailerURL:    m.TrailerURL(),
		Images:        movieImages(m, supplement),
		Metadata:      tvdbMetadata(m),
		LastSyncedAt:  &now,
	}
	record.Released = parseDate(m.ReleasedDate())

	if supplement != nil {
		applyTMDBMovieSupplement(&record, supplement)
	}

	existing, err := findExistingMovie(db, &record)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		if err := db.Create(&record).Error; err != nil {
			return nil, fmt.Errorf("insert movie tvdb=%d (%s): %w", m.ID, m.Name, err)
		}
		slog.Debug("[ingest] created movie", "tvdb_id", m.ID, "title", m.Name)
	} else {
		preserveExistingMovie(&record, existing, supplement != nil)
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
		if err := db.Save(&record).Error; err != nil {
			return nil, fmt.Errorf("update movie tvdb=%d (%s): %w", m.ID, m.Name, err)
		}
		slog.Debug("[ingest] updated movie", "tvdb_id", m.ID, "id", record.ID, "title", m.Name)
	}

	if err := upsertExternalIDs(db, "movie", record.ID, map[string]string{
		"tvdb": strconv.Itoa(record.TvdbID),
		"tmdb": optionalID(record.TmdbID),
		"imdb": record.ImdbID,
		"slug": record.Slug,
	}); err != nil {
		return nil, err
	}

	return &record, nil
}

// IngestTVDBShow creates or updates a show, its seasons and its episodes from a
// TVDB record, with an optional TMDB supplement. Idempotent in the same way as
// IngestTVDBMovie: keyed on tvdb_id, falling back to the other ids.
//
// Episodes arrive separately because TVDB serves them from their own endpoint,
// in the ordering the caller asked for; seasons are DERIVED from that episode
// list (TVDB's season list is per-ordering and carries no episode counts), so
// the season rows and the episodes can never disagree.
func IngestTVDBShow(db *gorm.DB, s *tvdb.SeriesExtended, episodes []tvdb.Episode, supplement *tmdb.Show) (*database.Show, error) {
	if db == nil {
		return nil, errors.New("metadata: a database is required")
	}
	if s == nil || s.ID <= 0 {
		return nil, errors.New("metadata: a tvdb series record with an id is required")
	}

	now := time.Now()
	record := database.Show{
		TvdbID:        s.ID,
		Slug:          s.Slug,
		ImdbID:        s.IMDBID(),
		Title:         s.Name,
		Overview:      s.Overview,
		Network:       s.Network(),
		Status:        s.Status.Name,
		Genres:        database.StringSlice(s.GenreNames()),
		Language:      s.OriginalLanguage,
		Certification: s.Certification(),
		Images:        seriesImages(s, supplement),
		Metadata:      tvdbMetadata(s),
		LastSyncedAt:  &now,
	}
	record.FirstAired = parseDate(s.FirstAired)
	record.LastAired = parseDate(s.LastAired)
	record.EpisodeCount, record.SeasonCount = countEpisodes(episodes)

	if supplement != nil {
		applyTMDBShowSupplement(&record, supplement)
	}

	existing, err := findExistingShow(db, &record)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		if err := db.Create(&record).Error; err != nil {
			return nil, fmt.Errorf("insert show tvdb=%d (%s): %w", s.ID, s.Name, err)
		}
		slog.Debug("[ingest] created show", "tvdb_id", s.ID, "title", s.Name)
	} else {
		preserveExistingShow(&record, existing, supplement != nil)
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
		if err := db.Save(&record).Error; err != nil {
			return nil, fmt.Errorf("update show tvdb=%d (%s): %w", s.ID, s.Name, err)
		}
		slog.Debug("[ingest] updated show", "tvdb_id", s.ID, "id", record.ID, "title", s.Name)
	}

	if err := upsertExternalIDs(db, "show", record.ID, map[string]string{
		"tvdb": strconv.Itoa(record.TvdbID),
		"tmdb": optionalID(record.TmdbID),
		"imdb": record.ImdbID,
		"slug": record.Slug,
	}); err != nil {
		return nil, err
	}

	if err := ingestTVDBSeasons(db, &record, episodes); err != nil {
		return nil, err
	}

	return &record, nil
}

// ingestTVDBSeasons upserts one season row per distinct episode season, then
// upserts the episodes themselves against those rows.
func ingestTVDBSeasons(db *gorm.DB, show *database.Show, episodes []tvdb.Episode) error {
	if len(episodes) == 0 {
		return nil
	}

	// One season row per distinct season number, in a stable order.
	seasonIDs := map[int]int64{}
	numbers := make([]int, 0, 8)
	counts := map[int]int{}
	for _, ep := range episodes {
		if _, seen := counts[ep.SeasonNumber]; !seen {
			numbers = append(numbers, ep.SeasonNumber)
		}
		counts[ep.SeasonNumber]++
	}

	now := time.Now()
	for _, number := range numbers {
		season := database.Season{
			ShowID:       show.ID,
			TmdbShowID:   show.TmdbID,
			SeasonNumber: number,
			EpisodeCount: counts[number],
			LastSyncedAt: &now,
		}

		var existing database.Season
		err := db.Where("show_id = ? AND season_number = ?", show.ID, number).First(&existing).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := db.Create(&season).Error; err != nil {
				return fmt.Errorf("insert season %d for show %d: %w", number, show.ID, err)
			}
			seasonIDs[number] = season.ID
		case err != nil:
			return fmt.Errorf("lookup season %d for show %d: %w", number, show.ID, err)
		default:
			season.ID = existing.ID
			season.CreatedAt = existing.CreatedAt
			if err := db.Save(&season).Error; err != nil {
				return fmt.Errorf("update season %d for show %d: %w", number, show.ID, err)
			}
			seasonIDs[number] = existing.ID
		}
	}

	return ingestTVDBEpisodes(db, show.ID, episodes, seasonIDs)
}

// ingestTVDBEpisodes upserts episodes keyed on (show_id, season_id, episode_number).
func ingestTVDBEpisodes(db *gorm.DB, showID int64, episodes []tvdb.Episode, seasonIDs map[int]int64) error {
	now := time.Now()
	records := make([]database.Episode, 0, len(episodes))

	for _, ep := range episodes {
		seasonID, ok := seasonIDs[ep.SeasonNumber]
		if !ok || seasonID == 0 {
			// A season row is created for every distinct season above, so this
			// can only happen if the episode list changed mid-loop; skipping
			// keeps the batch consistent with the season set that was written.
			continue
		}
		r := database.Episode{
			SeasonID:      seasonID,
			ShowID:        showID,
			EpisodeNumber: ep.Number,
			Title:         ep.Name,
			Overview:      ep.Overview,
			Runtime:       ep.Runtime,
			LastSyncedAt:  &now,
		}
		if ep.AbsoluteNumber > 0 {
			absolute := ep.AbsoluteNumber
			r.AbsoluteNumber = &absolute
		}
		// Always a non-nil map: a nil JSONMap makes GORM write DEFAULT into
		// the VALUES list, which sqlite's parser rejects (and which is a
		// portability accident rather than an intent).
		images := database.JSONMap{}
		if still := ep.ImageURL(); still != "" {
			images["still"] = still
			images["still_url"] = still
		}
		r.Images = images
		r.AiredAt = parseDate(ep.Aired)
		records = append(records, r)
	}

	if len(records) == 0 {
		return nil
	}

	// Upsert on the episode identity. The unique index behind this key is
	// (show_id, season_id, episode_number); a re-ingest updates the row.
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "show_id"},
			{Name: "season_id"},
			{Name: "episode_number"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"title", "overview", "runtime", "images",
			"aired_at", "absolute_number", "last_synced_at", "updated_at",
		}),
	}).CreateInBatches(records, 100).Error
}

// countEpisodes returns (episode count, season count) for a show, EXCLUDING
// season 0. Season 0 is TVDB's home for specials and extras: they are stored
// (Trakt-shaped clients expect a season 0) but they must not inflate a show's
// episode or season totals, which is what every UI shows as "12 seasons, 62
// episodes".
func countEpisodes(episodes []tvdb.Episode) (episodeCount, seasonCount int) {
	seasons := map[int]bool{}
	for _, ep := range episodes {
		if ep.SeasonNumber <= 0 {
			continue
		}
		episodeCount++
		seasons[ep.SeasonNumber] = true
	}
	return episodeCount, len(seasons)
}

// findExistingMovie looks for the movie row this record is an update of, first
// by tvdb id and then by any other id the record carries. Ids are tried in the
// order that keeps a merge deterministic: tvdb (the identity), then tmdb, then
// imdb, then slug.
func findExistingMovie(db *gorm.DB, record *database.Movie) (*database.Movie, error) {
	queries := []struct {
		where string
		arg   any
	}{
		{"tvdb_id = ?", record.TvdbID},
		{"tmdb_id = ?", record.TmdbID},
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
		{"tvdb_id = ?", record.TvdbID},
		{"tmdb_id = ?", record.TmdbID},
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

// upsertExternalIDs writes the lookup rows behind ?id_type= and slug
// resolution. Empty sources are skipped, and an existing row is re-pointed at
// this media id rather than duplicated (the unique key is
// media_type+source+external_id).
func upsertExternalIDs(db *gorm.DB, mediaType string, mediaID int64, pairs map[string]string) error {
	if mediaID == 0 {
		return nil
	}
	rows := make([]database.ExternalID, 0, len(pairs))
	// A fixed order keeps the write deterministic and the logs comparable.
	for _, source := range []string{"tvdb", "tmdb", "imdb", "slug"} {
		value := pairs[source]
		if value == "" {
			continue
		}
		rows = append(rows, database.ExternalID{
			MediaType:  mediaType,
			MediaID:    mediaID,
			Source:     source,
			ExternalID: value,
		})
	}
	if len(rows) == 0 {
		return nil
	}

	if err := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "media_type"},
			{Name: "source"},
			{Name: "external_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"media_id"}),
	}).CreateInBatches(rows, 50).Error; err != nil {
		return fmt.Errorf("upsert external ids for %s/%d: %w", mediaType, mediaID, err)
	}
	return nil
}

func optionalID(id int) string {
	if id <= 0 {
		return ""
	}
	return strconv.Itoa(id)
}

// preserveExistingMovie fills the gaps in a record from the row it is updating.
//
// Two cases make this necessary. A TVDB refresh keyed on the tvdb id must not
// drop the tmdb id (or slug/imdb id) the row already carried — otherwise the
// merge silently unmakes the link the other provider supplied. And a refresh
// with NO TMDB supplement must not zero the supplementary numbers (rating,
// popularity) a previous ingest stored, nor replace a merged image set with a
// TVDB-only one. When a supplement IS present those fields are the supplement's
// to set, so only the identifiers are carried over.
func preserveExistingMovie(record, existing *database.Movie, hasSupplement bool) {
	if record.TvdbID == 0 {
		record.TvdbID = existing.TvdbID
	}
	if record.TmdbID == 0 {
		record.TmdbID = existing.TmdbID
	}
	if record.ImdbID == "" {
		record.ImdbID = existing.ImdbID
	}
	if record.Slug == "" {
		record.Slug = existing.Slug
	}
	record.Images = mergeImages(existing.Images, record.Images)
	if hasSupplement {
		return
	}
	record.Popularity = existing.Popularity
	record.RatingAvg = existing.RatingAvg
	record.RatingCount = existing.RatingCount
	record.Adult = existing.Adult
}

// preserveExistingShow is preserveExistingMovie for a series (it has no tagline
// or adult-specific sourcing differences to speak of).
func preserveExistingShow(record, existing *database.Show, hasSupplement bool) {
	if record.TvdbID == 0 {
		record.TvdbID = existing.TvdbID
	}
	if record.TmdbID == 0 {
		record.TmdbID = existing.TmdbID
	}
	if record.ImdbID == "" {
		record.ImdbID = existing.ImdbID
	}
	if record.Slug == "" {
		record.Slug = existing.Slug
	}
	record.Images = mergeImages(existing.Images, record.Images)
	if hasSupplement {
		return
	}
	record.Popularity = existing.Popularity
	record.RatingAvg = existing.RatingAvg
	record.RatingCount = existing.RatingCount
	record.Adult = existing.Adult
}

// mergeImages layers a new image set over an existing one, so a refresh keeps
// whatever the previous ingest knew (for example a TMDB poster_path) while the
// fields this ingest did set win.
func mergeImages(existing, incoming database.JSONMap) database.JSONMap {
	if len(existing) == 0 {
		return incoming
	}
	merged := make(database.JSONMap, len(existing)+len(incoming))
	for key, value := range existing {
		merged[key] = value
	}
	for key, value := range incoming {
		merged[key] = value
	}
	return merged
}

// applyTMDBMovieSupplement layers TMDB's supplementary fields onto a movie the
// TVDB record already described.
func applyTMDBMovieSupplement(record *database.Movie, s *tmdb.Movie) {
	record.TmdbID = s.ID
	record.Popularity = s.Popularity
	record.RatingAvg = s.VoteAverage
	record.RatingCount = s.VoteCount
	record.Adult = s.Adult
	if record.Tagline == "" {
		record.Tagline = s.Tagline
	}
	if record.Overview == "" {
		record.Overview = s.Overview
	}
	if record.Runtime == 0 {
		record.Runtime = s.Runtime
	}
	if record.ImdbID == "" {
		record.ImdbID = s.IMDBID
	}
	if record.Title == "" {
		record.Title = s.Title
	}
	if record.OriginalTitle == "" {
		record.OriginalTitle = s.OriginalTitle
	}
	if record.Status == "" {
		record.Status = s.Status
	}
}

func applyTMDBShowSupplement(record *database.Show, s *tmdb.Show) {
	record.TmdbID = s.ID
	record.Popularity = s.Popularity
	record.RatingAvg = s.VoteAverage
	record.RatingCount = s.VoteCount
	record.Adult = s.Adult
	if record.Overview == "" {
		record.Overview = s.Overview
	}
	if record.Network == "" && len(s.Networks) > 0 {
		record.Network = s.Networks[0].Name
	}
	if record.ImdbID == "" {
		record.ImdbID = s.IMDBID
	}
	if record.Title == "" {
		record.Title = s.Name
	}
	if record.OriginalTitle == "" {
		record.OriginalTitle = s.OriginalName
	}
	if record.Status == "" {
		record.Status = s.Status
	}
}

// movieImages builds the movie image set. TVDB artwork URLs are absolute and
// are the primary source; TMDB's path-only poster/backdrop are carried both
// raw (poster_path, for clients that build TMDB URLs themselves) and joined
// (used only when TVDB has no artwork at all).
func movieImages(m *tvdb.MovieExtended, supplement *tmdb.Movie) database.JSONMap {
	images := database.JSONMap{}
	poster, backdrop := m.Poster(), m.Backdrop()
	if poster != "" {
		images["poster"] = poster
		images["tvdb_poster"] = poster
	}
	if backdrop != "" {
		images["backdrop"] = backdrop
		images["tvdb_backdrop"] = backdrop
	}
	if m.Image != "" {
		images["tvdb_image"] = m.Image
	}
	if supplement != nil {
		if supplement.PosterPath != "" {
			images["poster_path"] = supplement.PosterPath
			if poster == "" {
				images["poster"] = tmdb.ImageURL(supplement.PosterPath, tmdb.ImageSizePoster)
			}
		}
		if supplement.BackdropPath != "" {
			images["backdrop_path"] = supplement.BackdropPath
			if backdrop == "" {
				images["backdrop"] = tmdb.ImageURL(supplement.BackdropPath, tmdb.ImageSizeBackdrop)
			}
		}
	}
	return images
}

// seriesImages is movieImages for a series, plus the clear logo.
func seriesImages(s *tvdb.SeriesExtended, supplement *tmdb.Show) database.JSONMap {
	images := database.JSONMap{}
	poster, backdrop := s.Poster(), s.Backdrop()
	if poster != "" {
		images["poster"] = poster
		images["tvdb_poster"] = poster
	}
	if backdrop != "" {
		images["backdrop"] = backdrop
		images["tvdb_backdrop"] = backdrop
	}
	if logo := s.Logo(); logo != "" {
		images["logo"] = logo
	}
	if s.Image != "" {
		images["tvdb_image"] = s.Image
	}
	if supplement != nil {
		if supplement.PosterPath != "" {
			images["poster_path"] = supplement.PosterPath
			if poster == "" {
				images["poster"] = tmdb.ImageURL(supplement.PosterPath, tmdb.ImageSizePoster)
			}
		}
		if supplement.BackdropPath != "" {
			images["backdrop_path"] = supplement.BackdropPath
			if backdrop == "" {
				images["backdrop"] = tmdb.ImageURL(supplement.BackdropPath, tmdb.ImageSizeBackdrop)
			}
		}
	}
	return images
}

// tvdbMetadata stores the raw TVDB record under a namespaced key, so the shape
// the ingest read is auditable from the row itself. It deliberately does NOT
// replace metadata the TMDB paths already put there for a row.
func tvdbMetadata(v any) database.JSONMap {
	return database.JSONMap{"tvdb": toJSONMap(v)}
}
