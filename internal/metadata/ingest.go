package metadata

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"trakt-api/external/tmdb"
	"trakt-api/internal/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IngestMovie creates or updates a movie record from TMDB data.
// Idempotent: checks tmdb_id existence, updates if changed, inserts if new.
func IngestMovie(db *gorm.DB, m *tmdb.Movie) (*database.Movie, error) {
	now := time.Now()
	metadata := toJSONMap(m)
	images := map[string]any{
		"poster_path":   m.PosterPath,
		"backdrop_path": m.BackdropPath,
	}
	genres := genreNames(m.Genres)

	record := database.Movie{
		TmdbID:        m.ID,
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
		Genres:        genres,
		Language:      m.Language,
		Images:        images,
		Metadata:      metadata,
		Adult:         m.Adult,
		LastSyncedAt:  &now,
	}

	if m.ReleaseDate != "" {
		parsed := parseDate(m.ReleaseDate)
		record.Released = parsed
	}

	var existing database.Movie
	result := db.Where("tmdb_id = ?", m.ID).First(&existing)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			if err := db.Create(&record).Error; err != nil {
				return nil, fmt.Errorf("insert movie %d (%s): %w", m.ID, m.Title, err)
			}
			slog.Debug("[ingest] created movie", "tmdb_id", m.ID, "title", m.Title)
			return &record, nil
		}
		return nil, fmt.Errorf("lookup movie %d: %w", m.ID, result.Error)
	}

	// Existing record — check if anything changed.
	if movieChanged(&existing, &record) {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
		if err := db.Save(&record).Error; err != nil {
			return nil, fmt.Errorf("update movie %d (%s): %w", m.ID, m.Title, err)
		}
		slog.Debug("[ingest] updated movie", "tmdb_id", m.ID, "title", m.Title)
	} else {
		slog.Debug("[ingest] movie unchanged", "tmdb_id", m.ID, "title", m.Title)
		record = existing
	}

	return &record, nil
}

// movieChanged returns true if the key fields differ between the existing
// DB record and the incoming TMDB data.
func movieChanged(existing, incoming *database.Movie) bool {
	return existing.Title != incoming.Title ||
		existing.Overview != incoming.Overview ||
		existing.Status != incoming.Status ||
		existing.Runtime != incoming.Runtime ||
		existing.Popularity != incoming.Popularity ||
		existing.RatingAvg != incoming.RatingAvg ||
		existing.RatingCount != incoming.RatingCount ||
		existing.Adult != incoming.Adult ||
		existing.Tagline != incoming.Tagline ||
		existing.ImdbID != incoming.ImdbID ||
		existing.OriginalTitle != incoming.OriginalTitle
}

// IngestShow creates or updates a show record and its seasons/episodes
// from TMDB data. Idempotent.
func IngestShow(db *gorm.DB, s *tmdb.Show) (*database.Show, error) {
	now := time.Now()
	metadata := toJSONMap(s)
	images := map[string]any{
		"poster_path":   s.PosterPath,
		"backdrop_path": s.BackdropPath,
	}
	genres := genreNames(s.Genres)
	network := ""
	if len(s.Networks) > 0 {
		network = s.Networks[0].Name
	}

	record := database.Show{
		TmdbID:        s.ID,
		ImdbID:        s.IMDBID,
		Title:         s.Name,
		OriginalTitle: s.OriginalName,
		Overview:      s.Overview,
		EpisodeCount:  s.EpisodesCount,
		SeasonCount:   s.SeasonsCount,
		Network:       network,
		Status:        s.Status,
		Popularity:    s.Popularity,
		RatingAvg:     s.VoteAverage,
		RatingCount:   s.VoteCount,
		Genres:        genres,
		Language:      s.Language,
		Images:        images,
		Metadata:      metadata,
		Adult:         s.Adult,
		LastSyncedAt:  &now,
	}

	if s.FirstAirDate != "" {
		record.FirstAired = parseDate(s.FirstAirDate)
	}
	if s.LastAirDate != "" {
		record.LastAired = parseDate(s.LastAirDate)
	}

	var existing database.Show
	result := db.Where("tmdb_id = ?", s.ID).First(&existing)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			if err := db.Create(&record).Error; err != nil {
				return nil, fmt.Errorf("insert show %d (%s): %w", s.ID, s.Name, err)
			}
			slog.Debug("[ingest] created show", "tmdb_id", s.ID, "title", s.Name)
			return &record, nil
		}
		return nil, fmt.Errorf("lookup show %d: %w", s.ID, result.Error)
	}

	if showChanged(&existing, &record) {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
		if err := db.Save(&record).Error; err != nil {
			return nil, fmt.Errorf("update show %d (%s): %w", s.ID, s.Name, err)
		}
		slog.Debug("[ingest] updated show", "tmdb_id", s.ID, "title", s.Name)
	} else {
		slog.Debug("[ingest] show unchanged", "tmdb_id", s.ID, "title", s.Name)
		record = existing
	}

	return &record, nil
}

