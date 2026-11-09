package tmdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ErrNotFound is what a 404 from TMDB wraps. TMDB answers 404 both for a record
// that does not exist and for a route the tree does not carry, so callers that
// need to tell "no such title" (a 404 envelope) apart from "could not be asked"
// (a 502) test this rather than reading the message.
var ErrNotFound = errors.New("tmdb: not found")

// IsNotFound reports whether err is TMDB's own "no such record".
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// v3BaseURL is the TMDB API root this client addresses. Everything in this
// package — details, search, changes, popular/trending, credits, configuration
// — lives on v3; the v4 tree is for lists/auth/account.
const v3BaseURL = "https://api.themoviedb.org/3"

// Client talks to the TMDB v3 API (api.themoviedb.org/3) using a v4
// read-access token. Rate-limit aware; no caching (the metadata worker
// handles that).
//
// WHY v3 AND NOT /4: the client used to address /4, and every detail route
// there answers 404 for this project's token — GET /4/movie/603,
// /4/tv/1396, /4/search/multi and /4/configuration all return
// {"status_code":34,"status_message":"The resource you requested could not be
// found."} while the same calls on /3 return 200. The v4 tree is for lists,
// auth and account routes; the detail/search surface this package needs is v3.
// The token is still the v4 bearer token — it authenticates BOTH trees, which
// is why the failure was a 404 (route not found) rather than a 401.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

// New creates a TMDB client. Reads TMDB_API_KEY from env.
func New() (*Client, error) {
	token := strings.TrimSpace(os.Getenv("TMDB_API_KEY"))
	if token == "" {
		return nil, fmt.Errorf("TMDB_API_KEY environment variable is required")
	}
	return &Client{
		baseURL: v3BaseURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		token: token,
	}, nil
}

// SetBaseURL points the client at another host (tests, a proxy in front of
// TMDB). It does not change which API version is addressed: callers pass the
// root, e.g. an httptest server.
func (c *Client) SetBaseURL(base string) { c.baseURL = strings.TrimRight(base, "/") }

// SetHTTPClient replaces the underlying HTTP client.
func (c *Client) SetHTTPClient(h *http.Client) {
	if h != nil {
		c.httpClient = h
	}
}

// authHeader builds the Authorization header value.
func (c *Client) authHeader() string { return "Bearer " + c.token }

// do performs a TMDB request and returns the raw response.
func (c *Client) do(method, path string, params url.Values) (*http.Response, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if params != nil {
		u.RawQuery = params.Encode()
	}

	req, err := http.NewRequest(method, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tmdb request failed: %w", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("tmdb rate limited (429) on %s", path)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("tmdb error %d on %s: %s", resp.StatusCode, path, string(body))
	}
	return resp, nil
}

func decode[T any](resp *http.Response) (*T, error) {
	var v T
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, fmt.Errorf("failed to decode tmdb response: %w", err)
	}
	return &v, nil
}

// --- API methods ---

func (c *Client) GetMovie(id int) (*Movie, error) {
	// The appended sub-resources are what make TMDB a complete PRIMARY record
	// rather than a name-and-poster lookup: external ids (the IMDb and, for
	// shows, TVDB links), release dates (the certification) and videos (the
	// trailer). One call, not four.
	resp, err := c.do("GET", fmt.Sprintf("/movie/%d", id), url.Values{"append_to_response": {appendMovie}})
	if err != nil {
		return nil, err
	}
	return decode[Movie](resp)
}

func (c *Client) GetShow(id int) (*Show, error) {
	resp, err := c.do("GET", fmt.Sprintf("/tv/%d", id), url.Values{"append_to_response": {appendShow}})
	if err != nil {
		return nil, err
	}
	return decode[Show](resp)
}

func (c *Client) GetSeason(showID, seasonNum int) (*Season, error) {
	resp, err := c.do("GET", fmt.Sprintf("/tv/%d/season/%d", showID, seasonNum), nil)
	if err != nil {
		return nil, err
	}
	s, err := decode[Season](resp)
	if err != nil {
		return nil, err
	}
	// Inject show ID into each episode for context
	for i := range s.Episodes {
		s.Episodes[i].ShowID = showID
	}
	return s, nil
}

func (c *Client) GetPerson(id int) (*Person, error) {
	resp, err := c.do("GET", fmt.Sprintf("/person/%d", id), nil)
	if err != nil {
		return nil, err
	}
	return decode[Person](resp)
}

