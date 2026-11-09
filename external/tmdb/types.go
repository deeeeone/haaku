package tmdb

// --- Shared response shapes (partial — only what we need) ---

type IDSet struct {
	TMDB int    `json:"id"`
	IMDB string `json:"imdb_id"`
	TVDB int    `json:"tvdb_id"`
	Slug string // our computed slug
}

// Movie represents a TMDB movie detail response.
//
// GetMovie asks for external_ids, release_dates and videos alongside the base
// record (append_to_response), so the ingest gets the IMDb/TVDB links, the
// country certifications and the trailer in the SAME call as the title — the
// three things a Trakt-shaped client expects per title. The appended objects
// are pointers because a client that does not append (the change-feed sync)
// still decodes into this type.
type Movie struct {
	ID                  int                 `json:"id"`
	IMDBID              string              `json:"imdb_id"`
	Title               string              `json:"title"`
	OriginalTitle       string              `json:"original_title"`
	Tagline             string              `json:"tagline"`
	Overview            string              `json:"overview"`
	ReleaseDate         string              `json:"release_date"`
	Runtime             int                 `json:"runtime"`
	Status              string              `json:"status"`
	Popularity          float64             `json:"popularity"`
	VoteAverage         float64             `json:"vote_average"`
	VoteCount           int64               `json:"vote_count"`
	Genres              []Genre             `json:"genres"`
	Language            string              `json:"original_language"`
	PosterPath          string              `json:"poster_path"`
	BackdropPath        string              `json:"backdrop_path"`
	Adult               bool                `json:"adult"`
	Budget              int64               `json:"budget"`  // for metadata passthrough
	Revenue             int64               `json:"revenue"` // for metadata passthrough
	ProductionCountries []Country           `json:"production_countries"`
	ProductionCompanies []ProductionCompany `json:"production_companies"`
	ExternalIDs         *ExternalIDs        `json:"external_ids,omitempty"`
	ReleaseDates        *ReleaseDates       `json:"release_dates,omitempty"`
	Videos              *Videos             `json:"videos,omitempty"`
}

