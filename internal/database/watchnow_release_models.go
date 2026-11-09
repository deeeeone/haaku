package database

import "time"

// MovieRelease is one dated release of a movie in one country (issue #69).
//
// It is the queryable mirror of movies.metadata["releases"]: the ingest writes
// both from the same TMDB payload, and the backfill rebuilds these rows from
// the stored metadata without a provider call. The unique key keeps a repeated
// TMDB row from duplicating a release.
type MovieRelease struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	MovieID       int64     `gorm:"not null;uniqueIndex:idx_movie_releases_unique,priority:1;index" json:"movie_id"`
	Country       string    `gorm:"type:text;not null;uniqueIndex:idx_movie_releases_unique,priority:2" json:"country"`
	ReleaseType   string    `gorm:"type:text;not null;uniqueIndex:idx_movie_releases_unique,priority:3;index:idx_movie_releases_type_date,priority:1" json:"release_type"`
	ReleaseDate   Date      `gorm:"type:date;not null;uniqueIndex:idx_movie_releases_unique,priority:4;index:idx_movie_releases_type_date,priority:2" json:"release_date"`
	Certification string    `gorm:"type:text;not null;default:''" json:"certification"`
	Note          string    `gorm:"type:text;not null;default:''" json:"note"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (MovieRelease) TableName() string { return "movie_releases" }

// WatchNowSource is one provider's availability in one country (issue #69).
//
// The catalogue is global metadata rather than a per-title offer: TMDB names a
// provider and its per-country display priority. media_type records which
// catalogue (movie or tv) contributed the row so a refresh can replace each
// half independently; the read route merges them.
type WatchNowSource struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	MediaType  string    `gorm:"type:text;not null;uniqueIndex:idx_watchnow_sources_unique,priority:1" json:"media_type"`
	ProviderID int       `gorm:"not null;uniqueIndex:idx_watchnow_sources_unique,priority:2" json:"provider_id"`
	Source     string    `gorm:"type:text;not null" json:"source"`
	Name       string    `gorm:"type:text;not null" json:"name"`
	Country    string    `gorm:"type:text;not null;uniqueIndex:idx_watchnow_sources_unique,priority:3;index:idx_watchnow_sources_country,priority:1" json:"country"`
	Priority   int       `gorm:"not null;default:0;index:idx_watchnow_sources_country,priority:2" json:"priority"`
	LogoPath   string    `gorm:"type:text;not null;default:''" json:"logo_path"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (WatchNowSource) TableName() string { return "watchnow_sources" }

// WatchNowSourceModels returns the catalogue and release models for
// AutoMigrate in the sqlite-backed test suites.
func WatchNowSourceModels() []interface{} {
	return []interface{}{&MovieRelease{}, &WatchNowSource{}}
}
