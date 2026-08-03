package tmdb

// --- Shared response shapes (partial — only what we need) ---

type IDSet struct {
	TMDB    int    `json:"id"`
	IMDB    string `json:"imdb_id"`
	TVDB    int    `json:"tvdb_id"`
	Slug    string // our computed slug
}

// Movie represents a TMDB movie detail response.
type Movie struct {
	ID            int      `json:"id"`
	IMDBID        string   `json:"imdb_id"`
	Title         string   `json:"title"`
	OriginalTitle string   `json:"original_title"`
	Tagline       string   `json:"tagline"`
	Overview      string   `json:"overview"`
	ReleaseDate   string   `json:"release_date"`
	Runtime       int      `json:"runtime"`
	Status        string   `json:"status"`
	Popularity    float64  `json:"popularity"`
	VoteAverage   float64  `json:"vote_average"`
	VoteCount     int64    `json:"vote_count"`
	Genres        []Genre  `json:"genres"`
	Language      string   `json:"original_language"`
	PosterPath    string   `json:"poster_path"`
	BackdropPath  string   `json:"backdrop_path"`
	Adult         bool     `json:"adult"`
	Budget        int64    `json:"budget"`    // for metadata passthrough
	Revenue       int64    `json:"revenue"`   // for metadata passthrough
	ProductionCountries []Country `json:"production_countries"`
}

// Show represents a TMDB TV show detail response.
type Show struct {
	ID            int      `json:"id"`
	IMDBID        string   `json:"imdb_id"`
	Name          string   `json:"name"`
	OriginalName  string   `json:"original_name"`
	Tagline       string   `json:"tagline"`
	Overview      string   `json:"overview"`
	FirstAirDate  string   `json:"first_air_date"`
	LastAirDate   string   `json:"last_air_date"`
	Status        string   `json:"status"`
	Popularity    float64  `json:"popularity"`
	VoteAverage   float64  `json:"vote_average"`
	VoteCount     int64    `json:"vote_count"`
	Genres        []Genre  `json:"genres"`
	Language      string   `json:"original_language"`
	PosterPath    string   `json:"poster_path"`
	BackdropPath  string   `json:"backdrop_path"`
	Networks      []Network `json:"networks"`
	SeasonsCount  int       `json:"number_of_seasons"`
	EpisodesCount int       `json:"number_of_episodes"`
	Seasons       []SeasonSummary `json:"seasons"`
	Adult         bool     `json:"adult"`
	ProductionCountries []Country `json:"production_countries"`
}

// SeasonSummary is the lightweight season info from the show detail.
type SeasonSummary struct {
	ID           int    `json:"id"`
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
	EpisodeCount int    `json:"episode_count"`
	AirDate      string `json:"air_date"`
	PosterPath   string `json:"poster_path"`
}

// Season represents a full TMDB season detail response.
type Season struct {
	ID           int             `json:"id"`
	SeasonNumber int             `json:"season_number"`
	Name         string          `json:"name"`
	Overview     string          `json:"overview"`
	AirDate      string          `json:"air_date"`
	PosterPath   string          `json:"poster_path"`
	Episodes     []EpisodeDetail `json:"episodes"`
}

// EpisodeDetail is a single episode from the season detail response.
type EpisodeDetail struct {
	ID            int     `json:"id"`
	EpisodeNumber int     `json:"episode_number"`
	Name          string  `json:"name"`
	Overview      string  `json:"overview"`
	AirDate       string  `json:"air_date"`
	Runtime       int     `json:"runtime"`
	StillPath     string  `json:"still_path"`
	VoteAverage   float64 `json:"vote_average"`
	VoteCount     int64   `json:"vote_count"`
	SeasonNumber  int     `json:"season_number"`
	ShowID        int     // injected from context
}

// Person represents a TMDB person detail response.
type Person struct {
	ID                 int    `json:"id"`
	IMDBID             string `json:"imdb_id"`
	Name               string `json:"name"`
	Biography          string `json:"biography"`
	Birthday           string `json:"birthday"`
	Deathday           string `json:"deathday"`
	Gender             int    `json:"gender"`
	KnownForDepartment string `json:"known_for_department"`
	ProfilePath        string `json:"profile_path"`
	Adult              bool   `json:"adult"`
}

// Title is a search-hit summary.
type Title struct {
	ID           int     `json:"id"`
	MediaType    string  `json:"media_type"` // movie or tv
	Title        string  `json:"title,omitempty"`
	Name         string  `json:"name,omitempty"`
	Overview     string  `json:"overview"`
	ReleaseDate  string  `json:"release_date,omitempty"`
	FirstAirDate string  `json:"first_air_date,omitempty"`
	Popularity   float64 `json:"popularity"`
	PosterPath   string  `json:"poster_path,omitempty"`
	BackdropPath string  `json:"backdrop_path,omitempty"`
}

// Genre, Network, Country, Language are small supporting types.

type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Network struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Country struct {
	ISO   string `json:"iso_3166_1"`
	Name  string `json:"name"`
}

// Paginated wraps a TMDB paged response.
type Paginated[T any] struct {
	Page         int `json:"page"`
	TotalPages   int `json:"total_pages"`
	TotalResults int `json:"total_results"`
	Results      []T `json:"results"`
}

// ImageConfig holds the TMDB image CDN configuration.
type ImageConfig struct {
	BaseURL       string   `json:"base_url"`
	SecureBaseURL string   `json:"secure_base_url"`
	BackdropSizes []string `json:"backdrop_sizes"`
	PosterSizes   []string `json:"poster_sizes"`
	ProfileSizes  []string `json:"profile_sizes"`
	StillSizes    []string `json:"still_sizes"`
}