func (c *Client) SearchMulti(query string, page int) (*SearchResult, error) {
	p := url.Values{}
	p.Set("query", query)
	p.Set("page", fmt.Sprintf("%d", page))
	resp, err := c.do("GET", "/search/multi", p)
	if err != nil {
		return nil, err
	}
	return decode[SearchResult](resp)
}

// SearchMovie and SearchTV are the type-scoped halves of SearchMulti. They are
// what a `type=movie` (or `type=show`) search uses: /search/multi takes no type
// filter, so asking it for one type and dropping the rest would return short
// pages that a client is told are full.
//
// They return the same Title shape as /search/multi (TMDB's movie and TV search
// payloads differ only in `title` vs `name`), with MediaType filled in because
// neither response carries it.
func (c *Client) SearchMovie(query string, page int) (*Paginated[Title], error) {
	return c.searchTyped("/search/movie", "movie", query, page)
}

func (c *Client) SearchTV(query string, page int) (*Paginated[Title], error) {
	return c.searchTyped("/search/tv", "show", query, page)
}

func (c *Client) searchTyped(path, mediaType, query string, page int) (*Paginated[Title], error) {
	p := url.Values{}
	p.Set("query", query)
	p.Set("page", fmt.Sprintf("%d", page))
	resp, err := c.do("GET", path, p)
	if err != nil {
		return nil, err
	}
	result, err := decode[Paginated[Title]](resp)
	if err != nil {
		return nil, err
	}
	for i := range result.Results {
		result.Results[i].MediaType = mediaType
	}
	return result, nil
}

func (c *Client) GetMovieChanges(page int, since string) (*ChangeFeed[ChangeEntry], error) {
	p := url.Values{"page": {fmt.Sprintf("%d", page)}}
	if since != "" {
		p.Set("start_date", since)
	}
	resp, err := c.do("GET", "/movie/changes", p)
	if err != nil {
		return nil, err
	}
	return decode[ChangeFeed[ChangeEntry]](resp)
}

func (c *Client) GetShowChanges(page int, since string) (*ChangeFeed[ChangeEntry], error) {
	p := url.Values{"page": {fmt.Sprintf("%d", page)}}
	if since != "" {
		p.Set("start_date", since)
	}
	resp, err := c.do("GET", "/tv/changes", p)
	if err != nil {
		return nil, err
	}
	return decode[ChangeFeed[ChangeEntry]](resp)
}

func (c *Client) GetPersonChanges(page int, since string) (*ChangeFeed[ChangeEntry], error) {
	p := url.Values{"page": {fmt.Sprintf("%d", page)}}
	if since != "" {
		p.Set("start_date", since)
	}
	resp, err := c.do("GET", "/person/changes", p)
	if err != nil {
		return nil, err
	}
	return decode[ChangeFeed[ChangeEntry]](resp)
}

// GetConfiguration returns the image CDN config (base URL, sizes).
func (c *Client) GetConfiguration() (*ImageConfig, error) {
	resp, err := c.do("GET", "/configuration", nil)
	if err != nil {
		return nil, err
	}
	type cfgWrap struct {
		Images ImageConfig `json:"images"`
	}
	w, err := decode[cfgWrap](resp)
	if err != nil {
		return nil, err
	}
	return &w.Images, nil
}

// --- v3 API methods (discover, popular, trending, credits) ---

// doV3 performs a TMDB v3 API request. Every method in this package goes
// through here now (the whole client is v3); it is kept as a named seam so the
// v3 call sites read as v3.
func (c *Client) doV3(method, path string, params url.Values) (*http.Response, error) {
	return c.do(method, path, params)
}

// MoviePopular returns the popular movies list (v3).
func (c *Client) MoviePopular(page int) (*Paginated[MovieListItem], error) {
	p := url.Values{"page": {fmt.Sprintf("%d", page)}}
	resp, err := c.doV3("GET", "/movie/popular", p)
	if err != nil {
		return nil, err
	}
	return decode[Paginated[MovieListItem]](resp)
}

// TVPopular returns the popular TV shows list (v3).
func (c *Client) TVPopular(page int) (*Paginated[TVListItem], error) {
	p := url.Values{"page": {fmt.Sprintf("%d", page)}}
	resp, err := c.doV3("GET", "/tv/popular", p)
	if err != nil {
		return nil, err
	}
	return decode[Paginated[TVListItem]](resp)
}