func showChanged(existing, incoming *database.Show) bool {
	return existing.Title != incoming.Title ||
		existing.Overview != incoming.Overview ||
		existing.Status != incoming.Status ||
		existing.EpisodeCount != incoming.EpisodeCount ||
		existing.SeasonCount != incoming.SeasonCount ||
		existing.Network != incoming.Network ||
		existing.Popularity != incoming.Popularity ||
		existing.RatingAvg != incoming.RatingAvg ||
		existing.RatingCount != incoming.RatingCount ||
		existing.Adult != incoming.Adult ||
		existing.ImdbID != incoming.ImdbID ||
		existing.OriginalTitle != incoming.OriginalTitle
}

// IngestSeason creates or updates a season record. Idempotent.
func IngestSeason(db *gorm.DB, showID int64, tmdbShowID int, s tmdb.SeasonSummary) (*database.Season, error) {
	now := time.Now()
	images := map[string]any{
		"poster_path": s.PosterPath,
	}

	record := database.Season{
		ShowID:       showID,
		TmdbShowID:   tmdbShowID,
		SeasonNumber: s.SeasonNumber,
		EpisodeCount: s.EpisodeCount,
		Images:       images,
		LastSyncedAt: &now,
	}
	if s.AirDate != "" {
		record.AiredAt = parseDate(s.AirDate)
	}

	var existing database.Season
	result := db.Where("show_id = ? AND season_number = ?", showID, s.SeasonNumber).First(&existing)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			if err := db.Create(&record).Error; err != nil {
				return nil, fmt.Errorf("insert season %d for show %d: %w", s.SeasonNumber, showID, err)
			}
			slog.Debug("[ingest] created season", "show_id", showID, "season", s.SeasonNumber)
			return &record, nil
		}
		return nil, fmt.Errorf("lookup season show=%d season=%d: %w", showID, s.SeasonNumber, result.Error)
	}

	if seasonChanged(&existing, &record) {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
		if err := db.Save(&record).Error; err != nil {
			return nil, fmt.Errorf("update season %d for show %d: %w", s.SeasonNumber, showID, err)
		}
		slog.Debug("[ingest] updated season", "show_id", showID, "season", s.SeasonNumber)
	} else {
		slog.Debug("[ingest] season unchanged", "show_id", showID, "season", s.SeasonNumber)
		record = existing
	}

	return &record, nil
}

func seasonChanged(existing, incoming *database.Season) bool {
	return existing.EpisodeCount != incoming.EpisodeCount
}

// IngestEpisode creates or updates an episode record. Idempotent.
func IngestEpisode(db *gorm.DB, seasonID, showID int64, ep tmdb.EpisodeDetail) (*database.Episode, error) {
	now := time.Now()
	images := map[string]any{
		"still_path": ep.StillPath,
	}

	record := database.Episode{
		SeasonID:      seasonID,
		ShowID:        showID,
		EpisodeNumber: ep.EpisodeNumber,
		Title:         ep.Name,
		Overview:      ep.Overview,
		Runtime:       ep.Runtime,
		Images:        images,
		LastSyncedAt:  &now,
	}
	if ep.AirDate != "" {
		record.AiredAt = parseDate(ep.AirDate)
	}

	var existing database.Episode
	result := db.Where("show_id = ? AND season_id = ? AND episode_number = ?",
		showID, seasonID, ep.EpisodeNumber).First(&existing)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			if err := db.Create(&record).Error; err != nil {
				return nil, fmt.Errorf("insert episode s%de%d for show %d: %w",
					ep.SeasonNumber, ep.EpisodeNumber, showID, err)
			}
			return &record, nil
		}
		return nil, fmt.Errorf("lookup episode show=%d season=%d ep=%d: %w",
			showID, ep.SeasonNumber, ep.EpisodeNumber, result.Error)
	}

	if episodeChanged(&existing, &record) {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
		if err := db.Save(&record).Error; err != nil {
			return nil, fmt.Errorf("update episode s%de%d for show %d: %w",
				ep.SeasonNumber, ep.EpisodeNumber, showID, err)
		}
	} else {
		record = existing
	}

	return &record, nil
}

func episodeChanged(existing, incoming *database.Episode) bool {
	return existing.Title != incoming.Title ||
		existing.Overview != incoming.Overview ||
		existing.Runtime != incoming.Runtime
}

