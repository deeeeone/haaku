package tmdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// Client talks to TMDB v4 API (api.themoviedb.org/4) using a v4 read-access
// token. Rate-limit aware; no caching (the metadata worker handles that).
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

// New creates a TMDB client. Reads TMDB_API_TOKEN from env.
func New() (*Client, error) {
	token := os.Getenv("TMDB_API_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("TMDB_API_TOKEN environment variable is required")
	}
	return &Client{
		baseURL: "https://api.themoviedb.org/4",
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		token: token,
	}, nil
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
		resp.Body.Close()
		return nil, fmt.Errorf("tmdb rate limited (429) on %s", path)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("tmdb error %d on %s: %s", resp.StatusCode, path, string(body))
	}
	return resp, nil
}

func decode[T any](resp *http.Response) (*T, error) {
	var v T
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, fmt.Errorf("failed to decode tmdb response: %w", err)
	}
	return &v, nil
}

// --- API methods ---

func (c *Client) GetMovie(id int) (*Movie, error) {
	resp, err := c.do("GET", fmt.Sprintf("/movie/%d", id), nil)
	if err != nil {
		return nil, err
	}
	return decode[Movie](resp)
}

func (c *Client) GetShow(id int) (*Show, error) {
	resp, err := c.do("GET", fmt.Sprintf("/tv/%d", id), nil)
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

const v3BaseURL = "https://api.themoviedb.org/3"

// doV3 performs a TMDB v3 API request.
func (c *Client) doV3(method, path string, params url.Values) (*http.Response, error) {
	u, err := url.Parse(v3BaseURL + path)
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
		return nil, fmt.Errorf("tmdb v3 request failed: %w", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		return nil, fmt.Errorf("tmdb rate limited (429) on %s", path)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("tmdb v3 error %d on %s: %s", resp.StatusCode, path, string(body))
	}
	return resp, nil
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
