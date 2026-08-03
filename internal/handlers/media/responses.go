package media

import (
	"encoding/json"

	"trakt-api/internal/database"

	"github.com/gofiber/fiber/v2"
)

// --- Response types for media API ---

// IDs is the standard ID object returned with every media response.
type IDs struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug,omitempty"`
	TMDB int    `json:"tmdb,omitempty"`
	IMDB string `json:"imdb,omitempty"`
	TVDB string `json:"tvdb,omitempty"`
}

// RatingSummary is the community rating summary.
type RatingSummary struct {
	Rating       float64          `json:"rating"`
	Votes        int64            `json:"votes"`
	Distribution map[string]int64 `json:"distribution"`
}

// MovieResponse is the API response shape for a movie.
type MovieResponse struct {
	IDs           IDs            `json:"ids"`
	Title         string         `json:"title"`
	Year          int            `json:"year"`
	Images        map[string]any `json:"images"`
	RatingSummary *RatingSummary `json:"rating_summary,omitempty"`

	// Full extended fields
	Overview      string   `json:"overview,omitempty"`
	Tagline       string   `json:"tagline,omitempty"`
	Runtime       int      `json:"runtime,omitempty"`
	Genres        []string `json:"genres,omitempty"`
	Certification string   `json:"certification,omitempty"`
	Language      string   `json:"language,omitempty"`
	TrailerURL    string   `json:"trailer_url,omitempty"`
	Status        string   `json:"status,omitempty"`

	// Metadata extended
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ShowResponse is the API response shape for a show.
type ShowResponse struct {
	IDs           IDs            `json:"ids"`
	Title         string         `json:"title"`
	Year          int            `json:"year"`
	Images        map[string]any `json:"images"`
	RatingSummary *RatingSummary `json:"rating_summary,omitempty"`

	// Full extended fields
	Overview      string   `json:"overview,omitempty"`
	Runtime       int      `json:"runtime,omitempty"`
	Genres        []string `json:"genres,omitempty"`
	Certification string   `json:"certification,omitempty"`
	Language      string   `json:"language,omitempty"`
	Network       string   `json:"network,omitempty"`
	Status        string   `json:"status,omitempty"`
	EpisodeCount  int      `json:"episode_count,omitempty"`
	SeasonCount   int      `json:"season_count,omitempty"`
	FirstAired    string   `json:"first_aired,omitempty"`
	LastAired     string   `json:"last_aired,omitempty"`

	// Metadata extended
	Metadata map[string]any `json:"metadata,omitempty"`

	// Show always includes seasons array
	Seasons []SeasonResponse `json:"seasons"`
}

// SeasonResponse is the API response shape for a season.
type SeasonResponse struct {
	IDs           IDs            `json:"ids"`
	ShowID        int64          `json:"show_id"`
	SeasonNumber  int            `json:"season_number"`
	EpisodeCount  int            `json:"episode_count"`
	Images        map[string]any `json:"images"`
	RatingSummary *RatingSummary `json:"rating_summary,omitempty"`
	AiredAt       string         `json:"aired_at,omitempty"`
}

// EpisodeResponse is the API response shape for an episode.
type EpisodeResponse struct {
	IDs            IDs            `json:"ids"`
	SeasonID       int64          `json:"season_id"`
	ShowID         int64          `json:"show_id"`
	EpisodeNumber  int            `json:"episode_number"`
	Title          string         `json:"title"`
	Overview       string         `json:"overview,omitempty"`
	Runtime        int            `json:"runtime,omitempty"`
	Images         map[string]any `json:"images"`
	RatingSummary  *RatingSummary `json:"rating_summary,omitempty"`
	AiredAt        string         `json:"aired_at,omitempty"`
	AbsoluteNumber *int           `json:"absolute_number,omitempty"`
}

// PersonResponse is the API response shape for a person.
type PersonResponse struct {
	IDs                IDs            `json:"ids"`
	Name               string         `json:"name"`
	Biography          string         `json:"biography,omitempty"`
	Birthday           string         `json:"birthday,omitempty"`
	Deathday           string         `json:"deathday,omitempty"`
	Gender             int            `json:"gender"`
	KnownForDepartment string         `json:"known_for_department,omitempty"`
	Images             map[string]any `json:"images"`
	Credits            *PersonCredits `json:"credits,omitempty"`
}

// PersonCredits holds movie and show credits for a person.
type PersonCredits struct {
	Cast []CreditResponse `json:"cast,omitempty"`
	Crew []CreditResponse `json:"crew,omitempty"`
}

// CreditResponse is the API response shape for a credit.
type CreditResponse struct {
	MediaType     string `json:"media_type"`
	MediaID       int64  `json:"media_id"`
	Title         string `json:"title,omitempty"`
	Year          int    `json:"year,omitempty"`
	CreditType    string `json:"credit_type"`
	Job           string `json:"job,omitempty"`
	Character     string `json:"character,omitempty"`
	Department    string `json:"department,omitempty"`
	SeriesRegular bool   `json:"series_regular,omitempty"`
	EpisodeCount  int    `json:"episode_count,omitempty"`
}

// PeopleResponse is the response for cast+crew on a movie or show.
type PeopleResponse struct {
	Cast []CastMemberResponse `json:"cast,omitempty"`
	Crew []CrewMemberResponse `json:"crew,omitempty"`
}

// CastMemberResponse is a cast member entry.
type CastMemberResponse struct {
	Person    PersonResponse `json:"person"`
	Character string         `json:"character"`
	Order     int            `json:"order"`
}

// CrewMemberResponse is a crew member entry.
type CrewMemberResponse struct {
	Person     PersonResponse `json:"person"`
	Job        string         `json:"job"`
	Department string         `json:"department"`
}

// StatsResponse is the stats response for a media item.
type StatsResponse struct {
	Watchers   int64 `json:"watchers"`
	Plays      int64 `json:"plays"`
	Collectors int64 `json:"collectors"`
	Lists      int64 `json:"lists"`
}

// WatchingResponse is a stub — returns empty array for now.
type WatchingResponse []interface{}

// ListsResponse is a stub — returns empty array for now.
type ListsResponse []interface{}

// --- Builders ---

// buildIDs builds the IDs object from a media row and external ID lookups.
func buildIDs(mediaType string, mediaID int64, movie *database.Movie, show *database.Show) IDs {
	ids := IDs{ID: mediaID}

	if movie != nil {
		if movie.TmdbID != 0 {
			ids.TMDB = movie.TmdbID
		}
		if movie.ImdbID != "" {
			ids.IMDB = movie.ImdbID
		}
	} else if show != nil {
		if show.TmdbID != 0 {
			ids.TMDB = show.TmdbID
		}
		if show.ImdbID != "" {
			ids.IMDB = show.ImdbID
		}
	}

	// Slug and TVDB come from external_ids table.
	if db, err := database.GetDB(); err == nil {
		var exts []database.ExternalID
		db.Where("media_type = ? AND media_id = ? AND source IN ?",
			mediaType, mediaID, []string{"slug", "tvdb"}).Find(&exts)
		for _, ext := range exts {
			switch ext.Source {
			case "slug":
				ids.Slug = ext.ExternalID
			case "tvdb":
				ids.TVDB = ext.ExternalID
			}
		}
	}

	return ids
}

// BuildMovieResponse builds a MovieResponse from a database Movie (exported for cross-package use).
func BuildMovieResponse(m *database.Movie, extended string) MovieResponse {
	year := 0
	if m.Released != nil && !m.Released.IsZero() {
		year = m.Released.Year()
	}

	resp := MovieResponse{
		IDs:           buildIDs("movie", m.ID, m, nil),
		Title:         m.Title,
		Year:          year,
		Images:        m.Images,
		RatingSummary: buildRatingSummary(m.RatingAvg, m.RatingCount),
	}

	// Apply extended level.
	switch extended {
	case "full", "all":
		resp.Overview = m.Overview
		resp.Tagline = m.Tagline
		resp.Runtime = m.Runtime
		resp.Genres = m.Genres
		resp.Certification = m.Certification
		resp.Language = m.Language
		resp.TrailerURL = m.TrailerURL
		resp.Status = m.Status
		fallthrough
	case "metadata":
		resp.Metadata = m.Metadata
	case "images":
		// Images are always included for now (already sent above).
	}

	// "all" includes everything
	if extended == "all" {
		resp.Overview = m.Overview
		resp.Tagline = m.Tagline
		resp.Runtime = m.Runtime
		resp.Genres = m.Genres
		resp.Certification = m.Certification
		resp.Language = m.Language
		resp.TrailerURL = m.TrailerURL
		resp.Status = m.Status
		resp.Metadata = m.Metadata
	}

	return resp
}

// BuildShowResponse builds a ShowResponse from a database Show (exported for cross-package use).
func BuildShowResponse(s *database.Show, extended string) ShowResponse {
	year := 0
	if s.FirstAired != nil && !s.FirstAired.IsZero() {
		year = s.FirstAired.Year()
	}

	resp := ShowResponse{
		IDs:           buildIDs("show", s.ID, nil, s),
		Title:         s.Title,
		Year:          year,
		Images:        s.Images,
		RatingSummary: buildRatingSummary(s.RatingAvg, s.RatingCount),
	}

	switch extended {
	case "full", "all":
		resp.Overview = s.Overview
		resp.Genres = s.Genres
		resp.Certification = s.Certification
		resp.Language = s.Language
		resp.Network = s.Network
		resp.Status = s.Status
		resp.EpisodeCount = s.EpisodeCount
		resp.SeasonCount = s.SeasonCount
		if s.FirstAired != nil {
			resp.FirstAired = s.FirstAired.String()
		}
		if s.LastAired != nil {
			resp.LastAired = s.LastAired.String()
		}
		fallthrough
	case "metadata":
		resp.Metadata = s.Metadata
	case "images":
		// images always included
	}

	if extended == "all" {
		resp.Overview = s.Overview
		resp.Genres = s.Genres
		resp.Certification = s.Certification
		resp.Language = s.Language
		resp.Network = s.Network
		resp.Status = s.Status
		resp.EpisodeCount = s.EpisodeCount
		resp.SeasonCount = s.SeasonCount
		if s.FirstAired != nil {
			resp.FirstAired = s.FirstAired.String()
		}
		if s.LastAired != nil {
			resp.LastAired = s.LastAired.String()
		}
		resp.Metadata = s.Metadata
	}

	return resp
}

// buildSeasonResponse builds a SeasonResponse from a database Season.
func buildSeasonResponse(s *database.Season) SeasonResponse {
	resp := SeasonResponse{
		IDs:          buildIDs("season", s.ID, nil, nil),
		ShowID:       s.ShowID,
		SeasonNumber: s.SeasonNumber,
		EpisodeCount: s.EpisodeCount,
		Images:       s.Images,
	}
	if s.AiredAt != nil {
		resp.AiredAt = s.AiredAt.String()
	}
	if s.Images == nil || len(s.Images) == 0 {
		resp.Images = make(map[string]any)
	}
	return resp
}

// buildEpisodeResponse builds an EpisodeResponse from a database Episode.
func buildEpisodeResponse(e *database.Episode) EpisodeResponse {
	resp := EpisodeResponse{
		IDs:            buildIDs("episode", e.ID, nil, nil),
		SeasonID:       e.SeasonID,
		ShowID:         e.ShowID,
		EpisodeNumber:  e.EpisodeNumber,
		Title:          e.Title,
		Overview:       e.Overview,
		Runtime:        e.Runtime,
		Images:         e.Images,
		AbsoluteNumber: e.AbsoluteNumber,
	}
	if e.AiredAt != nil {
		resp.AiredAt = e.AiredAt.String()
	}
	if resp.Images == nil {
		resp.Images = make(map[string]any)
	}
	return resp
}

// buildPersonResponse builds a PersonResponse from a database Person.
func buildPersonResponse(p *database.Person) PersonResponse {
	resp := PersonResponse{
		IDs:                buildIDs("person", p.ID, nil, nil),
		Name:               p.Name,
		Biography:          p.Biography,
		Gender:             p.Gender,
		KnownForDepartment: p.KnownForDepartment,
		Images:             p.Images,
	}
	if p.Birthday != nil {
		resp.Birthday = p.Birthday.String()
	}
	if p.Deathday != nil {
		resp.Deathday = p.Deathday.String()
	}
	if resp.Images == nil {
		resp.Images = make(map[string]any)
	}
	return resp
}

// buildRatingSummary builds a RatingSummary from avg and count.
func buildRatingSummary(avg float64, count int64) *RatingSummary {
	return &RatingSummary{
		Rating:       avg,
		Votes:        count,
		Distribution: make(map[string]int64),
	}
}

// MarshalJSON ensures maps are never nil.
func (r MovieResponse) MarshalJSON() ([]byte, error) {
	type Alias MovieResponse
	a := Alias(r)
	if a.Images == nil {
		a.Images = make(map[string]any)
	}
	if a.Genres == nil {
		a.Genres = make([]string, 0)
	}
	return json.Marshal(a)
}

// MarshalJSON ensures maps are never nil.
func (r ShowResponse) MarshalJSON() ([]byte, error) {
	type Alias ShowResponse
	a := Alias(r)
	if a.Images == nil {
		a.Images = make(map[string]any)
	}
	if a.Genres == nil {
		a.Genres = make([]string, 0)
	}
	if a.Seasons == nil {
		a.Seasons = make([]SeasonResponse, 0)
	}
	return json.Marshal(a)
}

// MarshalJSON ensures maps are never nil.
func (r PersonResponse) MarshalJSON() ([]byte, error) {
	type Alias PersonResponse
	a := Alias(r)
	if a.Images == nil {
		a.Images = make(map[string]any)
	}
	return json.Marshal(a)
}

// GetExtended returns the extended level from query params, defaulting to "min".
func GetExtended(c *fiber.Ctx) string {
	ext := c.Query("extended", "min")
	switch ext {
	case "min", "full", "metadata", "images", "all":
		return ext
	default:
		return "min"
	}
}

// getExtended is the internal alias for GetExtended.
func getExtended(c *fiber.Ctx) string { return GetExtended(c) }
