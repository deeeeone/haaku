package tvdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is TVDB's v4 API root.
const DefaultBaseURL = "https://api4.thetvdb.com/v4"

// DefaultOrder is the episode ordering haaku addresses series in: TVDB's own
// recommended order, "default", which is the official/aired order for every
// series that has not chosen otherwise.
const DefaultOrder = "default"

// DefaultLanguage is the language haaku asks TVDB for. Names and overviews fall
// back to the series record's original language when a translation is missing,
// so this only decides which translation is preferred.
const DefaultLanguage = "eng"

const (
	requestTimeout = 20 * time.Second
	// tokenTTL is how long a cached login token is reused. TVDB tokens are
	// long lived but expire; refreshing well inside their lifetime keeps a
	// long-running worker from silently 401ing halfway through.
	tokenTTL = 12 * time.Hour
	// maxEpisodePages bounds the episode-pagination loop so a malformed links
	// block cannot spin. TVDB serves 500 episodes per page, so 20 pages is
	// 10,000 episodes — far past the longest series TVDB carries.
	maxEpisodePages = 20
)

// APIError is a non-2xx TVDB response. Status is the HTTP status; bodies are
// truncated because TVDB echoes the request back on some failures.
type APIError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("tvdb %s %s: status=%d body=%s", e.Method, e.Path, e.Status, e.Body)
}

// NotFoundError is a TVDB 404. Callers use it to tell "TVDB says this record
// does not exist" (an answer) from "the request failed" (the absence of one).
type NotFoundError struct{ APIError }

// IsNotFound reports whether err is a TVDB 404.
func IsNotFound(err error) bool {
	var nf *NotFoundError
	return errors.As(err, &nf)
}

// Client is a TVDB v4 API client. It is safe for concurrent use: the login
// token is cached behind a mutex and refreshed on expiry or on a 401.
type Client struct {
	baseURL string
	apiKey  string
	pin     string
	http    *http.Client

	mu      sync.Mutex
	token   string
	tokenAt time.Time
}

// New builds a client from the environment: TVDB_API_KEY is required and
// TVDB_PIN is optional (only projects that require a subscriber PIN need it).
//
// TVDB_API_BASE_URL overrides the API root, for a test server or a caching
// proxy in front of TVDB.
func New() (*Client, error) {
	key := strings.TrimSpace(os.Getenv("TVDB_API_KEY"))
	if key == "" {
		return nil, errors.New("TVDB_API_KEY environment variable is required (TVDB v4 API key)")
	}
	client := NewWithKey(key, strings.TrimSpace(os.Getenv("TVDB_PIN")))
	if base := strings.TrimSpace(os.Getenv("TVDB_API_BASE_URL")); base != "" {
		client.SetBaseURL(base)
	}
	return client, nil
}