// SearchResult wraps the multi-search response.
type SearchResult struct {
	Page         int     `json:"page"`
	TotalPages   int     `json:"total_pages"`
	TotalResults int     `json:"total_results"`
	Results      []Title `json:"results"`
}

// ChangeFeed is a TMDB change-feed entry.
type ChangeFeed[T any] struct {
	Page         int `json:"page"`
	TotalPages   int `json:"total_pages"`
	TotalResults int `json:"total_results"`
	Results      []T `json:"results"`
}

type ChangeEntry struct {
	ID   int    `json:"id"`
	Date string `json:"date"` // "2026-08-01"
}

// SeasonDetails resolves a full season from the nested episodes.
func (s *Season) SeasonDetails() []SeasonEpisode {
	out := make([]SeasonEpisode, len(s.Episodes))
	for i, ep := range s.Episodes {
		out[i] = SeasonEpisode{
			EpisodeNumber: ep.EpisodeNumber,
			Name:          ep.Name,
			Overview:      ep.Overview,
			AirDate:       ep.AirDate,
			Runtime:       ep.Runtime,
			StillPath:     ep.StillPath,
			VoteAverage:   ep.VoteAverage,
			VoteCount:     ep.VoteCount,
		}
	}
	return out
}

// SeasonEpisode is the flat episode shape we store.
type SeasonEpisode struct {
	EpisodeNumber int     `json:"episode_number"`
	Name          string  `json:"name"`
	Overview      string  `json:"overview"`
	AirDate       string  `json:"air_date"`
	Runtime       int     `json:"runtime"`
	StillPath     string  `json:"still_path"`
	VoteAverage   float64 `json:"vote_average"`
	VoteCount     int64   `json:"vote_count"`
}

// DisplayName returns the best available title for a search hit.
func (t *Title) DisplayName() string {
	if t.Title != "" {
		return t.Title
	}
	return t.Name
}

// ReleaseYear returns the release/first-air year or empty.
func (t *Title) ReleaseYear() string {
	d := t.ReleaseDate
	if d == "" {
		d = t.FirstAirDate
	}
	if len(d) >= 4 {
		return d[:4]
	}
	return ""
}

// --- Lightweight list types (v3 popular/discover/trending responses) ---

// MovieListItem is a lightweight movie from list/discover responses.
type MovieListItem struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	Overview      string  `json:"overview"`
	ReleaseDate   string  `json:"release_date"`
	Popularity    float64 `json:"popularity"`
	VoteAverage   float64 `json:"vote_average"`
	VoteCount     int64   `json:"vote_count"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	Language      string  `json:"original_language"`
	GenreIDs      []int   `json:"genre_ids"`
	Adult         bool    `json:"adult"`
}

// TVListItem is a lightweight TV show from list/discover responses.
type TVListItem struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	OriginalName  string  `json:"original_name"`
	Overview      string  `json:"overview"`
	FirstAirDate  string  `json:"first_air_date"`
	Popularity    float64 `json:"popularity"`
	VoteAverage   float64 `json:"vote_average"`
	VoteCount     int64   `json:"vote_count"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	Language      string  `json:"original_language"`
	GenreIDs      []int   `json:"genre_ids"`
	Adult         bool    `json:"adult"`
}

// TrendingItem is a single item from the trending endpoint.
type TrendingItem struct {
	ID            int     `json:"id"`
	MediaType     string  `json:"media_type"` // "movie" or "tv" or "person"
	Title         string  `json:"title,omitempty"`
	Name          string  `json:"name,omitempty"`
	OriginalTitle string  `json:"original_title,omitempty"`
	OriginalName  string  `json:"original_name,omitempty"`
	Overview      string  `json:"overview"`
	ReleaseDate   string  `json:"release_date,omitempty"`
	FirstAirDate  string  `json:"first_air_date,omitempty"`
	Popularity    float64 `json:"popularity"`
	VoteAverage   float64 `json:"vote_average"`
	VoteCount     int64   `json:"vote_count"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	Language      string  `json:"original_language"`
	GenreIDs      []int   `json:"genre_ids"`
	Adult         bool    `json:"adult"`
}

// --- Credits ---

// CreditsResponse holds cast and crew for a movie or TV show.
type CreditsResponse struct {
	ID   int           `json:"id"`
	Cast []CastMember  `json:"cast"`
	Crew []CrewMember  `json:"crew"`
}

// CastMember represents an actor/actress credit.
type CastMember struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	Character          string `json:"character"`
	Order              int    `json:"order"`
	ProfilePath        string `json:"profile_path"`
	Gender             int    `json:"gender"`
	KnownForDepartment string `json:"known_for_department"`
	Adult              bool   `json:"adult"`
}

// CrewMember represents a crew credit (director, writer, etc.).
type CrewMember struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	Job                string `json:"job"`
	Department         string `json:"department"`
	ProfilePath        string `json:"profile_path"`
	Gender             int    `json:"gender"`
	KnownForDepartment string `json:"known_for_department"`
	Adult              bool   `json:"adult"`
}

// --- Watch providers (v3) ---

// WatchProvidersResponse is the TMDB v3 watch/providers response.
type WatchProvidersResponse struct {
	ID      int                         `json:"id"`
	Results map[string]CountryProviders `json:"results"`
}

// CountryProviders holds streaming/rent/buy availability for one country.
type CountryProviders struct {
	Link     string     `json:"link"`
	Flatrate []Provider `json:"flatrate"`
	Watch    []Provider `json:"watch"`
	Rent     []Provider `json:"rent"`
	Buy      []Provider `json:"buy"`
}

// Provider is a streaming service entry.
type Provider struct {
	ProviderID   int    `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	LogoPath     string `json:"logo_path"`
}
