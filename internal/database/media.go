package database

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// JSONMap is a map[string]any that implements sql.Scanner and driver.Valuer
// for GORM compatibility with both PostgreSQL jsonb and SQLite TEXT columns.
type JSONMap map[string]any

func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("JSONMap.Scan: unsupported type %T", value)
	}
	if len(bytes) == 0 {
		*j = nil
		return nil
	}
	return json.Unmarshal(bytes, j)
}

// StringSlice is a []string that implements sql.Scanner and driver.Valuer
// for GORM compatibility with both PostgreSQL jsonb arrays and SQLite TEXT columns.
type StringSlice []string

func (s StringSlice) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}

func (s *StringSlice) Scan(value interface{}) error {
	if value == nil {
		*s = nil
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("StringSlice.Scan: unsupported type %T", value)
	}
	if len(bytes) == 0 {
		*s = nil
		return nil
	}
	return json.Unmarshal(bytes, (*[]string)(s))
}

// --- Media (cached from TMDB — our source of truth after ingest) ---
// See ../trakt-killer-plan/02-architecture/data-model.md §2.

// Movie mirrors TMDB movie detail with our community fields.
type Movie struct {
	ID            int64       `gorm:"primaryKey" json:"id"`
	TmdbID        int         `gorm:"uniqueIndex" json:"tmdb_id"`
	ImdbID        string      `json:"imdb_id,omitempty"`
	Title         string      `json:"title"`
	OriginalTitle string      `json:"original_title"`
	Tagline       string      `json:"tagline,omitempty"`
	Overview      string      `json:"overview,omitempty"`
	Released      *Date       `json:"released,omitempty"`
	Runtime       int         `json:"runtime,omitempty"`
	Certification string      `json:"certification,omitempty"`
	Genres        StringSlice `gorm:"type:jsonb;default:'[]'" json:"genres"`
	Language      string      `json:"language,omitempty"`
	Status        string      `json:"status,omitempty"`
	Popularity    float64     `json:"popularity"`
	RatingAvg     float64     `json:"rating_avg"`
	RatingCount   int64       `json:"rating_count"`
	TrailerURL    string      `json:"trailer_url,omitempty"`
	Images        JSONMap     `gorm:"type:jsonb;default:'{}'" json:"images"`
	Metadata      JSONMap     `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	Adult         bool        `json:"adult"`
	LastSyncedAt  *time.Time  `json:"last_synced_at,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

func (Movie) TableName() string { return "movies" }

// Show mirrors TMDB TV show detail.
type Show struct {
	ID            int64       `gorm:"primaryKey" json:"id"`
	TmdbID        int         `gorm:"uniqueIndex" json:"tmdb_id"`
	ImdbID        string      `json:"imdb_id,omitempty"`
	Title         string      `json:"title"`
	OriginalTitle string      `json:"original_title"`
	Overview      string      `json:"overview,omitempty"`
	FirstAired    *Date       `json:"first_aired,omitempty"`
	LastAired     *Date       `json:"last_aired,omitempty"`
	EpisodeCount  int         `json:"episode_count"`
	SeasonCount   int         `json:"season_count"`
	Network       string      `json:"network,omitempty"`
	Status        string      `json:"status,omitempty"`
	Popularity    float64     `json:"popularity"`
	RatingAvg     float64     `json:"rating_avg"`
	RatingCount   int64       `json:"rating_count"`
	Genres        StringSlice `gorm:"type:jsonb;default:'[]'" json:"genres"`
	Language      string      `json:"language,omitempty"`
	Certification string      `json:"certification,omitempty"`
	Images        JSONMap     `gorm:"type:jsonb;default:'{}'" json:"images"`
	Metadata      JSONMap     `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	Adult         bool        `json:"adult"`
	SearchVector  string      `gorm:"->" json:"-"`
	LastSyncedAt  *time.Time  `json:"last_synced_at,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

func (Show) TableName() string { return "shows" }

// Season belongs to a show.
type Season struct {
	ID           int64      `gorm:"primaryKey" json:"id"`
	ShowID       int64      `gorm:"index" json:"show_id"`
	TmdbShowID   int        `json:"tmdb_show_id"`
	SeasonNumber int        `json:"season_number"`
	EpisodeCount int        `json:"episode_count"`
	AiredAt      *Date      `json:"aired_at,omitempty"`
	Images       JSONMap    `gorm:"type:jsonb;default:'{}'" json:"images"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (Season) TableName() string { return "seasons" }

// Episode belongs to a season and show.
type Episode struct {
	ID             int64      `gorm:"primaryKey" json:"id"`
	SeasonID       int64      `gorm:"uniqueIndex:idx_episode_show_season_ep,priority:2" json:"season_id"`
	ShowID         int64      `gorm:"uniqueIndex:idx_episode_show_season_ep,priority:1" json:"show_id"`
	EpisodeNumber  int        `gorm:"uniqueIndex:idx_episode_show_season_ep,priority:3" json:"episode_number"`
	AbsoluteNumber *int       `json:"absolute_number,omitempty"`
	Title          string     `json:"title"`
	Overview       string     `json:"overview,omitempty"`
	Runtime        int        `json:"runtime,omitempty"`
	AiredAt        *Date      `json:"aired_at,omitempty"`
	Images         JSONMap    `gorm:"type:jsonb;default:'{}'" json:"images"`
	SearchVector   string     `gorm:"->" json:"-"`
	LastSyncedAt   *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (Episode) TableName() string { return "episodes" }

// Person mirrors TMDB person detail.
type Person struct {
	ID                 int64      `gorm:"primaryKey" json:"id"`
	TmdbID             int        `gorm:"uniqueIndex" json:"tmdb_id"`
	ImdbID             string     `json:"imdb_id,omitempty"`
	Name               string     `json:"name"`
	Biography          string     `json:"biography,omitempty"`
	Birthday           *Date      `json:"birthday,omitempty"`
	Deathday           *Date      `json:"deathday,omitempty"`
	Gender             int        `json:"gender"`
	KnownForDepartment string     `json:"known_for_department,omitempty"`
	Images             JSONMap    `gorm:"type:jsonb;default:'{}'" json:"images"`
	Adult              bool       `json:"adult"`
	SearchVector       string     `gorm:"->" json:"-"`
	LastSyncedAt       *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (Person) TableName() string { return "people" }

// Credit links a person to a media title.
type Credit struct {
	ID            int64  `gorm:"primaryKey" json:"id"`
	PersonID      int64  `gorm:"index" json:"person_id"`
	MediaType     string `gorm:"index" json:"media_type"` // movie or show
	MediaID       int64  `gorm:"index" json:"media_id"`
	CreditType    string `json:"credit_type"` // cast or crew
	Job           string `json:"job,omitempty"`
	Character     string `gorm:"column:character_name" json:"character,omitempty"`
	Department    string `json:"department,omitempty"`
	Order         int    `json:"order"`
	SeriesRegular bool   `json:"series_regular"`
	EpisodeCount  int    `json:"episode_count,omitempty"`
}

func (Credit) TableName() string { return "credits" }

// ExternalID maps an external identifier (IMDB, TVDB, TVRage, Trakt, slug) to
// one of our canonical media rows. See data-model.md §2 for the full intent.
// The media_type/media_id pattern mirrors Trakt's polymorphic id lookups.
type ExternalID struct {
	ID         int64  `gorm:"primaryKey" json:"id"`
	MediaType  string `gorm:"index" json:"media_type"`
	MediaID    int64  `gorm:"index" json:"media_id"`
	Source     string `gorm:"index" json:"source"` // imdb / tvdb / tmdb / tvrage / trakt / slug
	ExternalID string `json:"external_id"`
}

func (ExternalID) TableName() string { return "external_ids" }

// --- Supporting types ---

// Genre, Certification, Network are small reference tables synced from TMDB.

type Genre struct {
	ID   int64  `gorm:"primaryKey;autoIncrement:false" json:"id"`
	Name string `json:"name"`
}

func (Genre) TableName() string { return "genres" }

type Network struct {
	ID   int64  `gorm:"primaryKey;autoIncrement:false" json:"id"`
	Name string `json:"name"`
}

func (Network) TableName() string { return "networks" }

// Date is a custom type for parsing TMDB's "YYYY-MM-DD" strings from JSON
// without a time component.
type Date struct{ time.Time }

func (d *Date) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" || s == `""` || s == "" {
		return nil
	}
	s = s[1 : len(s)-1] // strip quotes
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return err
	}
	d.Time = t
	return nil
}

func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + d.Format("2006-01-02") + `"`), nil
}

func (d *Date) String() string {
	if d == nil || d.IsZero() {
		return ""
	}
	return d.Format("2006-01-02")
}

// Value implements the driver.Valuer interface for database storage.
func (d Date) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, nil
	}
	return d.Format("2006-01-02"), nil
}

// Scan implements the sql.Scanner interface for reading from the database.
func (d *Date) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		d.Time = v
	case string:
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return err
		}
		d.Time = t
	case []byte:
		t, err := time.Parse("2006-01-02", string(v))
		if err != nil {
			return err
		}
		d.Time = t
	default:
		return fmt.Errorf("unsupported type for Date.Scan: %T", value)
	}
	return nil
}

// Ensure AutoMigrate picks up all media models (called from db.go InitDB).
func MediaModels() []interface{} {
	return []interface{}{
		&Movie{}, &Show{}, &Season{}, &Episode{},
		&Person{}, &Credit{}, &ExternalID{},
		&Genre{}, &Network{},
	}
}
