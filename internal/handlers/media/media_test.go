package media

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"trakt-api/internal/database"

	"github.com/gofiber/fiber/v2"
)

// ---------------------------------------------------------------------------
// media/id_resolver.go — isNumeric
// ---------------------------------------------------------------------------

func TestIsNumeric(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"0", true},
		{"123", true},
		{"999999999", true},
		{"12a", false},
		{"a", false},
		{" 1", false},
		{"1.5", false},
		{"-1", false},
	}
	for _, tc := range cases {
		if got := isNumeric(tc.in); got != tc.want {
			t.Errorf("isNumeric(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// media/responses.go — response builders
// ---------------------------------------------------------------------------

func TestBuildRatingSummary(t *testing.T) {
	rs := buildRatingSummary(7.5, 100)
	if rs == nil {
		t.Fatal("buildRatingSummary returned nil")
	}
	if rs.Rating != 7.5 || rs.Votes != 100 {
		t.Errorf("rating summary = %+v, want rating 7.5 votes 100", rs)
	}
	if rs.Distribution == nil {
		t.Error("Distribution must never be nil (MarshalJSON contract)")
	}
	if len(rs.Distribution) != 0 {
		t.Errorf("Distribution should start empty, got %v", rs.Distribution)
	}
}

func TestBuildMovieResponseMinAndFull(t *testing.T) {
	rel := database.Date{time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)}
	m := &database.Movie{
		ID:            1,
		TmdbID:        550,
		ImdbID:        "tt0137523",
		Title:         "Fight Club",
		Overview:      "An insomniac.",
		Released:      &rel,
		Runtime:       139,
		RatingAvg:     8.4,
		RatingCount:   25000,
		Certification: "R",
		Language:      "en",
	}

	minResp := buildMovieResponse(nil, m, "min")
	if minResp.IDs.ID != 1 || minResp.IDs.TMDB != 550 || minResp.IDs.IMDB != "tt0137523" {
		t.Errorf("min IDs wrong: %+v", minResp.IDs)
	}
	if minResp.Title != "Fight Club" || minResp.Year != 2020 {
		t.Errorf("min title/year wrong: %q %d", minResp.Title, minResp.Year)
	}
	if minResp.RatingSummary == nil || minResp.RatingSummary.Rating != 8.4 {
		t.Errorf("min rating summary wrong: %+v", minResp.RatingSummary)
	}
	// Extended fields must be absent at "min".
	if minResp.Overview != "" || minResp.Runtime != 0 || minResp.Genres != nil {
		t.Errorf("min extended fields leaked: %+v", minResp)
	}

	fullResp := buildMovieResponse(nil, m, "full")
	if fullResp.Overview != "An insomniac." || fullResp.Runtime != 139 {
		t.Errorf("full extended fields wrong: overview=%q runtime=%d", fullResp.Overview, fullResp.Runtime)
	}
	if fullResp.Certification != "R" || fullResp.Language != "en" {
		t.Errorf("full certification/language wrong: %q %q", fullResp.Certification, fullResp.Language)
	}
}

func TestBuildShowResponseExtended(t *testing.T) {
	first := database.Date{time.Date(2011, 4, 17, 0, 0, 0, 0, time.UTC)}
	s := &database.Show{
		ID:           2,
		TmdbID:       1399,
		Title:        "Game of Thrones",
		FirstAired:   &first,
		EpisodeCount: 73,
		SeasonCount:  8,
		Network:      "HBO",
		Status:       "ended",
		RatingAvg:    9.3,
		RatingCount:  15000,
	}

	allResp := buildShowResponse(nil, s, "all")
	if allResp.IDs.TMDB != 1399 {
		t.Errorf("show TMDB id = %d, want 1399", allResp.IDs.TMDB)
	}
	if allResp.Year != 2011 {
		t.Errorf("show year = %d, want 2011", allResp.Year)
	}
	if allResp.EpisodeCount != 73 || allResp.SeasonCount != 8 || allResp.Network != "HBO" {
		t.Errorf("show full fields wrong: %+v", allResp)
	}
	if allResp.Status != "ended" {
		t.Errorf("show status = %q, want ended", allResp.Status)
	}

	// "min" drops the full extended fields.
	minResp := buildShowResponse(nil, s, "min")
	if minResp.EpisodeCount != 0 || minResp.Network != "" {
		t.Errorf("min show extended fields leaked: %+v", minResp)
	}
}

func TestBuildSeasonAndEpisodeResponse(t *testing.T) {
	aired := database.Date{time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)}
	season := &database.Season{
		ID:           10,
		ShowID:       3,
		SeasonNumber: 2,
		EpisodeCount: 10,
		AiredAt:      &aired,
	}
	sResp := buildSeasonResponse(nil, season)
	if sResp.ShowID != 3 || sResp.SeasonNumber != 2 || sResp.EpisodeCount != 10 {
		t.Errorf("season response wrong: %+v", sResp)
	}
	if sResp.AiredAt != "2019-01-01" {
		t.Errorf("season aired_at = %q, want 2019-01-01", sResp.AiredAt)
	}
	if sResp.Images == nil {
		t.Error("season Images must be non-nil")
	}

	ep := &database.Episode{
		ID:            20,
		SeasonID:      10,
		ShowID:        3,
		EpisodeNumber: 5,
		Title:         "The Bells",
		Overview:      "The bells ring.",
		Runtime:       60,
	}
	eResp := buildEpisodeResponse(nil, ep)
	if eResp.ShowID != 3 || eResp.SeasonID != 10 || eResp.EpisodeNumber != 5 {
		t.Errorf("episode response wrong: %+v", eResp)
	}
	if eResp.Title != "The Bells" || eResp.Overview != "The bells ring." || eResp.Runtime != 60 {
		t.Errorf("episode content wrong: %+v", eResp)
	}
}

// TestMovieResponseMarshalJSONNeverNilMaps guards the "maps are never nil"
// contract on the wire: a MovieResponse with nil Images/Genres must still
// serialize Images as {} and Genres as [] (regression for empty responses).
func TestMovieResponseMarshalJSONNeverNilMaps(t *testing.T) {
	resp := MovieResponse{Title: "x"} // Images and Genres intentionally nil
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(m["images"]) != "{}" {
		t.Errorf(`images = %s, want {}`, m["images"])
	}
	// Genres carries omitempty: an empty (or nil) slice is dropped entirely,
	// never serialized as null.
	if g, ok := m["genres"]; ok {
		if string(g) == "null" {
			t.Errorf(`genres = null, want omitted or []`)
		}
	}
}

func TestGetExtended(t *testing.T) {
	app := fiber.New()
	app.Get("/ext", func(c *fiber.Ctx) error {
		return c.SendString(GetExtended(c))
	})

	cases := []struct {
		query string
		want  string
	}{
		{"", "min"},       // default
		{"?extended=full", "full"},
		{"?extended=metadata", "metadata"},
		{"?extended=images", "images"},
		{"?extended=all", "all"},
		{"?extended=bogus", "min"}, // unknown falls back
		{"?extended=full&x=1", "full"},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest("GET", "/ext"+tc.query, nil)
		req.Host = "example.com"
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request %q failed: %v", tc.query, err)
		}
		buf := make([]byte, 16)
		n, _ := resp.Body.Read(buf)
		resp.Body.Close()
		got := string(buf[:n])
		if got != tc.want {
			t.Errorf("GetExtended(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// media/translations.go — pure TMDB JSON → response builders
// ---------------------------------------------------------------------------

func TestFilterTranslations(t *testing.T) {
	raw := []byte(`{"translations":[
		{"iso_639_1":"en","iso_3166_1":"US","name":"English","data":{"title":"Title EN","overview":"Overview EN","tagline":"Tagline"}},
		{"iso_639_1":"es","iso_3166_1":"ES","name":"Spanish","data":{"title":"","name":"Nombre","overview":"Sinopsis","tagline":""}},
		{"iso_639_1":"fr","iso_3166_1":"FR","name":"French","data":{"title":"Titre FR","overview":"","tagline":""}}
	]}`)

	out, err := filterTranslations(raw, "es")
	if err != nil {
		t.Fatalf("filterTranslations: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("es filter returned %d, want 1", len(out))
	}
	// Spanish uses Name when Title is empty.
	if out[0].Title != "Nombre" || out[0].Overview != "Sinopsis" {
		t.Errorf("es translation = %+v", out[0])
	}
	if out[0].Language != "es" {
		t.Errorf("es language = %q", out[0].Language)
	}

	// Case-insensitive lang match on iso_639_1.
	outUS, err := filterTranslations(raw, "en")
	if err != nil {
		t.Fatalf("filterTranslations: %v", err)
	}
	if len(outUS) != 1 || outUS[0].Title != "Title EN" {
		t.Errorf("en filter = %+v, want Title EN", outUS)
	}
}

func TestFilterTranslationsNoMatchReturnsEmptySlice(t *testing.T) {
	raw := []byte(`{"translations":[{"iso_639_1":"de","iso_3166_1":"DE","data":{"title":"Deutsch"}}]}`)
	out, err := filterTranslations(raw, "pt")
	if err != nil {
		t.Fatalf("filterTranslations: %v", err)
	}
	if out == nil {
		t.Fatal("filterTranslations must return a non-nil empty slice (not nil) so it serializes as []")
	}
	if len(out) != 0 {
		t.Errorf("out = %+v, want empty", out)
	}
}

func TestFilterTranslationsBadJSON(t *testing.T) {
	if _, err := filterTranslations([]byte(`not json`), "en"); err == nil {
		t.Error("filterTranslations expected error on malformed JSON")
	}
}

func TestParseAliases(t *testing.T) {
	raw := []byte(`{"titles":[
		{"iso_3166_1":"US","title":"Fight Club","type":"original"},
		{"iso_3166_1":"BR","title":"Clube da Luta","type":"custom"}
	]}`)
	out := parseAliases(raw)
	if len(out) != 2 {
		t.Fatalf("parseAliases returned %d, want 2", len(out))
	}
	if out[0].Title != "Fight Club" || out[0].Country != "US" {
		t.Errorf("alias[0] = %+v", out[0])
	}
	if out[1].Title != "Clube da Luta" || out[1].Country != "BR" {
		t.Errorf("alias[1] = %+v", out[1])
	}
}

func TestParseAliasesBadJSONReturnsEmpty(t *testing.T) {
	out := parseAliases([]byte(`{bad`))
	if out == nil {
		t.Fatal("parseAliases must return a non-nil empty slice on bad JSON")
	}
	if len(out) != 0 {
		t.Errorf("out = %+v, want empty", out)
	}
}

func TestParseAliasesEmptyTitlesReturnsEmptySlice(t *testing.T) {
	out := parseAliases([]byte(`{"titles":[]}`))
	if out == nil {
		t.Fatal("parseAliases must return non-nil empty slice")
	}
	if len(out) != 0 {
		t.Errorf("out = %+v, want empty", out)
	}
}
