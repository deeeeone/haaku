package reference

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/external/tmdb"
)

func TestSlugify(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Action", "action"},
		{"Action & Adventure", "action-and-adventure"},
		{"SCIENCE FICTION", "science-fiction"},
		{"Sci-Fi", "sci-fi"},
		{"", ""},
		{"  Spaces  ", "--spaces--"}, // every space becomes a hyphen (impl behavior)
	}
	for _, tc := range cases {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCacheKey(t *testing.T) {
	if got := cacheKey("genres", "movie"); got != "reference:genres:movie" {
		t.Errorf("cacheKey = %q", got)
	}
	if got := cacheKey("certifications", ""); got != "reference:certifications:" {
		t.Errorf("cacheKey(empty subtype) = %q", got)
	}
}

// TestCertificationCatalog pins the transform that backs GET
// /v1/certifications/movies and /shows: TMDB's per-country list becomes Trakt's
// country-keyed object of {name, slug, description}. The US entry is the one the
// vendored contract marks required, so a regression there is the one that would
// make a Trakt client's filter UI come up empty.
func TestCertificationCatalog(t *testing.T) {
	resp := &tmdb.CertificationsResponse{
		Certifications: map[string][]tmdb.Certification{
			"US": {
				{Certification: "G", Meaning: "General Audiences", Order: 1},
				{Certification: "PG", Meaning: "Parental Guidance Suggested", Order: 2},
				{Certification: "PG-13", Meaning: "Parents Strongly Cautioned", Order: 3},
				{Certification: "R", Meaning: "Restricted", Order: 4},
				{Certification: "NC-17", Meaning: "Adults Only", Order: 5},
				{Certification: "", Meaning: "ignored", Order: 6}, // empty name is dropped
			},
			"GB": {
				{Certification: "15", Meaning: "Suitable for 15 years and over", Order: 5},
			},
		},
	}

	got := certificationCatalog(resp)

	// Country codes are lowercased so a client keys by the contract's own codes.
	if _, ok := got["US"]; ok {
		t.Error("catalog kept the upper-case country key US")
	}
	us, ok := got["us"]
	if !ok {
		t.Fatalf("catalog missing required country key 'us'; got keys %v", catalogKeys(got))
	}
	if len(us) != 5 {
		t.Fatalf("us has %d certifications, want 5 (empty name dropped): %v", len(us), us)
	}

	want := []CertificationOption{
		{Name: "G", Slug: "g", Description: "General Audiences"},
		{Name: "PG", Slug: "pg", Description: "Parental Guidance Suggested"},
		{Name: "PG-13", Slug: "pg-13", Description: "Parents Strongly Cautioned"},
		{Name: "R", Slug: "r", Description: "Restricted"},
		{Name: "NC-17", Slug: "nc-17", Description: "Adults Only"},
	}
	// Order is the order TMDB returned the fixtures in.
	for i, w := range want {
		if us[i] != w {
			t.Errorf("us[%d] = %+v, want %+v", i, us[i], w)
		}
	}

	gb := got["gb"]
	if len(gb) != 1 || gb[0] != (CertificationOption{Name: "15", Slug: "15", Description: "Suitable for 15 years and over"}) {
		t.Errorf("gb = %+v, want one entry {15 15 Suitable for 15 years and over}", gb)
	}

	// Every entry must carry the contract's three required string fields.
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	var raw map[string][]map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal catalog: %v", err)
	}
	for country, entries := range raw {
		for i, e := range entries {
			for _, field := range []string{"name", "slug", "description"} {
				if _, ok := e[field]; !ok {
					t.Errorf("%s[%d] missing required field %q", country, i, field)
				}
			}
		}
	}
}

func catalogKeys(c CertificationCatalog) []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// TestCertificationLiteralRoutesServe exercises the two registered handlers end
// to end against a fake TMDB. It proves the literal routes actually reach their
// handler (i.e. /certifications/:type did not shadow them) and that the wire body
// is the contract's country-keyed object, not the pre-existing array envelope.
func TestCertificationLiteralRoutesServe(t *testing.T) {
	t.Setenv("TMDB_API_KEY", "test-token-for-fake-tmdb")

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/certification/movie/list":
			_, _ = io.WriteString(w, `{"certifications":{"US":[{"certification":"G","meaning":"General Audiences","order":1},{"certification":"PG-13","meaning":"Parents Strongly Cautioned","order":3}],"GB":[{"certification":"15","meaning":"Suitable for 15 years and over","order":5}]}}`)
		case "/certification/tv/list":
			_, _ = io.WriteString(w, `{"certifications":{"US":[{"certification":"TV-MA","meaning":"Mature Audience Only","order":5}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	client, err := getTMDBClient()
	if err != nil {
		t.Fatalf("getTMDBClient: %v", err)
	}
	client.SetBaseURL(fake.URL)
	t.Cleanup(func() {
		// Drop the singleton so a later test re-reads the restored environment.
		tmdbOnce = sync.Once{}
		tmdbClient, tmdbClientErr = nil, nil
	})

	app := fiber.New()
	RegisterRoutes(app.Group("/v1"))

	fetch := func(path string) map[string][]CertificationOption {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp, err := app.Test(req, 5000)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d", path, resp.StatusCode)
		}
		var got map[string][]CertificationOption
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("GET %s: decode: %v", path, err)
		}
		return got
	}

	movies := fetch("/v1/certifications/movies")
	if len(movies["us"]) != 2 || movies["us"][0].Name != "G" {
		t.Errorf("movies us = %+v, want [G, PG-13]", movies["us"])
	}
	if _, ok := movies["gb"]; !ok {
		t.Errorf("movies missing gb group: %v", movies)
	}

	// TV has TV-MA and no GB: if the :type route had swallowed the literal
	// route (or vice versa) the two responses would be identical.
	shows := fetch("/v1/certifications/shows")
	if len(shows["us"]) != 1 || shows["us"][0].Name != "TV-MA" {
		t.Errorf("shows us = %+v, want [TV-MA]", shows["us"])
	}
	if _, ok := shows["gb"]; ok {
		t.Errorf("shows unexpectedly has a gb group: %v", shows)
	}
}
