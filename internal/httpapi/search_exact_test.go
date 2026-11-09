package httpapi

import (
	"net/http"
	"net/url"
	"testing"
)

func TestSearch70ExactMatchesTitleCaseInsensitively(t *testing.T) {
	app, db := newSearch70App(t)
	seedSearch70(t, db)

	for _, query := range []string{"batman begins", "BATMAN BEGINS", "BaTmAn BeGiNs"} {
		resp, raw := doSearch70(t, app, http.MethodGet, "/v1/search/movie/exact?query="+url.QueryEscape(query), "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET exact %q = %d, want 200 (%s)", query, resp.StatusCode, raw)
		}
		items := decodeSearch70Array(t, raw)
		if len(items) != 1 {
			t.Fatalf("GET exact %q returned %d items, want 1 (%s)", query, len(items), raw)
		}
		if typ := items[0]["type"]; typ != "movie" {
			t.Fatalf("type = %v, want movie", typ)
		}
		movie, ok := items[0]["movie"].(map[string]any)
		if !ok {
			t.Fatalf("item has no movie object: %v", items[0])
		}
		if movie["title"] != "Batman Begins" {
			t.Fatalf("movie.title = %v, want Batman Begins", movie["title"])
		}
	}
}

func TestSearch70ExactDoesNotReturnPartialMatches(t *testing.T) {
	app, db := newSearch70App(t)
	seedSearch70(t, db)

	resp, raw := doSearch70(t, app, http.MethodGet, "/v1/search/movie/exact?query=Batman", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET exact partial = %d, want 200 (%s)", resp.StatusCode, raw)
	}
	if items := decodeSearch70Array(t, raw); len(items) != 0 {
		t.Fatalf("partial title matched %d items, want 0 (%s)", len(items), raw)
	}
}

func TestSearch70ExactRejectsUnsupportedType(t *testing.T) {
	app, db := newSearch70App(t)
	seedSearch70(t, db)

	resp, raw := doSearch70(t, app, http.MethodGet, "/v1/search/person/exact?query=Christian+Bale", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET exact person = %d, want 400 (%s)", resp.StatusCode, raw)
	}
}

func TestSearch70ExactDoesNotShadowIDLookup(t *testing.T) {
	app, db := newSearch70App(t)
	seedSearch70(t, db)

	resp, raw := doSearch70(t, app, http.MethodGet, "/v1/search/tmdb/272", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /search/tmdb/272 = %d, want 200 (%s)", resp.StatusCode, raw)
	}
	items := decodeSearch70Array(t, raw)
	if len(items) != 1 {
		t.Fatalf("id lookup returned %d items, want 1 (%s)", len(items), raw)
	}
	movie, ok := items[0]["movie"].(map[string]any)
	if !ok {
		t.Fatalf("id lookup has no movie object: %v", items[0])
	}
	if movie["title"] != "Batman Begins" {
		t.Fatalf("id lookup movie.title = %v, want Batman Begins", movie["title"])
	}
}