// Show represents a TMDB TV show detail response.
//
// As with Movie, GetShow appends external_ids, content_ratings and videos: the
// TV SHOW external-ids payload is the only place TMDB names a show's TVDB id
// (the movie one does not carry it — measured against the live API), which is
// what makes the TMDB→TVDB supplementary link one call instead of a search.
type Show struct {
	ID                  int                 `json:"id"`
	IMDBID              string              `json:"imdb_id"`
	Name                string              `json:"name"`
	OriginalName        string              `json:"original_name"`
	Tagline             string              `json:"tagline"`
	Overview            string              `json:"overview"`
	FirstAirDate        string              `json:"first_air_date"`
	LastAirDate         string              `json:"last_air_date"`
	Status              string              `json:"status"`
	Popularity          float64             `json:"popularity"`
	VoteAverage         float64             `json:"vote_average"`
	VoteCount           int64               `json:"vote_count"`
	Genres              []Genre             `json:"genres"`
	Language            string              `json:"original_language"`
	PosterPath          string              `json:"poster_path"`
	BackdropPath        string              `json:"backdrop_path"`
	Networks            []Network           `json:"networks"`
	SeasonsCount        int                 `json:"number_of_seasons"`
	EpisodesCount       int                 `json:"number_of_episodes"`
	Seasons             []SeasonSummary     `json:"seasons"`
	Adult               bool                `json:"adult"`
	ProductionCountries []Country           `json:"production_countries"`
	ProductionCompanies []ProductionCompany `json:"production_companies"`
	ExternalIDs         *ExternalIDs        `json:"external_ids,omitempty"`
	ContentRatings      *ContentRatings     `json:"content_ratings,omitempty"`
	Videos              *Videos             `json:"videos,omitempty"`
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
//
// GetSeasonExtras appends videos and credits, so a season read can store one
// season's videos and its credit set in a single request.
type Season struct {
	ID           int              `json:"id"`
	SeasonNumber int              `json:"season_number"`
	Name         string           `json:"name"`
	Overview     string           `json:"overview"`
	AirDate      string           `json:"air_date"`
	PosterPath   string           `json:"poster_path"`
	Episodes     []EpisodeDetail  `json:"episodes"`
	Videos       *Videos          `json:"videos,omitempty"`
	Credits      *CreditsResponse `json:"credits,omitempty"`
}

// EpisodeDetail is a single episode from the season detail response.
type EpisodeDetail struct {
	ID            int              `json:"id"`
	EpisodeNumber int              `json:"episode_number"`
	Name          string           `json:"name"`
	Overview      string           `json:"overview"`
	AirDate       string           `json:"air_date"`
	Runtime       int              `json:"runtime"`
	StillPath     string           `json:"still_path"`
	VoteAverage   float64          `json:"vote_average"`
	VoteCount     int64            `json:"vote_count"`
	SeasonNumber  int              `json:"season_number"`
	ShowID        int              // injected from context
	GuestStars    []CastMember     `json:"guest_stars,omitempty"`
	Crew          []CrewMember     `json:"crew,omitempty"`
	Videos        *Videos          `json:"videos,omitempty"`
	Credits       *CreditsResponse `json:"credits,omitempty"`
}

// ProductionCompany is one studio from TMDB's production_companies list. The
// origin country is TMDB's own, and it is what a Trakt-shaped studio object
// carries alongside the name and ids.
type ProductionCompany struct {
	ID            int    `json:"id"`
	LogoPath      string `json:"logo_path"`
	Name          string `json:"name"`
	OriginCountry string `json:"origin_country"`
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
	ISO  string `json:"iso_3166_1"`
	Name string `json:"name"`
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
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	OriginalName string  `json:"original_name"`
	Overview     string  `json:"overview"`
	FirstAirDate string  `json:"first_air_date"`
	Popularity   float64 `json:"popularity"`
	VoteAverage  float64 `json:"vote_average"`
	VoteCount    int64   `json:"vote_count"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	Language     string  `json:"original_language"`
	GenreIDs     []int   `json:"genre_ids"`
	Adult        bool    `json:"adult"`
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

// CreditsResponse holds cast and crew for a movie or TV show. An EPISODE's
// credits carry guest_stars instead of a full cast; the field is kept so one
// type covers both payloads.
type CreditsResponse struct {
	ID         int          `json:"id"`
	Cast       []CastMember `json:"cast"`
	GuestStars []CastMember `json:"guest_stars"`
	Crew       []CrewMember `json:"crew"`
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

// --- Reference / configuration types (v3) ---

// GenreList is the wrapper for TMDB genre list responses.
// GET /genre/movie/list and /genre/tv/list both return this shape.
type GenreList struct {
	Genres []Genre `json:"genres"`
}

// ConfigLanguage is a single entry from GET /configuration/languages.
type ConfigLanguage struct {
	ISOCode     string `json:"iso_639_1"`
	EnglishName string `json:"english_name"`
	Name        string `json:"name"`
}

// ConfigCountry is a single entry from GET /configuration/countries.
type ConfigCountry struct {
	ISOCode     string `json:"iso_3166_1"`
	EnglishName string `json:"english_name"`
}

// CertificationsResponse is the wrapper for GET /certification/{movie,tv}/list.
type CertificationsResponse struct {
	Certifications map[string][]Certification `json:"certifications"`
}

// Certification is a single rating entry within a country's list.
type Certification struct {
	Certification string `json:"certification"`
	Meaning       string `json:"meaning"`
	Order         int    `json:"order"`
}

// --- Appended sub-resources (append_to_response) ---

// appendParams is the append_to_response list GetMovie/GetShow ask for. One
// detail call then carries everything a Trakt-shaped card and its detail page
// need; without it a title costs three extra requests (external ids,
// certifications, videos) and a TMDB rate budget.
const (
	appendMovie = "external_ids,release_dates,videos"
	appendShow  = "external_ids,content_ratings,videos"
	// Season and episode reads append their own videos and credits (issue #68).
	appendSeason  = "videos,credits"
	appendEpisode = "videos,credits"
)

// ExternalIDs is TMDB's external_ids payload.
//
// The MOVIE payload carries no tvdb_id — measured against the live API: GET
// /movie/603/external_ids returns imdb_id and the social handles only, while
// GET /tv/1396/external_ids returns tvdb_id 81189. So a movie's TMDB→TVDB link
// is recovered through TVDB's own remote-id search instead.
type ExternalIDs struct {
	IMDBID      string `json:"imdb_id"`
	TVDBID      int    `json:"tvdb_id"`
	TVRageID    int    `json:"tvrage_id"`
	WikidataID  string `json:"wikidata_id"`
	FacebookID  string `json:"facebook_id"`
	InstagramID string `json:"instagram_id"`
	TwitterID   string `json:"twitter_id"`
}

// Videos is TMDB's videos payload.
type Videos struct {
	Results []Video `json:"results"`
}

// Video is one trailer/teaser/clip.
//
// The extra fields are what the Trakt contract's video object requires: a size
// (360/480/720/1080), a publish timestamp, and the language/country the video
// was published for. They are part of the appended videos payload.
type Video struct {
	Key         string `json:"key"`
	Site        string `json:"site"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Official    bool   `json:"official"`
	Size        int    `json:"size"`
	PublishedAt string `json:"published_at"`
	ISO639      string `json:"iso_639_1"`
	ISO3166     string `json:"iso_3166_1"`
}

// TrailerURL returns a YouTube URL for the best trailer TMDB has, or "".
//
// Preference order: an official "Trailer" beats an unofficial one, which beats
// a "Teaser". Non-YouTube entries are skipped — the field is a URL a client
// embeds, and every other site TMDB lists would need a different embed.
func (v *Videos) TrailerURL() string {
	if v == nil {
		return ""
	}
	pick := ""
	pickRank := 0
	for _, video := range v.Results {
		if video.Site != "YouTube" || video.Key == "" {
			continue
		}
		rank := 0
		switch video.Type {
		case "Trailer":
			rank = 2
		case "Teaser":
			rank = 1
		default:
			continue
		}
		if video.Official {
			rank++
		}
		if rank > pickRank {
			pick, pickRank = video.Key, rank
		}
	}
	if pick == "" {
		return ""
	}
	return "https://www.youtube.com/watch?v=" + pick
}

// ReleaseDates is TMDB's release_dates payload for a movie.
type ReleaseDates struct {
	Results []CountryReleaseDates `json:"results"`
}

// CountryReleaseDates holds one country's dated releases.
type CountryReleaseDates struct {
	ISO3166      string        `json:"iso_3166_1"`
	ReleaseDates []ReleaseDate `json:"release_dates"`
}

// ReleaseDate is a single dated release with its certification.
type ReleaseDate struct {
	Certification string `json:"certification"`
	Type          int    `json:"type"`
	ReleaseDate   string `json:"release_date"`
}

// Certification returns a movie certification, preferring the US rating (the
// one a Trakt-shaped client shows as the title's rating) and falling back to
// the first country that has one, so a non-US title is not left blank.
func (r *ReleaseDates) Certification() string {
	if r == nil {
		return ""
	}
	fallback := ""
	for _, country := range r.Results {
		for _, release := range country.ReleaseDates {
			if release.Certification == "" {
				continue
			}
			if country.ISO3166 == "US" {
				return release.Certification
			}
			if fallback == "" {
				fallback = release.Certification
			}
		}
	}
	return fallback
}

// ContentRatings is TMDB's content_ratings payload for a TV show.
type ContentRatings struct {
	Results []ContentRating `json:"results"`
}

// ContentRating is one country's TV rating.
type ContentRating struct {
	ISO3166 string `json:"iso_3166_1"`
	Rating  string `json:"rating"`
}

// Certification returns a show's content rating, preferring the US one.
func (c *ContentRatings) Certification() string {
	if c == nil {
		return ""
	}
	fallback := ""
	for _, rating := range c.Results {
		if rating.Rating == "" {
			continue
		}
		if rating.ISO3166 == "US" {
			return rating.Rating
		}
		if fallback == "" {
			fallback = rating.Rating
		}
	}
	return fallback
}

// TVDBID returns the TVDB id TMDB names for this movie, or 0. TMDB's movie
// external-ids payload does not carry one, so this is almost always 0 — the
// link is recovered through TVDB's remote-id search instead.
func (m *Movie) TVDBID() int {
	if m == nil || m.ExternalIDs == nil {
		return 0
	}
	return m.ExternalIDs.TVDBID
}

// TVDBID returns the TVDB id TMDB names for this show, or 0. The TV external-ids
// payload does carry one, which is the cheap half of the supplementary link.
func (s *Show) TVDBID() int {
	if s == nil || s.ExternalIDs == nil {
		return 0
	}
	return s.ExternalIDs.TVDBID
}

// Certification returns the movie's country certification, or "".
func (m *Movie) Certification() string {
	if m == nil {
		return ""
	}
	return m.ReleaseDates.Certification()
}

// Certification is Certification for a show's content ratings.
func (s *Show) Certification() string {
	if s == nil {
		return ""
	}
	return s.ContentRatings.Certification()
}

// TrailerURL returns the movie's best YouTube trailer URL, or "".
func (m *Movie) TrailerURL() string {
	if m == nil {
		return ""
	}
	return m.Videos.TrailerURL()
}

// TrailerURL is TrailerURL for a show.
func (s *Show) TrailerURL() string {
	if s == nil {
		return ""
	}
	return s.Videos.TrailerURL()
}
