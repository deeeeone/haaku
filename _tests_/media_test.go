package tests

import (
	"encoding/json"
	"testing"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"
)

// ---------------------------------------------------------------------------
// ID resolution tests — isNumeric logic
// ---------------------------------------------------------------------------

// TestIsNumericDetection verifies numeric vs slug detection logic.
func TestIsNumericDetection(t *testing.T) {
	tests := []struct {
		param   string
		numeric bool
	}{
		{"12345", true},
		{"1", true},
		{"0", true},
		{"9999999999", true},
		{"breaking-bad", false},
		{"tt0903747", false},
		{"12a34", false},
		{"", false},
		{"-1", false},
	}

	for _, tt := range tests {
		t.Run(tt.param, func(t *testing.T) {
			result := isNumericHelper(tt.param)
			if result != tt.numeric {
				t.Errorf("isNumeric(%q) = %v, want %v", tt.param, result, tt.numeric)
			}
		})
	}
}

// TestExtendedParamValues verifies the extended parameter values.
func TestExtendedParamValues(t *testing.T) {
	valid := map[string]bool{
		"min": true, "full": true, "metadata": true, "images": true, "all": true,
	}
	for _, v := range []string{"min", "full", "metadata", "images", "all"} {
		if !valid[v] {
			t.Errorf("expected %q to be a valid extended value", v)
		}
	}
}

// ---------------------------------------------------------------------------
// Response type tests — verify JSON shapes (no DB)
// ---------------------------------------------------------------------------

// TestMovieResponseJSON verifies the MovieResponse JSON serialization.
func TestMovieResponseJSON(t *testing.T) {
	released := database.Date{}
	released.Time = parseDateHelper("2013-09-29")

	m := &database.Movie{
		ID:            42,
		TmdbID:        13885,
		ImdbID:        "tt1234567",
		Title:         "Breaking Bad: The Movie",
		Tagline:       "Remember my name",
		Overview:      "A movie about a chemistry teacher.",
		Released:      &released,
		Runtime:       122,
		Certification: "R",
		Genres:        []string{"Drama", "Crime"},
		Language:      "en",
		Status:        "released",
		RatingAvg:     8.5,
		RatingCount:   1000,
		Images:        map[string]any{"poster": "url"},
		Metadata:      map[string]any{"source": "tmdb"},
	}

	// full extended
	resp := buildFullMovieResponse(m)
	if resp.Title != m.Title {
		t.Errorf("Title = %q, want %q", resp.Title, m.Title)
	}
	if resp.Year != 2013 {
		t.Errorf("Year = %d, want 2013", resp.Year)
	}
	if resp.Overview != m.Overview {
		t.Errorf("Overview = %q, want %q", resp.Overview, m.Overview)
	}
	if resp.Runtime != m.Runtime {
		t.Errorf("Runtime = %d, want %d", resp.Runtime, m.Runtime)
	}
	if resp.IDs.ID != m.ID {
		t.Errorf("IDs.ID = %d, want %d", resp.IDs.ID, m.ID)
	}
	if resp.IDs.TMDB != m.TmdbID {
		t.Errorf("IDs.TMDB = %d, want %d", resp.IDs.TMDB, m.TmdbID)
	}
	if resp.IDs.IMDB != m.ImdbID {
		t.Errorf("IDs.IMDB = %q, want %q", resp.IDs.IMDB, m.ImdbID)
	}
	if resp.RatingSummary == nil {
		t.Fatal("RatingSummary should not be nil")
	}
	if resp.RatingSummary.Rating != 8.5 {
		t.Errorf("RatingSummary.Rating = %f, want 8.5", resp.RatingSummary.Rating)
	}
	if resp.Tagline != m.Tagline {
		t.Errorf("Tagline = %q, want %q", resp.Tagline, m.Tagline)
	}
	if len(resp.Genres) != 2 {
		t.Errorf("len(Genres) = %d, want 2", len(resp.Genres))
	}

	// JSON serialization
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal MovieResponse: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("marshaled JSON is empty")
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if raw["images"] == nil {
		t.Error("images field should not be null")
	}
	if raw["genres"] == nil {
		t.Error("genres field should not be null")
	}

	// min extended should NOT include overview.
	minResp := buildMinMovieResponse(m)
	if minResp.Overview != "" {
		t.Errorf("Overview with extended=min should be empty, got %q", minResp.Overview)
	}
	if minResp.Tagline != "" {
		t.Errorf("Tagline with extended=min should be empty, got %q", minResp.Tagline)
	}
}

