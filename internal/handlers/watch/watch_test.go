package watch

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"trakt-api/external/tmdb"
	"trakt-api/internal/database"

	"github.com/gofiber/fiber/v2"
)

// cloneMeta simulates a fresh DB read of the metadata JSONB column for a new
// request (GORM scans the stored column into a distinct map each time).
func cloneMeta(in database.JSONMap) (database.JSONMap, error) {
	data, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out database.JSONMap
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// makeWatchApp registers a GET /watch handler that resolves watch providers for
// a given country against a simulated DB row (dbMeta), persisting any updated
// metadata back to dbMeta on each request — mirroring what handleMovieWatch/
// handleShowWatch do with db.Model(&row).Update("metadata", row.Metadata).
func makeWatchApp(dbMeta *database.JSONMap, fetchCalls *int, mediaType string, tmdbID int) *fiber.App {
	app := fiber.New()
	app.Get("/watch", func(c *fiber.Ctx) error {
		meta, err := cloneMeta(*dbMeta)
		if err != nil {
			return err
		}
		persist := func() error {
			stored, err := cloneMeta(meta)
			if err != nil {
				return err
			}
			*dbMeta = stored
			return nil
		}
		fetchFresh := func() (*tmdb.WatchProvidersResponse, error) {
			*fetchCalls++
			return &tmdb.WatchProvidersResponse{
				ID: tmdbID,
				Results: map[string]tmdb.CountryProviders{
					"US": {
						Link:     "https://www.themoviedb.org/movie/1/watch?locale=US",
						Flatrate: []tmdb.Provider{{ProviderID: 8, ProviderName: "Netflix", LogoPath: "/netflix.png"}},
					},
					"GB": {
						Link: "https://www.themoviedb.org/movie/1/watch?locale=GB",
					},
				},
			}, nil
		}
		if mediaType == "show" {
			fetchFresh = func() (*tmdb.WatchProvidersResponse, error) {
				*fetchCalls++
				return &tmdb.WatchProvidersResponse{
					ID: tmdbID,
					Results: map[string]tmdb.CountryProviders{
						"US": {Link: "https://www.themoviedb.org/tv/1/watch?locale=US"},
					},
				}, nil
			}
		}
		return serveWatchProviders(c, meta, c.Query("country", "us"), mediaType, persist, fetchFresh)
	})
	return app
}

// TestWatchProvidersCachePersistedAcrossRequests is the regression test for the
// watch cache-persistence bug: the provider set must be written back to the DB
// metadata column after a TMDB fetch so that a subsequent request re-reads it
// from the row and does NOT hit TMDB a second time.
func TestWatchProvidersCachePersistedAcrossRequests(t *testing.T) {
	dbMeta := database.JSONMap{}
	fetchCalls := 0
	app := makeWatchApp(&dbMeta, &fetchCalls, "movie", 1)

	// Request 1: cold cache -> must fetch TMDB once and persist to the DB row.
	resp1, err := app.Test(httptestReq("/watch?country=US"))
	if err != nil {
		t.Fatalf("request 1 failed: %v", err)
	}
	defer func() { _ = resp1.Body.Close() }()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("request 1 status = %d, want 200", resp1.StatusCode)
	}
	if fetchCalls != 1 {
		t.Fatalf("request 1 TMDB fetch calls = %d, want 1", fetchCalls)
	}
	if _, ok := dbMeta["watch_providers"]; !ok {
		t.Fatalf("watch_providers was NOT persisted to the DB metadata after request 1 — cache is inert")
	}

	// Request 2: cache now lives in the DB row -> re-read from DB, no TMDB fetch.
	resp2, err := app.Test(httptestReq("/watch?country=US"))
	if err != nil {
		t.Fatalf("request 2 failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("request 2 status = %d, want 200", resp2.StatusCode)
	}
	if fetchCalls != 1 {
		t.Fatalf("second request refetched TMDB (calls=%d) — persisted cache was not re-read from DB", fetchCalls)
	}

	// The served body for request 2 must contain the cached provider.
	var w WatchResponse
	if err := json.NewDecoder(resp2.Body).Decode(&w); err != nil {
		t.Fatalf("decode request 2 body: %v", err)
	}
	if len(w.Flatrate) == 0 || w.Flatrate[0].Name != "Netflix" {
		t.Fatalf("request 2 served providers = %+v, want Netflix from cache", w.Flatrate)
	}
}

// TestWatchProvidersNegativeCaching confirms a country absent from cached results
// does not trigger a TMDB refetch on repeated requests within the negative TTL.
func TestWatchProvidersNegativeCaching(t *testing.T) {
	dbMeta := database.JSONMap{}
	fetchCalls := 0
	app := makeWatchApp(&dbMeta, &fetchCalls, "movie", 1)

	// First request for a country with no providers (TMDB returns US/GB only).
	resp1, err := app.Test(httptestReq("/watch?country=CA"))
	if err != nil {
		t.Fatalf("request 1 failed: %v", err)
	}
	_ = resp1.Body.Close()
	if fetchCalls != 1 {
		t.Fatalf("first absent-country request fetch calls = %d, want 1", fetchCalls)
	}
	if _, ok := dbMeta["watch_providers_absence"]; !ok {
		t.Fatalf("absence marker was not persisted for the missing country")
	}

	// Second request: the negative should be honored -> still one TMDB fetch total.
	resp2, err := app.Test(httptestReq("/watch?country=CA"))
	if err != nil {
		t.Fatalf("request 2 failed: %v", err)
	}
	_ = resp2.Body.Close()
	if fetchCalls != 1 {
		t.Fatalf("repeated absent-country request refetched TMDB (calls=%d) — negative cache not honored", fetchCalls)
	}

	// A different absent country is not negatively cached yet -> triggers a refetch.
	resp3, err := app.Test(httptestReq("/watch?country=DE"))
	if err != nil {
		t.Fatalf("request 3 failed: %v", err)
	}
	_ = resp3.Body.Close()
	if fetchCalls != 2 {
		t.Fatalf("new absent-country request fetch calls = %d, want 2", fetchCalls)
	}
}

// TestWatchProvidersAbsenceExpires confirms the negative cache is TTL-bound: once
// the absence marker is older than countryAbsenceTTL, the country is re-checked.
func TestWatchProvidersAbsenceExpires(t *testing.T) {
	dbMeta := database.JSONMap{}
	fetchCalls := 0
	app := makeWatchApp(&dbMeta, &fetchCalls, "movie", 1)

	// Seed a valid watch_providers blob that does NOT contain "CA", plus an
	// absence marker for CA that is already expired.
	blob := mustPersistWatchProviders(map[string]string{"US": "us"})
	absence := database.JSONMap{metadataAbsenceKey: database.JSONMap{
		"CA": time.Now().UTC().Add(-countryAbsenceTTL - time.Minute).Format(time.RFC3339),
	}}
	for k, v := range blob {
		dbMeta[k] = v
	}
	for k, v := range absence {
		dbMeta[k] = v
	}

	resp, err := app.Test(httptestReq("/watch?country=CA"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if fetchCalls != 1 {
		t.Fatalf("expired negative was not re-checked: fetch calls = %d, want 1", fetchCalls)
	}
}

// TestWatchShowCountryMissLinkIsTV confirms the show branch of the country-miss
// path builds a /tv/ TMDB watch link, not a /movie/ link. Regression for the
// hardcoded "movie" passed to cpLink in the show country-miss path.
func TestWatchShowCountryMissLinkIsTV(t *testing.T) {
	dbMeta := database.JSONMap{}
	fetchCalls := 0
	// The makeWatchApp "show" fetch returns only US providers (id=1).
	app := makeWatchApp(&dbMeta, &fetchCalls, "show", 1)

	resp, err := app.Test(httptestReq("/watch?country=CA"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var w WatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&w); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if w.Link == "" {
		t.Fatalf("expected a TMDB watch link for the country-miss show path, got empty link")
	}
	if !strings.Contains(w.Link, "/tv/1/watch") {
		t.Fatalf("show country-miss link = %q, want /tv/1/watch link (movie link would be wrong)", w.Link)
	}
	if strings.Contains(w.Link, "/movie/") {
		t.Fatalf("show country-miss link = %q, must NOT point at /movie/", w.Link)
	}
}

func httptestReq(target string) *http.Request {
	req, _ := http.NewRequest("GET", target, nil)
	req.Host = "example.com"
	return req
}

// mustPersistWatchProviders returns a stored watch_providers blob (fresh synced
// time) so loadCachedProviders treats it as valid, keyed by the given country
// links.
func mustPersistWatchProviders(results map[string]string) database.JSONMap {
	blob := cachedWatchProviders{
		SyncedAt: time.Now().UTC().Format(time.RFC3339),
		Results:  make(map[string]cachedCountryResult),
	}
	for code, link := range results {
		blob.Results[code] = cachedCountryResult{Link: link}
	}
	data, err := json.Marshal(blob)
	if err != nil {
		panic(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		panic(err)
	}
	return database.JSONMap{"watch_providers": raw}
}