// Trending returns trending movies/TV/people (v3).
// mediaType: "all", "movie", "tv", "person"
// timeWindow: "day" or "week"
func (c *Client) Trending(mediaType, timeWindow string, page int) (*Paginated[TrendingItem], error) {
	p := url.Values{"page": {fmt.Sprintf("%d", page)}}
	resp, err := c.doV3("GET", fmt.Sprintf("/trending/%s/%s", mediaType, timeWindow), p)
	if err != nil {
		return nil, err
	}
	return decode[Paginated[TrendingItem]](resp)
}

// GetMovieCredits returns the cast and crew for a movie (v3).
func (c *Client) GetMovieCredits(id int) (*CreditsResponse, error) {
	resp, err := c.doV3("GET", fmt.Sprintf("/movie/%d/credits", id), nil)
	if err != nil {
		return nil, err
	}
	return decode[CreditsResponse](resp)
}

// GetShowCredits returns the cast and crew for a TV show (v3).
func (c *Client) GetShowCredits(id int) (*CreditsResponse, error) {
	resp, err := c.doV3("GET", fmt.Sprintf("/tv/%d/credits", id), nil)
	if err != nil {
		return nil, err
	}
	return decode[CreditsResponse](resp)
}

// GetMovieWatchProviders returns streaming/rent/buy availability for a movie (v3).
func (c *Client) GetMovieWatchProviders(movieID int) (*WatchProvidersResponse, error) {
	resp, err := c.doV3("GET", fmt.Sprintf("/movie/%d/watch/providers", movieID), nil)
	if err != nil {
		return nil, err
	}
	return decode[WatchProvidersResponse](resp)
}

// GetShowWatchProviders returns streaming/rent/buy availability for a TV show (v3).
func (c *Client) GetShowWatchProviders(showID int) (*WatchProvidersResponse, error) {
	resp, err := c.doV3("GET", fmt.Sprintf("/tv/%d/watch/providers", showID), nil)
	if err != nil {
		return nil, err
	}
	return decode[WatchProvidersResponse](resp)
}

// --- Reference / configuration API methods (v3) ---

// GetMovieGenres returns the list of movie genres from TMDB.
func (c *Client) GetMovieGenres() (*GenreList, error) {
	resp, err := c.doV3("GET", "/genre/movie/list", nil)
	if err != nil {
		return nil, err
	}
	return decode[GenreList](resp)
}

// GetTVGenres returns the list of TV show genres from TMDB.
func (c *Client) GetTVGenres() (*GenreList, error) {
	resp, err := c.doV3("GET", "/genre/tv/list", nil)
	if err != nil {
		return nil, err
	}
	return decode[GenreList](resp)
}

// GetConfigLanguages returns the list of supported languages from TMDB.
func (c *Client) GetConfigLanguages() ([]ConfigLanguage, error) {
	resp, err := c.doV3("GET", "/configuration/languages", nil)
	if err != nil {
		return nil, err
	}
	langs, err := decode[[]ConfigLanguage](resp)
	if err != nil {
		return nil, err
	}
	return *langs, nil
}

// GetConfigCountries returns the list of supported countries from TMDB.
func (c *Client) GetConfigCountries() ([]ConfigCountry, error) {
	resp, err := c.doV3("GET", "/configuration/countries", nil)
	if err != nil {
		return nil, err
	}
	countries, err := decode[[]ConfigCountry](resp)
	if err != nil {
		return nil, err
	}
	return *countries, nil
}

// GetV3 returns the raw response body from a TMDB v3 API path.
// Used for proxying endpoints like translations, alternative titles, etc.
func (c *Client) GetV3(path string) ([]byte, error) {
	resp, err := c.doV3("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

// GetMovieCertifications returns movie certifications grouped by country.
func (c *Client) GetMovieCertifications() (*CertificationsResponse, error) {
	resp, err := c.doV3("GET", "/certification/movie/list", nil)
	if err != nil {
		return nil, err
	}
	return decode[CertificationsResponse](resp)
}

// GetTVCertifications returns TV show certifications grouped by country.
func (c *Client) GetTVCertifications() (*CertificationsResponse, error) {
	resp, err := c.doV3("GET", "/certification/tv/list", nil)
	if err != nil {
		return nil, err
	}
	return decode[CertificationsResponse](resp)
}