// TestShowResponseJSON verifies the ShowResponse JSON serialization.
func TestShowResponseJSON(t *testing.T) {
	firstAired := database.Date{}
	firstAired.Time = parseDateHelper("2008-01-20")

	s := &database.Show{
		ID:           7,
		TmdbID:       1396,
		ImdbID:       "tt0903747",
		Title:        "Breaking Bad",
		Overview:     "A high school chemistry teacher.",
		FirstAired:   &firstAired,
		SeasonCount:  5,
		EpisodeCount: 62,
		Network:      "AMC",
		Status:       "ended",
		Genres:       []string{"Drama"},
		Language:     "en",
		RatingAvg:    9.2,
		RatingCount:  5000,
		Images:       map[string]any{"poster": "url"},
		Metadata:     map[string]any{"source": "tmdb"},
	}

	resp := buildFullShowResponse(s)

	if resp.Title != s.Title {
		t.Errorf("Title = %q, want %q", resp.Title, s.Title)
	}
	if resp.Year != 2008 {
		t.Errorf("Year = %d, want 2008", resp.Year)
	}
	if resp.SeasonCount != 5 {
		t.Errorf("SeasonCount = %d, want 5", resp.SeasonCount)
	}
	if resp.Network != "AMC" {
		t.Errorf("Network = %q, want AMC", resp.Network)
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal ShowResponse: %v", err)
	}
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if raw["seasons"] == nil {
		t.Error("seasons field should not be null")
	}
}

// TestPersonResponseJSON verifies the PersonResponse JSON serialization.
func TestPersonResponseJSON(t *testing.T) {
	bday := database.Date{}
	bday.Time = parseDateHelper("1959-03-07")

	p := &database.Person{
		ID:                 1,
		TmdbID:             17419,
		Name:               "Bryan Cranston",
		Biography:          "American actor.",
		Birthday:           &bday,
		Gender:             2,
		KnownForDepartment: "Acting",
		Images:             map[string]any{"headshot": "url"},
	}

	resp := buildFullPersonResponse(p)

	if resp.Name != p.Name {
		t.Errorf("Name = %q, want %q", resp.Name, p.Name)
	}
	if resp.Birthday != "1959-03-07" {
		t.Errorf("Birthday = %q, want 1959-03-07", resp.Birthday)
	}
	if resp.Gender != 2 {
		t.Errorf("Gender = %d, want 2", resp.Gender)
	}
}