// NewWithKey builds a client with an explicit API key, for tests and for
// callers that hold the credential somewhere other than the environment.
func NewWithKey(apiKey, pin string) *Client {
	return &Client{
		baseURL: DefaultBaseURL,
		apiKey:  strings.TrimSpace(apiKey),
		pin:     strings.TrimSpace(pin),
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// SetBaseURL points the client at another host — the test server in tests.
func (c *Client) SetBaseURL(base string) { c.baseURL = strings.TrimRight(base, "/") }

// SetHTTPClient replaces the underlying HTTP client (tests, custom transport).
func (c *Client) SetHTTPClient(h *http.Client) {
	if h != nil {
		c.http = h
	}
}

// --- API methods ---

// Search asks TVDB's name search for series and/or movies.
//
// mediaTypes are haaku media types ("movie", "show"); TVDB's own vocabulary
// ("movie", "series") is applied in the query. An empty list searches both.
// page is 1-based for callers and translated to TVDB's 0-based pages here.
//
// TVDB's `type` filter takes exactly ONE value — it is not a comma list. Asking
// for `type=movie,series` (or `type=all`) matches nothing and returns an empty
// page, which is what made a default "movie,show" search come back blank on a
// cold catalogue. So a multi-type request issues one search per type and merges
// the pages here, deduped by TVDB's objectID and trimmed to limit.
func (c *Client) Search(ctx context.Context, query string, mediaTypes []string, page, limit int) (*SearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return &SearchResponse{}, nil
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}

	types := tvdbSearchTypes(mediaTypes)
	if len(types) == 0 {
		// Empty list means "both": search each type rather than sending no
		// filter, so the page size stays predictable.
		types = []string{"movie", "series"}
	}
	if len(types) == 1 {
		return c.searchType(ctx, query, types[0], page, limit)
	}

	pages := make([][]SearchResult, 0, len(types))
	var totalItems int
	for _, searchType := range types {
		pageResult, err := c.searchType(ctx, query, searchType, page, limit)
		if err != nil {
			return nil, err
		}
		totalItems += pageResult.Links.TotalItems
		pages = append(pages, pageResult.Data)
	}

	// Interleave the per-type pages rather than concatenating them: "severance"
	// as movies alone would fill a short page and hide the series of the same
	// name, which is the hit the person was looking for.
	merged := SearchResponse{Data: make([]SearchResult, 0, limit)}
	seen := make(map[string]bool, limit)
	for depth := 0; len(merged.Data) < limit; depth++ {
		appended := false
		for _, pageResults := range pages {
			if depth >= len(pageResults) {
				continue
			}
			result := pageResults[depth]
			appended = true
			if key := result.ObjectID; key != "" {
				if seen[key] {
					continue
				}
				seen[key] = true
			}
			merged.Data = append(merged.Data, result)
			if len(merged.Data) >= limit {
				break
			}
		}
		if !appended {
			break
		}
	}
	merged.Links.TotalItems = totalItems
	merged.Status = "success"
	return &merged, nil
}

// searchType runs one TVDB name search for a single type ("movie" or "series").
func (c *Client) searchType(ctx context.Context, query, searchType string, page, limit int) (*SearchResponse, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("limit", strconv.Itoa(limit))
	params.Set("page", strconv.Itoa(page-1))
	params.Set("type", searchType)

	var out SearchResponse
	if err := c.do(ctx, http.MethodGet, "/search", params, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// tvdbSearchTypes maps haaku media types onto TVDB's search types. The result
// is one type per request, because TVDB's `type` filter is single-valued.
func tvdbSearchTypes(mediaTypes []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(mediaTypes))
	for _, t := range mediaTypes {
		var v string
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "movie":
			v = "movie"
		case "show", "series":
			v = "series"
		default:
			continue
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// GetSeriesExtended fetches a series' full record, including its remote ids
// (the TMDB/IMDb links), artwork and season list.
func (c *Client) GetSeriesExtended(ctx context.Context, seriesID int) (*SeriesExtended, error) {
	if seriesID <= 0 {
		return nil, errors.New("tvdb: series id is required")
	}
	var out struct {
		Data *SeriesExtended `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/series/%d/extended", seriesID), nil, nil, &out); err != nil {
		return nil, err
	}
	if out.Data == nil {
		return nil, fmt.Errorf("tvdb: series %d returned no record", seriesID)
	}
	return out.Data, nil
}

// GetSeriesEpisodes returns every episode of a series in the given ordering,
// following TVDB's pagination. order "" means DefaultOrder.
func (c *Client) GetSeriesEpisodes(ctx context.Context, seriesID int, order, language string) ([]Episode, error) {
	if seriesID <= 0 {
		return nil, errors.New("tvdb: series id is required")
	}
	if strings.TrimSpace(order) == "" {
		order = DefaultOrder
	}
	if strings.TrimSpace(language) == "" {
		language = DefaultLanguage
	}

	var all []Episode
	for page := 0; page < maxEpisodePages; page++ {
		params := url.Values{}
		params.Set("page", strconv.Itoa(page))

		var out episodeListResponse
		path := fmt.Sprintf("/series/%d/episodes/%s/%s", seriesID, order, language)
		if err := c.do(ctx, http.MethodGet, path, params, nil, &out); err != nil {
			// TVDB answers a series that carries no episodes in an ordering
			// with a 404 on this path as well as with an empty list. Both
			// mean "no episodes here", not a failure.
			if IsNotFound(err) && len(all) == 0 {
				return nil, nil
			}
			return nil, err
		}
		all = append(all, out.Data.Episodes...)
		if out.Links.Next == nil || len(out.Data.Episodes) == 0 {
			break
		}
	}
	return all, nil
}

// GetMovieExtended fetches a movie's full record.
//
// TVDB keeps a movie's overview and tagline out of the extended record, so the
// English translation is fetched and merged in. A failed translation call is
// NOT fatal: the record is still complete enough to ingest (TMDB supplements
// the text), so the error is dropped and the caller sees empty text.
func (c *Client) GetMovieExtended(ctx context.Context, movieID int) (*MovieExtended, error) {
	if movieID <= 0 {
		return nil, errors.New("tvdb: movie id is required")
	}
	var out struct {
		Data *MovieExtended `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/movies/%d/extended", movieID), nil, nil, &out); err != nil {
		return nil, err
	}
	if out.Data == nil {
		return nil, fmt.Errorf("tvdb: movie %d returned no record", movieID)
	}

	if translation, err := c.GetMovieTranslation(ctx, movieID, DefaultLanguage); err == nil && translation != nil {
		out.Data.OverviewText = translation.Overview
		out.Data.TaglineText = translation.Tagline
		if out.Data.Name == "" {
			out.Data.Name = translation.Name
		}
	}
	return out.Data, nil
}

// GetMovieTranslation fetches a movie's translated name/overview/tagline.
func (c *Client) GetMovieTranslation(ctx context.Context, movieID int, language string) (*MovieTranslation, error) {
	if movieID <= 0 {
		return nil, errors.New("tvdb: movie id is required")
	}
	if strings.TrimSpace(language) == "" {
		language = DefaultLanguage
	}
	var out struct {
		Data *MovieTranslation `json:"data"`
	}
	path := fmt.Sprintf("/movies/%d/translations/%s", movieID, language)
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// --- transport ---

// do performs one authenticated request. A 401 invalidates the cached token
// and the request is retried once, which is what makes a token that expired
// between the TTL check and the call invisible to callers.
func (c *Client) do(ctx context.Context, method, path string, params url.Values, body, out any) error {
	err := c.doOnce(ctx, method, path, params, body, out)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
		c.invalidateToken()
		return c.doOnce(ctx, method, path, params, body, out)
	}
	return err
}

func (c *Client) doOnce(ctx context.Context, method, path string, params url.Values, body, out any) error {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("tvdb: invalid URL for %s: %w", path, err)
	}
	if params != nil {
		u.RawQuery = params.Encode()
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("tvdb: encode %s body: %w", path, err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return fmt.Errorf("tvdb: build %s request: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if path != "/login" {
		token, err := c.ensureToken(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("tvdb: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("tvdb: read %s response: %w", path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{
			Method: method,
			Path:   path,
			Status: resp.StatusCode,
			Body:   truncate(strings.TrimSpace(string(raw)), 1500),
		}
		if resp.StatusCode == http.StatusNotFound {
			return &NotFoundError{*apiErr}
		}
		return apiErr
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("tvdb: decode %s response: %w", path, err)
	}
	return nil
}

const userAgent = "haaku/1.0 (+https://haaku.tv)"

// ensureToken returns a cached login token, logging in when there is none or
// the cached one is older than tokenTTL.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && time.Since(c.tokenAt) < tokenTTL {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	if c.apiKey == "" {
		return "", errors.New("tvdb: an API key is required")
	}

	payload := map[string]string{"apikey": c.apiKey}
	if c.pin != "" {
		payload["pin"] = c.pin
	}

	var out struct {
		Status string `json:"status"`
		Data   struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := c.doOnce(ctx, http.MethodPost, "/login", nil, payload, &out); err != nil {
		return "", fmt.Errorf("tvdb: login failed: %w", err)
	}
	token := strings.TrimSpace(out.Data.Token)
	if token == "" {
		return "", errors.New("tvdb: login succeeded but returned an empty token")
	}

	c.mu.Lock()
	c.token, c.tokenAt = token, time.Now()
	c.mu.Unlock()
	return token, nil
}

func (c *Client) invalidateToken() {
	c.mu.Lock()
	c.token, c.tokenAt = "", time.Time{}
	c.mu.Unlock()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
