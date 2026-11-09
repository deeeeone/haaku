package metadata

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This file holds the record-level writers that are NOT part of the movie/show
// composite ingest in provider_ingest.go: a season on its own, an episode, a
// batch of episodes, a person, and a credit set. IngestMovie/IngestShow own the
// whole title (row, ids, artwork, seasons and episodes) and call IngestSeason /
// IngestEpisodesBatch for the structural half; the person and credit writers are
// the pieces issue #68 wires into the resolve path.

// IngestSeason creates or updates a season record. Idempotent.
//
// TMDB's season detail carries a poster path and an air date, but a caller that
// only has the show's season SUMMARY may hold neither, and db.Save writes every
// column — so an existing row's artwork and air date are carried forward rather
// than blanked when the incoming value is empty.
func IngestSeason(db *gorm.DB, showID int64, tmdbShowID int, s tmdb.SeasonSummary) (*database.Season, error) {
	now := time.Now()
	images := map[string]any{}
	if s.PosterPath != "" {
		images["poster_path"] = s.PosterPath
		images["poster"] = tmdb.ImageURL(s.PosterPath, tmdb.ImageSizePoster)
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

	if len(images) == 0 {
		record.Images = existing.Images
	}
	if record.AiredAt == nil {
		record.AiredAt = existing.AiredAt
	}
	// metadata carries the hydrated videos/credits marker; db.Save writes every
	// column, so a re-ingest must carry the stored blob forward rather than
	// blanking it (issue #68).
	if len(record.Metadata) == 0 {
		record.Metadata = existing.Metadata
	}

	if seasonChanged(&existing, &record) || !sameJSONMap(existing.Images, record.Images) || !sameDate(existing.AiredAt, record.AiredAt) {
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

// sameJSONMap reports whether two image maps carry the same values. A Go map
// cannot be compared with ==, and comparing the rendered form is enough here:
// the maps are small, flat and built by this package.
func sameJSONMap(a, b database.JSONMap) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		other, ok := b[key]
		if !ok || fmt.Sprint(other) != fmt.Sprint(value) {
			return false
		}
	}
	return true
}

// sameDate compares two optional dates by the day they name.
func sameDate(a, b *database.Date) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(b.Time)
}

// IngestEpisode creates or updates an episode record. Idempotent.
func IngestEpisode(db *gorm.DB, seasonID, showID int64, ep tmdb.EpisodeDetail) (*database.Episode, error) {
	now := time.Now()

	record := database.Episode{
		SeasonID:      seasonID,
		ShowID:        showID,
		EpisodeNumber: ep.EpisodeNumber,
		Title:         ep.Name,
		Overview:      ep.Overview,
		Runtime:       ep.Runtime,
		Images:        episodeImages(ep.StillPath),
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
		if len(record.Metadata) == 0 {
			record.Metadata = existing.Metadata
		}
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

// episodeImages builds an episode's image set from a TMDB still path. The path
// is kept raw for clients that build TMDB URLs themselves, and joined into
// `still` — and into `poster`, which is the key the web app's episode cards and
// the season page read for an episode's thumbnail (the media response passes
// the map through untouched).
func episodeImages(stillPath string) database.JSONMap {
	images := database.JSONMap{}
	if stillPath == "" {
		return images
	}
	still := tmdb.ImageURL(stillPath, tmdb.ImageSizeStill)
	images["still_path"] = stillPath
	images["still"] = still
	images["still_url"] = still
	images["poster"] = still
	return images
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
			Images:        episodeImages(ep.StillPath),
			LastSyncedAt:  &now,
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
//
// Each credit's TMDB person is ingested first, because a credit row with
// PersonID 0 has nobody to render: the people read skips it. The existing set
// for this media item is replaced, so a credit list TMDB no longer carries does
// not linger. mediaType is "movie", "show", "season" or "episode".
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
		personID, err := ingestCreditPerson(db, c.ID, c.Name, c.ProfilePath, c.Gender, c.KnownForDepartment, c.Adult)
		if err != nil {
			return err
		}
		records = append(records, database.Credit{
			PersonID:   personID,
			MediaType:  mediaType,
			MediaID:    mediaID,
			CreditType: "cast",
			Character:  c.Character,
			Order:      c.Order,
		})
	}

	// Crew
	for _, c := range credits.Crew {
		personID, err := ingestCreditPerson(db, c.ID, c.Name, c.ProfilePath, c.Gender, c.KnownForDepartment, c.Adult)
		if err != nil {
			return err
		}
		records = append(records, database.Credit{
			PersonID:   personID,
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

// ingestCreditPerson upserts the person behind a credit and returns the local
// row id. A credit TMDB gives no id for cannot be linked, so it keeps the old
// PersonID 0 and the people read skips it rather than rendering a ghost.
func ingestCreditPerson(db *gorm.DB, tmdbID int, name, profilePath string, gender int, knownForDepartment string, adult bool) (int64, error) {
	if tmdbID <= 0 || name == "" {
		return 0, nil
	}
	person, err := IngestPerson(db, &tmdb.Person{
		ID:                 tmdbID,
		Name:               name,
		ProfilePath:        profilePath,
		Gender:             gender,
		KnownForDepartment: knownForDepartment,
		Adult:              adult,
	})
	if err != nil {
		return 0, fmt.Errorf("ingest credit person %d (%s): %w", tmdbID, name, err)
	}
	return person.ID, nil
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