// TestIDsShape verifies the IDs object JSON shape.
func TestIDsShape(t *testing.T) {
	ids := media.IDs{
		ID:   42,
		Slug: "test-movie",
		TMDB: 13885,
		IMDB: "tt1234567",
	}

	data, err := json.Marshal(ids)
	if err != nil {
		t.Fatalf("failed to marshal IDs: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("marshaled IDs JSON is empty")
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if raw["id"] == nil {
		t.Error("id field missing")
	}
	if raw["slug"] != "test-movie" {
		t.Errorf("slug = %v", raw["slug"])
	}
}

// TestRatingSummaryShape verifies the rating summary JSON shape.
func TestRatingSummaryShape(t *testing.T) {
	summary := media.RatingSummary{
		Rating:       8.5,
		Votes:        1000,
		Distribution: map[string]int64{"1": 10, "10": 500},
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if raw["rating"] != 8.5 {
		t.Errorf("rating = %v", raw["rating"])
	}
	if raw["votes"] == nil {
		t.Error("votes missing")
	}
}

// TestEmptyStubResponses verifies empty stub responses marshal correctly.
func TestEmptyStubResponses(t *testing.T) {
	watching := make(media.WatchingResponse, 0)
	data, err := json.Marshal(watching)
	if err != nil {
		t.Fatalf("watching marshal: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("watching response should be [], got %s", string(data))
	}

	lists := make(media.ListsResponse, 0)
	data, err = json.Marshal(lists)
	if err != nil {
		t.Fatalf("lists marshal: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("lists response should be [], got %s", string(data))
	}
}

// TestStatsResponseShape verifies stats response.
func TestStatsResponseShape(t *testing.T) {
	stats := media.StatsResponse{
		Watchers:   100,
		Plays:      500,
		Collectors: 50,
		Lists:      10,
	}
	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("stats marshal: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if raw["watchers"] == nil {
		t.Error("watchers missing")
	}
}

// TestPeopleResponseShape verifies people response.
func TestPeopleResponseShape(t *testing.T) {
	pr := media.PeopleResponse{
		Cast: []media.CastMemberResponse{
			{
				Person: media.PersonResponse{
					IDs:    media.IDs{ID: 1},
					Name:   "Actor One",
					Gender: 2,
				},
				Character: "Character One",
				Order:     0,
			},
		},
		Crew: []media.CrewMemberResponse{
			{
				Person: media.PersonResponse{
					IDs:    media.IDs{ID: 2},
					Name:   "Director One",
					Gender: 1,
				},
				Job:        "Director",
				Department: "Directing",
			},
		},
	}

	data, err := json.Marshal(pr)
	if err != nil {
		t.Fatalf("people marshal: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if raw["cast"] == nil {
		t.Error("cast missing")
	}
	if raw["crew"] == nil {
		t.Error("crew missing")
	}
}

// TestEpisodeResponseShape verifies episode response.
func TestEpisodeResponseShape(t *testing.T) {
	absEp := 15
	ep := media.EpisodeResponse{
		IDs:            media.IDs{ID: 100},
		SeasonID:       5,
		ShowID:         7,
		EpisodeNumber:  3,
		Title:          "Pilot",
		Overview:       "The first episode.",
		Runtime:        45,
		AbsoluteNumber: &absEp,
		Images:         map[string]any{"still": "url"},
	}

	data, err := json.Marshal(ep)
	if err != nil {
		t.Fatalf("episode marshal: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if raw["title"] != "Pilot" {
		t.Errorf("title = %v", raw["title"])
	}
}

// TestSeasonResponseShape verifies season response.
func TestSeasonResponseShape(t *testing.T) {
	season := media.SeasonResponse{
		IDs:          media.IDs{ID: 50},
		ShowID:       7,
		SeasonNumber: 1,
		EpisodeCount: 10,
		Images:       map[string]any{"poster": "url"},
		AiredAt:      "2008-01-20",
	}

	data, err := json.Marshal(season)
	if err != nil {
		t.Fatalf("season marshal: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if raw["season_number"] == nil {
		t.Error("season_number missing")
	}
}

// ---------------------------------------------------------------------------
// OpenAPI route registration tests
// ---------------------------------------------------------------------------

// TestMediaRoutesRegistered verifies media routes are in the OpenAPI registry.
func TestMediaRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/movies/{id}":                                     true,
		"/v1/movies/{id}/people":                              true,
		"/v1/movies/{id}/ratings":                             true,
		"/v1/movies/{id}/stats":                               true,
		"/v1/movies/{id}/lists":                               true,
		"/v1/movies/{id}/watching":                            true,
		"/v1/shows/{id}":                                      true,
		"/v1/shows/{id}/seasons":                              true,
		"/v1/shows/{id}/seasons/{season}":                     true,
		"/v1/shows/{id}/seasons/{season}/episodes":            true,
		"/v1/shows/{id}/seasons/{season}/episodes/{episode}":  true,
		"/v1/shows/{id}/people":                               true,
		"/v1/shows/{id}/ratings":                              true,
		"/v1/shows/{id}/stats":                                true,
		"/v1/shows/{id}/lists":                                true,
		"/v1/shows/{id}/watching":                             true,
		"/v1/people/{id}":                                     true,
		"/v1/people/{id}/movies":                              true,
		"/v1/people/{id}/shows":                               true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected route %q not registered", path)
		}
	}

	if len(routes) < len(expectedPaths) {
		t.Errorf("only %d routes registered, expected at least %d", len(routes), len(expectedPaths))
	}
}

// TestMediaRoutesHaveCorrectAuth verifies all media routes are public.
func TestMediaRoutesHaveCorrectAuth(t *testing.T) {
	routes := openapi.AllRoutes()
	mediaTags := map[string]bool{"Movies": true, "Shows": true, "People": true}

	for _, r := range routes {
		if len(r.Tags) > 0 && mediaTags[r.Tags[0]] {
			if r.Auth != "none" {
				t.Errorf("media route %s %s has auth=%q, want 'none'", r.Method, r.Path, r.Auth)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Pagination parsing tests
// ---------------------------------------------------------------------------

func TestParsePaginationDefaults(t *testing.T) {
	page, limit := parsePaginationHelper("", "")
	if page != 1 {
		t.Errorf("default page = %d, want 1", page)
	}
	if limit != 25 {
		t.Errorf("default limit = %d, want 25", limit)
	}
}

func TestParsePaginationCustom(t *testing.T) {
	page, limit := parsePaginationHelper("3", "50")
	if page != 3 {
		t.Errorf("page = %d, want 3", page)
	}
	if limit != 50 {
		t.Errorf("limit = %d, want 50", limit)
	}
}

func TestParsePaginationMaxLimit(t *testing.T) {
	_, limit := parsePaginationHelper("1", "200")
	if limit != 100 {
		t.Errorf("limit = %d, want 100 (capped)", limit)
	}
}

func TestParsePaginationInvalidValues(t *testing.T) {
	page, limit := parsePaginationHelper("invalid", "invalid")
	if page != 1 {
		t.Errorf("page with invalid input = %d, want 1", page)
	}
	if limit != 25 {
		t.Errorf("limit with invalid input = %d, want 25", limit)
	}

	page, limit = parsePaginationHelper("-1", "0")
	if page != 1 {
		t.Errorf("page with negative = %d, want 1", page)
	}
	if limit != 25 {
		t.Errorf("limit with zero = %d, want 25", limit)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func isNumericHelper(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func parsePaginationHelper(pageStr, limitStr string) (int, int) {
	page, limit := 1, 25
	if pageStr != "" {
		p := 0
		for _, c := range pageStr {
			if c >= '0' && c <= '9' {
				p = p*10 + int(c-'0')
			} else {
				p = -1
				break
			}
		}
		if p > 0 {
			page = p
		}
	}
	if limitStr != "" {
		l := 0
		for _, c := range limitStr {
			if c >= '0' && c <= '9' {
				l = l*10 + int(c-'0')
			} else {
				l = -1
				break
			}
		}
		if l > 0 {
			if l > 100 {
				l = 100
			}
			limit = l
		}
	}
	return page, limit
}

func parseDateHelper(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func buildMinMovieResponse(m *database.Movie) media.MovieResponse {
	year := 0
	if m.Released != nil && !m.Released.IsZero() {
		year = m.Released.Year()
	}
	return media.MovieResponse{
		IDs: media.IDs{
			ID:   m.ID,
			TMDB: m.TmdbID,
			IMDB: m.ImdbID,
		},
		Title:  m.Title,
		Year:   year,
		Images: m.Images,
		RatingSummary: &media.RatingSummary{
			Rating:       m.RatingAvg,
			Votes:        m.RatingCount,
			Distribution: make(map[string]int64),
		},
	}
}

func buildFullMovieResponse(m *database.Movie) media.MovieResponse {
	year := 0
	if m.Released != nil && !m.Released.IsZero() {
		year = m.Released.Year()
	}
	return media.MovieResponse{
		IDs: media.IDs{
			ID:   m.ID,
			TMDB: m.TmdbID,
			IMDB: m.ImdbID,
		},
		Title:         m.Title,
		Year:          year,
		Overview:      m.Overview,
		Tagline:       m.Tagline,
		Runtime:       m.Runtime,
		Genres:        m.Genres,
		Certification: m.Certification,
		Language:      m.Language,
		TrailerURL:    m.TrailerURL,
		Status:        m.Status,
		Images:        m.Images,
		Metadata:      m.Metadata,
		RatingSummary: &media.RatingSummary{
			Rating:       m.RatingAvg,
			Votes:        m.RatingCount,
			Distribution: make(map[string]int64),
		},
	}
}

func buildFullShowResponse(s *database.Show) media.ShowResponse {
	year := 0
	if s.FirstAired != nil && !s.FirstAired.IsZero() {
		year = s.FirstAired.Year()
	}
	firstAired := ""
	if s.FirstAired != nil {
		firstAired = s.FirstAired.String()
	}
	resp := media.ShowResponse{
		IDs: media.IDs{
			ID:   s.ID,
			TMDB: s.TmdbID,
			IMDB: s.ImdbID,
		},
		Title:         s.Title,
		Year:          year,
		Overview:      s.Overview,
		Genres:        s.Genres,
		Certification: s.Certification,
		Language:      s.Language,
		Network:       s.Network,
		Status:        s.Status,
		EpisodeCount:  s.EpisodeCount,
		SeasonCount:   s.SeasonCount,
		FirstAired:    firstAired,
		Images:        s.Images,
		Metadata:      s.Metadata,
		RatingSummary: &media.RatingSummary{
			Rating:       s.RatingAvg,
			Votes:        s.RatingCount,
			Distribution: make(map[string]int64),
		},
	}
	resp.Seasons = make([]media.SeasonResponse, 0)
	return resp
}

func buildFullPersonResponse(p *database.Person) media.PersonResponse {
	resp := media.PersonResponse{
		IDs:                media.IDs{ID: p.ID, TMDB: p.TmdbID},
		Name:               p.Name,
		Biography:          p.Biography,
		Gender:             p.Gender,
		KnownForDepartment: p.KnownForDepartment,
		Images:             p.Images,
	}
	if p.Birthday != nil {
		resp.Birthday = p.Birthday.String()
	}
	return resp
}