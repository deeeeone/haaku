// Package tvdb is the TheTVDB v4 client. TVDB is haaku's PRIMARY media source:
// identity (the tvdb id), titles, seasons and episodes in TVDB's own default
// (official) order, and artwork all come from here. TMDB is supplementary and
// is merged on top of a TVDB record in internal/metadata — see that package's
// tvdb_ingest.go for the split and docs/media-metadata.md for why.
//
// The client is deliberately self-contained rather than wrapping
// github.com/Pasithea0/go-tmdb-episodes-fix/pkg/tvdb the way theintrodb-api
// does: the library's client owns its token and exposes id-based routes only
// (no name search), and haaku needs name search — the route the search page
// starts from — in the SAME client so a page view costs one login, not two.
package tvdb

import (
	"strconv"
	"strings"
)

// Artwork type ids from GET /v4/artwork/types. Only the ones haaku stores are
// named; the rest of TVDB's ~27 types are ignored on purpose.
const (
	ArtworkSeriesPoster     = 2
	ArtworkSeriesBackground = 3
	ArtworkSeriesIcon       = 5
	ArtworkMoviePoster      = 14
	ArtworkMovieBackground  = 15
)

// ArtworkBaseURL prefixes the relative artwork paths TVDB returns for
// episodes ("/banners/episodes/81189/3859781.jpg"). Series, season and movie
// artwork is already absolute; only episode stills need joining.
const ArtworkBaseURL = "https://artworks.thetvdb.com"

// Links is the pagination block TVDB returns beside list responses.
type Links struct {
	Prev       *string `json:"prev"`
	Self       string  `json:"self"`
	Next       *string `json:"next"`
	TotalItems int     `json:"total_items"`
	PageSize   int     `json:"page_size"`
}

// RemoteID is one entry of a record's remoteIds list — the cross-reference to
// another provider. SourceName is what has to be matched on ("TheMovieDB.com",
// "IMDB"), not the numeric type.
type RemoteID struct {
	ID         string `json:"id"`
	Type       int    `json:"type"`
	SourceName string `json:"sourceName"`
}

// Source names TVDB uses in remoteIds for the two providers haaku links.
const (
	RemoteSourceTMDB = "TheMovieDB.com"
	RemoteSourceIMDB = "IMDB"
)

// --- Search ---

// SearchResponse is GET /v4/search.
type SearchResponse struct {
	Status string         `json:"status"`
	Data   []SearchResult `json:"data"`
	Links  Links          `json:"links"`
}

// SearchResult is one search hit. TVDB returns it in a flatter, string-heavy
// shape than the extended records (year and tvdb_id are strings; status is a
// bare string), which is why this is not a subset of SeriesExtended.
type SearchResult struct {
	ObjectID        string     `json:"objectID"`
	ID              string     `json:"id"`
	TVDBIDString    string     `json:"tvdb_id"`
	Name            string     `json:"name"`
	Type            string     `json:"type"`
	PrimaryType     string     `json:"primary_type"`
	YearString      string     `json:"year"`
	Slug            string     `json:"slug"`
	ImageURL        string     `json:"image_url"`
	Thumbnail       string     `json:"thumbnail"`
	Overview        string     `json:"overview"`
	FirstAirTime    string     `json:"first_air_time"`
	PrimaryLanguage string     `json:"primary_language"`
	Country         string     `json:"country"`
	Network         string     `json:"network"`
	Status          string     `json:"status"`
	Director        string     `json:"director"`
	Genres          []string   `json:"genres"`
	Aliases         []string   `json:"aliases"`
	RemoteIDs       []RemoteID `json:"remote_ids"`
}

// TVDBID returns the numeric TVDB id, from tvdb_id (the field TVDB actually
// populates) or by parsing the "series-81189" / "movie-169" objectID.
func (s SearchResult) TVDBID() int {
	if n, err := strconv.Atoi(strings.TrimSpace(s.TVDBIDString)); err == nil && n > 0 {
		return n
	}
	_, suffix, ok := strings.Cut(s.ObjectID, "-")
	if !ok {
		_, suffix, ok = strings.Cut(s.ID, "-")
	}
	if !ok {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(suffix))
	return n
}

// MediaType maps TVDB's search type onto haaku's Trakt-shaped media type.
// Anything that is not a series or a movie (person, list, ...) returns "".
func (s SearchResult) MediaType() string {
	switch strings.ToLower(s.Type) {
	case "series":
		return "show"
	case "movie":
		return "movie"
	default:
		return ""
	}
}

