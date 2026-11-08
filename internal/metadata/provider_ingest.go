package metadata

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/external/tvdb"
	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

	if existing == nil {
		if err := db.Create(&record).Error; err != nil {
			return nil, fmt.Errorf("insert movie tmdb=%d (%s): %w", m.ID, m.Title, err)
		}
		slog.Debug("[ingest] created movie", "tmdb_id", m.ID, "title", m.Title)
	} else {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
		if err := db.Save(&record).Error; err != nil {
			return nil, fmt.Errorf("update movie tmdb=%d (%s): %w", m.ID, m.Title, err)
		}
		slog.Debug("[ingest] updated movie", "tmdb_id", m.ID, "id", record.ID, "title", m.Title)
	}

	if err := upsertExternalIDs(db, "movie", record.ID, map[string]string{
		"tmdb": strconv.Itoa(record.TmdbID),
		"tvdb": optionalID(record.TvdbID),
		"imdb": record.ImdbID,
		"slug": record.Slug,
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

// upsertExternalIDs writes the lookup rows behind ?id_type= and slug
// resolution. Empty sources are skipped, and an existing row is re-pointed at
// this media id rather than duplicated (the unique key is
// media_type+source+external_id).
//
// The re-point is why a slug must be unique per media type before it is written
// here: upserting the same slug for a different title would silently take it
// away from the first one. ensureSlug is what guarantees that.
func upsertExternalIDs(db *gorm.DB, mediaType string, mediaID int64, pairs map[string]string) error {
	if mediaID == 0 {
		return nil
	}
	rows := make([]database.ExternalID, 0, len(pairs))
	// A fixed order keeps the write deterministic and the logs comparable.
	for _, source := range []string{"tmdb", "tvdb", "imdb", "slug"} {
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

// --- slugs ---

// ensureSlug picks the slug a media row is addressed by at /movies/{slug} and
// /shows/{slug}.
//
// TMDB names no slug, so with TMDB as the primary provider the slug comes from
// one of three places, in order: the TVDB supplement's slug (the value the
// previous, TVDB-primary ingest stored, so existing URLs keep working), the
// slug the row already carries, or a value derived from the title. Derivation
// is collision-checked because external_ids has a UNIQUE
// (media_type, source, external_id) key and the ingest upserts it: two titles
// deriving the same slug would make the second one take the slug away from the
// first, breaking its URL. excludeID is the row being written, which may of
// course keep the slug it already owns.
func ensureSlug(db *gorm.DB, mediaType, provided, title string, externalID, year int, excludeID int64) string {
	if provided != "" && slugIsFree(db, mediaType, provided, excludeID) {
		return provided
	}

	base := slugify(title)
	if base == "" {
		if externalID > 0 {
			return fmt.Sprintf("%s-%d", mediaType, externalID)
		}
		return ""
	}

	candidates := []string{base}
	if year > 0 {
		candidates = append(candidates, fmt.Sprintf("%s-%d", base, year))
	}
	if externalID > 0 {
		candidates = append(candidates, fmt.Sprintf("%s-%d", base, externalID))
	}
	for _, candidate := range candidates {
		if slugIsFree(db, mediaType, candidate, excludeID) {
			return candidate
		}
	}
	// Every candidate is taken; a media-type-qualified id is unique by
	// construction because the id is.
	if externalID > 0 {
		return fmt.Sprintf("%s-%d-%d", base, externalID, year)
	}
	return ""
}

// slugIsFree reports whether a slug is unused, or already belongs to the row
// being written. A lookup-table error is treated as "not free" so a slug is
// never stolen on the strength of a failed query.
func slugIsFree(db *gorm.DB, mediaType, slug string, excludeID int64) bool {
	if db == nil || slug == "" {
		return false
	}
	var ext database.ExternalID
	err := db.Where("media_type = ? AND source = ? AND external_id = ?", mediaType, "slug", slug).
		First(&ext).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return true
	case err != nil:
		slog.Warn("[ingest] slug availability check failed, deriving another",
			"media_type", mediaType, "slug", slug, "error", err)
		return false
	default:
		return excludeID != 0 && ext.MediaID == excludeID
	}
}

// slugify lowercases a title and reduces every run of non-alphanumerics to a
// single dash: "The Matrix" → "the-matrix", "S.W.A.T." → "s-w-a-t".
func slugify(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func yearOf(d *database.Date) int {
	if d == nil || d.IsZero() {
		return 0
	}
	return d.Year()
}

func idOfMovie(m *database.Movie) int64 {
	if m == nil {
		return 0
	}
	return m.ID
}

func idOfShow(s *database.Show) int64 {
	if s == nil {
		return 0
	}
	return s.ID
}

// --- updating an existing row ---

// preserveExistingMovie fills the gaps in a record from the row it is updating.
//
// Two cases make this necessary. A refresh keyed on the tmdb id must not drop
// the tvdb id (or slug/imdb id) the row already carried — otherwise the merge
// silently unmakes the link the other provider supplied. And a TMDB record
// whose text came back empty (a thin translation, a not-yet-populated overview)
// must not blank what an earlier ingest stored: db.Save writes every column.
//
// The supplementary numbers are TMDB's to set on every ingest — it is the
// primary provider, so its vote average and popularity always win — but an
// image set and a metadata blob are MERGED, because each provider contributes
// half of both.
func preserveExistingMovie(record, existing *database.Movie) {
	if record.TmdbID == 0 {
		record.TmdbID = existing.TmdbID
	}
	if record.TvdbID == 0 {
		record.TvdbID = existing.TvdbID
	}
	if record.ImdbID == "" {
		record.ImdbID = existing.ImdbID
	}
	if record.Slug == "" {
		record.Slug = existing.Slug
	}
	record.Images = mergeImages(existing.Images, record.Images)
	if record.Overview == "" {
		record.Overview = existing.Overview
	}
	if record.Tagline == "" {
		record.Tagline = existing.Tagline
	}
	if record.OriginalTitle == "" {
		record.OriginalTitle = existing.OriginalTitle
	}
	if record.Certification == "" {
		record.Certification = existing.Certification
	}
	if record.TrailerURL == "" {
		record.TrailerURL = existing.TrailerURL
	}
	if record.Runtime == 0 {
		record.Runtime = existing.Runtime
	}
	if len(record.Genres) == 0 {
		record.Genres = existing.Genres
	}
	// The metadata blob is namespaced ({"tmdb": ..., "tvdb": ...}); replacing
	// it would drop the other provider's half.
	record.Metadata = mergeJSONMaps(existing.Metadata, record.Metadata)
}

// preserveExistingShow is preserveExistingMovie for a series (it has no tagline
// to speak of, and carries first/last air dates instead).
func preserveExistingShow(record, existing *database.Show) {
	if record.TmdbID == 0 {
		record.TmdbID = existing.TmdbID
	}
	if record.TvdbID == 0 {
		record.TvdbID = existing.TvdbID
	}
	if record.ImdbID == "" {
		record.ImdbID = existing.ImdbID
	}
	if record.Slug == "" {
		record.Slug = existing.Slug
	}
	record.Images = mergeImages(existing.Images, record.Images)
	if record.Overview == "" {
		record.Overview = existing.Overview
	}
	if record.OriginalTitle == "" {
		record.OriginalTitle = existing.OriginalTitle
	}
	if record.Network == "" {
		record.Network = existing.Network
	}
	if record.Certification == "" {
		record.Certification = existing.Certification
	}
	if len(record.Genres) == 0 {
		record.Genres = existing.Genres
	}
	record.Metadata = mergeJSONMaps(existing.Metadata, record.Metadata)
}

// mergeJSONMaps layers incoming over existing, so keys this ingest set win and
// keys it did not know about survive.
func mergeJSONMaps(existing, incoming database.JSONMap) database.JSONMap {
	if len(existing) == 0 {
		return incoming
	}
	if len(incoming) == 0 {
		return existing
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

// mergeImages layers a new image set over an existing one, so a refresh keeps
// whatever the previous ingest knew while the fields this ingest did set win.
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

// --- the TVDB supplement ---

// applyTVDBMovieSupplement fills the gaps a TMDB movie record left, and records
// the TVDB id link.
//
// It deliberately does NOT overwrite anything TMDB supplied: certification,
// trailer and slug are fallbacks, and the numbers stay TMDB's. TVDB's
// contribution to a movie is the id every TVDB-keyed client speaks, its raw
// record under metadata.tvdb, and a value for any field TMDB answered empty.
func applyTVDBMovieSupplement(record *database.Movie, s *tvdb.MovieExtended) {
	if record.TvdbID == 0 {
		record.TvdbID = s.ID
	}
	if record.ImdbID == "" {
		record.ImdbID = s.IMDBID()
	}
	if record.Slug == "" {
		record.Slug = s.Slug
	}
	if record.Certification == "" {
		record.Certification = s.Certification()
	}
	if record.TrailerURL == "" {
		record.TrailerURL = s.TrailerURL()
	}
	if record.Runtime == 0 {
		record.Runtime = s.Runtime
	}
	if record.Overview == "" {
		record.Overview = s.Overview()
	}
	if record.Tagline == "" {
		record.Tagline = s.Tagline()
	}
	if len(record.Genres) == 0 {
		record.Genres = database.StringSlice(s.GenreNames())
	}
	if record.Status == "" {
		record.Status = s.Status.Name
	}
	if record.Released == nil {
		record.Released = parseDate(s.ReleasedDate())
	}
}

// applyTVDBShowSupplement is applyTVDBMovieSupplement for a series.
func applyTVDBShowSupplement(record *database.Show, s *tvdb.SeriesExtended) {
	if record.TvdbID == 0 {
		record.TvdbID = s.ID
	}
	if record.ImdbID == "" {
		record.ImdbID = s.IMDBID()
	}
	if record.Slug == "" {
		record.Slug = s.Slug
	}
	if record.Certification == "" {
		record.Certification = s.Certification()
	}
	if record.Overview == "" {
		record.Overview = s.Overview
	}
	if record.Network == "" {
		record.Network = s.Network()
	}
	if len(record.Genres) == 0 {
		record.Genres = database.StringSlice(s.GenreNames())
	}
	if record.Status == "" {
		record.Status = s.Status.Name
	}
	if record.FirstAired == nil {
		record.FirstAired = parseDate(s.FirstAired)
	}
}

// --- artwork ---

// movieImages builds the movie image set.
//
// TMDB is the primary artwork source: its poster/backdrop paths are joined into
// absolute CDN URLs and stored under `poster`/`backdrop`, which is what every
// client and the web app read. The raw paths are kept alongside them
// (`poster_path`, `poster_path`) for clients that build TMDB URLs themselves.
// TVDB's absolute artwork is kept as `tvdb_poster`/`tvdb_backdrop`, and is only
// promoted into `poster`/`backdrop` when TMDB has none for the title.
func movieImages(m *tmdb.Movie, supplement *tvdb.MovieExtended) database.JSONMap {
	images := database.JSONMap{}
	poster, backdrop := "", ""

	if m.PosterPath != "" {
		poster = tmdb.ImageURL(m.PosterPath, tmdb.ImageSizePoster)
		images["poster_path"] = m.PosterPath
	}
	if m.BackdropPath != "" {
		backdrop = tmdb.ImageURL(m.BackdropPath, tmdb.ImageSizeBackdrop)
		images["backdrop_path"] = m.BackdropPath
	}

	if supplement != nil {
		tvdbPoster, tvdbBackdrop := supplement.Poster(), supplement.Backdrop()
		if tvdbPoster != "" {
			images["tvdb_poster"] = tvdbPoster
			if poster == "" {
				poster = tvdbPoster
			}
		}
		if tvdbBackdrop != "" {
			images["tvdb_backdrop"] = tvdbBackdrop
			if backdrop == "" {
				backdrop = tvdbBackdrop
			}
		}
		if supplement.Image != "" {
			images["tvdb_image"] = supplement.Image
		}
	}

	if poster != "" {
		images["poster"] = poster
	}
	if backdrop != "" {
		images["backdrop"] = backdrop
	}
	return images
}

// seriesImages is movieImages for a series, plus the clear logo.
func seriesImages(s *tmdb.Show, supplement *tvdb.SeriesExtended) database.JSONMap {
	images := database.JSONMap{}
	poster, backdrop := "", ""

	if s.PosterPath != "" {
		poster = tmdb.ImageURL(s.PosterPath, tmdb.ImageSizePoster)
		images["poster_path"] = s.PosterPath
	}
	if s.BackdropPath != "" {
		backdrop = tmdb.ImageURL(s.BackdropPath, tmdb.ImageSizeBackdrop)
		images["backdrop_path"] = s.BackdropPath
	}

	if supplement != nil {
		tvdbPoster, tvdbBackdrop := supplement.Poster(), supplement.Backdrop()
		if tvdbPoster != "" {
			images["tvdb_poster"] = tvdbPoster
			if poster == "" {
				poster = tvdbPoster
			}
		}
		if tvdbBackdrop != "" {
			images["tvdb_backdrop"] = tvdbBackdrop
			if backdrop == "" {
				backdrop = tvdbBackdrop
			}
		}
		if logo := supplement.Logo(); logo != "" {
			images["logo"] = logo
		}
		if supplement.Image != "" {
			images["tvdb_image"] = supplement.Image
		}
	}

	if poster != "" {
		images["poster"] = poster
	}
	if backdrop != "" {
		images["backdrop"] = backdrop
	}
	return images
}

// --- the raw record blob ---

// movieMetadata stores the raw provider records under namespaced keys, so the
// shape each ingest read is auditable from the row itself. The TMDB half is
// what the primary ingest read; the TVDB half (including its `score`, which is
// not a 0-10 rating and is deliberately not written over vote_average) is only
// present when a supplement was fetched.
func movieMetadata(m *tmdb.Movie, supplement *tvdb.MovieExtended) database.JSONMap {
	metadata := database.JSONMap{"tmdb": toJSONMap(m)}
	if supplement != nil {
		metadata["tvdb"] = toJSONMap(supplement)
	}
	return metadata
}

// showMetadata is movieMetadata for a series.
func showMetadata(s *tmdb.Show, supplement *tvdb.SeriesExtended) database.JSONMap {
	metadata := database.JSONMap{"tmdb": toJSONMap(s)}
	if supplement != nil {
		metadata["tvdb"] = toJSONMap(supplement)
	}
	return metadata
}