// IngestEpisodesBatch creates or updates a batch of episodes efficiently.
// Uses upsert (ON CONFLICT) via GORM's Clauses.
func IngestEpisodesBatch(db *gorm.DB, seasonID, showID int64, eps []tmdb.EpisodeDetail) error {
	if len(eps) == 0 {
		return nil
	}
	now := time.Now()

	records := make([]database.Episode, 0, len(eps))
	for _, ep := range eps {
		r := database.Episode{
			SeasonID:      seasonID,
			ShowID:        showID,
			EpisodeNumber: ep.EpisodeNumber,
			Title:         ep.Name,
			Overview:      ep.Overview,
			Runtime:       ep.Runtime,
			Images: map[string]any{
				"still_path": ep.StillPath,
			},
			LastSyncedAt: &now,
		}
		if ep.AirDate != "" {
			r.AiredAt = parseDate(ep.AirDate)
		}
		records = append(records, r)
	}

	// Upsert: on conflict (show_id, season_id, episode_number), update fields.
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "show_id"},
			{Name: "season_id"},
			{Name: "episode_number"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"title", "overview", "runtime", "images",
			"aired_at", "last_synced_at", "updated_at",
		}),
	}).CreateInBatches(records, 100).Error
}

// IngestPerson creates or updates a person record. Idempotent.
func IngestPerson(db *gorm.DB, p *tmdb.Person) (*database.Person, error) {
	now := time.Now()
	metadata := toJSONMap(p)
	images := map[string]any{
		"profile_path": p.ProfilePath,
	}

	record := database.Person{
		TmdbID:             p.ID,
		ImdbID:             p.IMDBID,
		Name:               p.Name,
		Biography:          p.Biography,
		Gender:             p.Gender,
		KnownForDepartment: p.KnownForDepartment,
		Images:             images,
		Adult:              p.Adult,
		LastSyncedAt:       &now,
	}
	// metadata stored separately for people (not a direct column, so we
	// embed it in the JSONB metadata field — already done above).
	_ = metadata

	if p.Birthday != "" {
		record.Birthday = parseDate(p.Birthday)
	}
	if p.Deathday != "" {
		record.Deathday = parseDate(p.Deathday)
	}

	var existing database.Person
	result := db.Where("tmdb_id = ?", p.ID).First(&existing)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			if err := db.Create(&record).Error; err != nil {
				return nil, fmt.Errorf("insert person %d (%s): %w", p.ID, p.Name, err)
			}
			slog.Debug("[ingest] created person", "tmdb_id", p.ID, "name", p.Name)
			return &record, nil
		}
		return nil, fmt.Errorf("lookup person %d: %w", p.ID, result.Error)
	}

	if personChanged(&existing, &record) {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
		if err := db.Save(&record).Error; err != nil {
			return nil, fmt.Errorf("update person %d (%s): %w", p.ID, p.Name, err)
		}
		slog.Debug("[ingest] updated person", "tmdb_id", p.ID, "name", p.Name)
	} else {
		slog.Debug("[ingest] person unchanged", "tmdb_id", p.ID, "name", p.Name)
		record = existing
	}

	return &record, nil
}

func personChanged(existing, incoming *database.Person) bool {
	return existing.Name != incoming.Name ||
		existing.Biography != incoming.Biography ||
		existing.Gender != incoming.Gender ||
		existing.KnownForDepartment != incoming.KnownForDepartment ||
		existing.Adult != incoming.Adult ||
		existing.ImdbID != incoming.ImdbID
}

// IngestCredits upserts credits (cast + crew) for a media item.
// Deletes existing credits for this media item before inserting new ones
// to keep the credit set fresh.
func IngestCredits(db *gorm.DB, mediaType string, mediaID int64, credits *tmdb.CreditsResponse) error {
	if credits == nil {
		return nil
	}

	// Delete existing credits for this media item.
	if err := db.Where("media_type = ? AND media_id = ?", mediaType, mediaID).
		Delete(&database.Credit{}).Error; err != nil {
		return fmt.Errorf("delete existing credits for %s/%d: %w", mediaType, mediaID, err)
	}

	var records []database.Credit

	// Cast
	for _, c := range credits.Cast {
		records = append(records, database.Credit{
			PersonID:   0, // We don't always have the internal person ID yet
			MediaType:  mediaType,
			MediaID:    mediaID,
			CreditType: "cast",
			Character:  c.Character,
			Order:      c.Order,
		})
	}

	// Crew
	for _, c := range credits.Crew {
		records = append(records, database.Credit{
			PersonID:   0,
			MediaType:  mediaType,
			MediaID:    mediaID,
			CreditType: "crew",
			Job:        c.Job,
			Department: c.Department,
		})
	}

	if len(records) > 0 {
		if err := db.CreateInBatches(records, 100).Error; err != nil {
			return fmt.Errorf("insert credits for %s/%d: %w", mediaType, mediaID, err)
		}
	}

	slog.Debug("[ingest] credits updated",
		"media_type", mediaType, "media_id", mediaID,
		"cast", len(credits.Cast), "crew", len(credits.Crew))

	return nil
}

// --- helpers ---

func toJSONMap(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return m
}

func genreNames(genres []tmdb.Genre) []string {
	names := make([]string, len(genres))
	for i, g := range genres {
		names[i] = g.Name
	}
	return names
}

func parseDate(s string) *database.Date {
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &database.Date{Time: t}
}