// Year returns the release year, or 0 when TVDB has none.
func (s SearchResult) Year() int {
	n, _ := strconv.Atoi(strings.TrimSpace(s.YearString))
	return n
}

// RemoteID returns the first remote id from the named source, or "".
func (s SearchResult) RemoteID(source string) string { return remoteID(s.RemoteIDs, source) }

// TMDBID returns the linked TMDB id, or 0.
func (s SearchResult) TMDBID() int { return intOf(s.RemoteID(RemoteSourceTMDB)) }

// IMDBID returns the linked IMDb id, or "".
func (s SearchResult) IMDBID() string { return s.RemoteID(RemoteSourceIMDB) }

// --- Extended records ---

// Status is the record state, shared by series and movies ("Continuing",
// "Ended", "Released", ...).
type Status struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	RecordType string `json:"recordType"`
}

// Company is a network/studio/production company reference.
type Company struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// Genre is a TVDB genre reference.
type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// Artwork is one artwork record. Type is one of the Artwork* constants.
type Artwork struct {
	ID        int    `json:"id"`
	Type      int    `json:"type"`
	Language  string `json:"language"`
	Image     string `json:"image"`
	Thumbnail string `json:"thumbnail"`
	Score     int    `json:"score"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// ContentRating is a country's certification ("TV-MA" in usa, "18" in gbr).
type ContentRating struct {
	Name        string `json:"name"`
	Country     string `json:"country"`
	Description string `json:"description"`
	ContentType string `json:"contentType"`
	FullName    string `json:"fullname"`
}

// Trailer is a promo video. URLs are YouTube watch links.
type Trailer struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Language string `json:"language"`
	Runtime  int    `json:"runtime"`
}

// Alias is a translated/alternative name.
type Alias struct {
	Language string `json:"language"`
	Name     string `json:"name"`
}

// MovieTranslation is GET /v4/movies/{id}/translations/{lang} — where TVDB
// keeps a movie's overview and tagline.
type MovieTranslation struct {
	Name     string `json:"name"`
	Overview string `json:"overview"`
	Tagline  string `json:"tagline"`
	Language string `json:"language"`
}

// Release is a movie's release window; Country "global" is the primary one.
type Release struct {
	Country string `json:"country"`
	Date    string `json:"date"`
	Detail  string `json:"detail"`
}

// SeasonType is one of TVDB's orderings (official / dvd / absolute).
type SeasonType struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	AlternateName string `json:"alternateName"`
}

// SeriesSeason is a season as the extended record lists it, tagged with the
// ordering it belongs to.
type SeriesSeason struct {
	ID          int        `json:"id"`
	SeriesID    int        `json:"seriesId"`
	Type        SeasonType `json:"type"`
	Number      int        `json:"number"`
	Image       string     `json:"image"`
	ImageType   int        `json:"imageType"`
	Name        string     `json:"name"`
	LastUpdated string     `json:"lastUpdated"`
}

// SeriesExtended is GET /v4/series/{id}/extended.
type SeriesExtended struct {
	ID                int             `json:"id"`
	Name              string          `json:"name"`
	Slug              string          `json:"slug"`
	YearString        string          `json:"year"`
	Overview          string          `json:"overview"`
	FirstAired        string          `json:"firstAired"`
	LastAired         string          `json:"lastAired"`
	AverageRuntime    int             `json:"averageRuntime"`
	OriginalLanguage  string          `json:"originalLanguage"`
	OriginalCountry   string          `json:"originalCountry"`
	Score             float64         `json:"score"`
	Image             string          `json:"image"`
	DefaultSeasonType int             `json:"defaultSeasonType"`
	Status            Status          `json:"status"`
	OriginalNetwork   *Company        `json:"originalNetwork"`
	LatestNetwork     *Company        `json:"latestNetwork"`
	Genres            []Genre         `json:"genres"`
	Artworks          []Artwork       `json:"artworks"`
	RemoteIDs         []RemoteID      `json:"remoteIds"`
	ContentRatings    []ContentRating `json:"contentRatings"`
	Seasons           []SeriesSeason  `json:"seasons"`
	SeasonTypes       []SeasonType    `json:"seasonTypes"`
	Trailers          []Trailer       `json:"trailers"`
	Aliases           []Alias         `json:"aliases"`
}

// Year returns the show's first year, or 0.
func (s SeriesExtended) Year() int {
	if n, err := strconv.Atoi(strings.TrimSpace(s.YearString)); err == nil {
		return n
	}
	return yearOf(s.FirstAired)
}

// Network returns the show's network name, preferring the network it is on now.
func (s SeriesExtended) Network() string {
	if s.LatestNetwork != nil && s.LatestNetwork.Name != "" {
		return s.LatestNetwork.Name
	}
	if s.OriginalNetwork != nil {
		return s.OriginalNetwork.Name
	}
	return ""
}

// TMDBID returns the linked TMDB id, or 0.
func (s SeriesExtended) TMDBID() int { return intOf(remoteID(s.RemoteIDs, RemoteSourceTMDB)) }

// IMDBID returns the linked IMDb id, or "".
func (s SeriesExtended) IMDBID() string { return remoteID(s.RemoteIDs, RemoteSourceIMDB) }

// GenreNames returns the genre names in TVDB's order.
func (s SeriesExtended) GenreNames() []string { return genreNames(s.Genres) }

// Poster returns the best series poster URL (English first, then top scored).
func (s SeriesExtended) Poster() string {
	if u := pickArtwork(s.Artworks, ArtworkSeriesPoster); u != "" {
		return u
	}
	return s.Image
}

// Backdrop returns the best series background URL, or "".
func (s SeriesExtended) Backdrop() string {
	return pickArtwork(s.Artworks, ArtworkSeriesBackground)
}

// Logo returns the best clear-logo URL, or "".
func (s SeriesExtended) Logo() string { return pickArtwork(s.Artworks, ArtworkSeriesIcon) }

// Certification returns the US content rating, or the first one TVDB has.
func (s SeriesExtended) Certification() string { return certification(s.ContentRatings) }

// TrailerURL returns an English trailer, or the first one, or "".
func (s SeriesExtended) TrailerURL() string { return trailerURL(s.Trailers) }

// MovieExtended is GET /v4/movies/{id}/extended.
//
// TVDB does not put a movie's overview or tagline in the extended record: it
// lists the languages it HAS them in (overviewTranslations) and serves the text
// from /movies/{id}/translations/{lang}. GetMovieExtended fills OverviewText
// and TaglineText from the English translation so a caller gets a complete
// record from one call, and those two fields are deliberately `json:"-"`: the
// record is what the API returned, the text is the merged view.
type MovieExtended struct {
	ID               int             `json:"id"`
	Name             string          `json:"name"`
	Slug             string          `json:"slug"`
	YearString       string          `json:"year"`
	Runtime          int             `json:"runtime"`
	Score            float64         `json:"score"`
	Image            string          `json:"image"`
	OriginalLanguage string          `json:"originalLanguage"`
	OriginalCountry  string          `json:"originalCountry"`
	FirstRelease     *Release        `json:"first_release"`
	Status           Status          `json:"status"`
	Genres           []Genre         `json:"genres"`
	Artworks         []Artwork       `json:"artworks"`
	RemoteIDs        []RemoteID      `json:"remoteIds"`
	ContentRatings   []ContentRating `json:"contentRatings"`
	Trailers         []Trailer       `json:"trailers"`
	Aliases          []Alias         `json:"aliases"`

	// OverviewText / TaglineText are merged in by GetMovieExtended from the
	// English translation; they are not fields of the extended response.
	OverviewText string `json:"-"`
	TaglineText  string `json:"-"`
}

// Overview returns the movie's English overview, or "".
func (m MovieExtended) Overview() string { return m.OverviewText }

// Tagline returns the movie's English tagline, or "".
func (m MovieExtended) Tagline() string { return m.TaglineText }

// Year returns the movie's release year, or 0.
func (m MovieExtended) Year() int {
	if n, err := strconv.Atoi(strings.TrimSpace(m.YearString)); err == nil && n > 0 {
		return n
	}
	if m.FirstRelease != nil {
		return yearOf(m.FirstRelease.Date)
	}
	return 0
}

// ReleasedDate returns the primary release date ("YYYY-MM-DD"), or "".
func (m MovieExtended) ReleasedDate() string {
	if m.FirstRelease == nil {
		return ""
	}
	return m.FirstRelease.Date
}

// TMDBID returns the linked TMDB id, or 0.
func (m MovieExtended) TMDBID() int { return intOf(remoteID(m.RemoteIDs, RemoteSourceTMDB)) }

// IMDBID returns the linked IMDb id, or "".
func (m MovieExtended) IMDBID() string { return remoteID(m.RemoteIDs, RemoteSourceIMDB) }

// GenreNames returns the genre names in TVDB's order.
func (m MovieExtended) GenreNames() []string { return genreNames(m.Genres) }

// Poster returns the best movie poster URL (English first, then top scored).
func (m MovieExtended) Poster() string {
	if u := pickArtwork(m.Artworks, ArtworkMoviePoster); u != "" {
		return u
	}
	return m.Image
}

// Backdrop returns the best movie background URL, or "".
func (m MovieExtended) Backdrop() string { return pickArtwork(m.Artworks, ArtworkMovieBackground) }

// Certification returns the US content rating, or the first one TVDB has.
func (m MovieExtended) Certification() string { return certification(m.ContentRatings) }

// TrailerURL returns an English trailer, or the first one, or "".
func (m MovieExtended) TrailerURL() string { return trailerURL(m.Trailers) }

// --- Episodes ---

// Episode is one episode record from /v4/series/{id}/episodes/{order}/{lang}.
// It is the record haaku stores: season/episode numbering as TVDB's default
// (official) order has it, which is TVDB's own recommendation and what media
// servers configured against TVDB use.
type Episode struct {
	ID             int64  `json:"id"`
	SeriesID       int    `json:"seriesId"`
	Name           string `json:"name"`
	Aired          string `json:"aired"`
	Runtime        int    `json:"runtime"`
	Overview       string `json:"overview"`
	Image          string `json:"image"`
	Number         int    `json:"number"`
	AbsoluteNumber int    `json:"absoluteNumber"`
	SeasonNumber   int    `json:"seasonNumber"`
	YearString     string `json:"year"`
}

// ImageURL returns the episode still as an absolute URL, or "".
func (e Episode) ImageURL() string {
	if e.Image == "" {
		return ""
	}
	if strings.HasPrefix(e.Image, "http") {
		return e.Image
	}
	return ArtworkBaseURL + e.Image
}

// episodeListResponse is the envelope of an episode-list page.
type episodeListResponse struct {
	Status string `json:"status"`
	Data   struct {
		Episodes []Episode `json:"episodes"`
	} `json:"data"`
	Links Links `json:"links"`
}

// --- helpers ---

func remoteID(ids []RemoteID, source string) string {
	for _, id := range ids {
		if strings.EqualFold(id.SourceName, source) {
			return strings.TrimSpace(id.ID)
		}
	}
	return ""
}

// intOf parses a decimal id, returning 0 for anything unparseable (TVDB uses
// "0", "" and absent interchangeably for "no such remote id").
func intOf(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func genreNames(genres []Genre) []string {
	names := make([]string, 0, len(genres))
	for _, g := range genres {
		if g.Name != "" {
			names = append(names, g.Name)
		}
	}
	return names
}

// pickArtwork chooses one artwork URL of a type: English first, then the
// highest TVDB score, then anything. TVDB scores every artwork, so "highest
// score in the requested language" is the same rule the TVDB website uses.
func pickArtwork(artworks []Artwork, artworkType int) string {
	var best string
	bestScore := -1
	var fallback string
	fallbackScore := -1
	for _, a := range artworks {
		if a.Type != artworkType || a.Image == "" {
			continue
		}
		if strings.EqualFold(a.Language, "eng") || a.Language == "" {
			if a.Score > bestScore {
				best, bestScore = a.Image, a.Score
			}
			continue
		}
		if a.Score > fallbackScore {
			fallback, fallbackScore = a.Image, a.Score
		}
	}
	if best != "" {
		return best
	}
	return fallback
}

// certification prefers the US rating (haaku is US-first today) and otherwise
// takes the first rating TVDB carries, so a record is never left uncertified
// just because TVDB has no US entry.
func certification(ratings []ContentRating) string {
	for _, r := range ratings {
		if strings.EqualFold(r.Country, "usa") && r.Name != "" {
			return r.Name
		}
	}
	for _, r := range ratings {
		if r.Name != "" {
			return r.Name
		}
	}
	return ""
}

func trailerURL(trailers []Trailer) string {
	for _, t := range trailers {
		if strings.EqualFold(t.Language, "eng") && t.URL != "" {
			return t.URL
		}
	}
	for _, t := range trailers {
		if t.URL != "" {
			return t.URL
		}
	}
	return ""
}

// yearOf extracts the year from a "YYYY-MM-DD" date, or 0.
func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	n, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return n
}
