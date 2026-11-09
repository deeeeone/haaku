package shared

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Movie{}, &database.Show{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// parseRequest runs ParseCatalogueFilter against a query string, with the
// media collections' full capability set, and returns the handler's status.
func parseRequest(t *testing.T, path string) int {
	t.Helper()
	status, _, _ := parseRequestFull(t, path)
	return status
}

// parseRequestFull also returns the body and the ignored-filters header, so a
// test can prove an unstored parameter was ignored rather than applied.
func parseRequestFull(t *testing.T, path string) (int, string, string) {
	t.Helper()
	app := fiber.New()
	app.Get("/x", func(c *fiber.Ctx) error {
		if _, ok := ParseCatalogueFilter(c, FilterGenres, FilterYears, FilterRatings, FilterRuntimes, FilterStatuses, FilterCertifications); !ok {
			return nil
		}
		return c.SendString("ok")
	})
	req, _ := http.NewRequest("GET", path, nil)
	req.Host = "example.com"
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	body := ""
	if res.Body != nil {
		var buf strings.Builder
		if _, err := io.Copy(&buf, res.Body); err != nil {
			t.Fatalf("read body %s: %v", path, err)
		}
		body = buf.String()
	}
	return res.StatusCode, body, res.Header.Get(IgnoredFiltersHeader)
}

// TestGenreMembershipDoesNotFanOut is the count-fan-out guard: a movie carrying
// two of the requested genres must match exactly once, so a paginated count is
// the count of items, not of item-genre pairs.
func TestGenreMembershipDoesNotFanOut(t *testing.T) {
	db := testDB(t)
	if err := db.Create(&database.Movie{ID: 1, Title: "Both", Genres: database.StringSlice{"Action", "Drama"}}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Movie{ID: 2, Title: "Comedy", Genres: database.StringSlice{"Comedy"}}).Error; err != nil {
		t.Fatal(err)
	}

	f := &CatalogueFilter{genres: []string{"action", "drama"}}
	clause, args := f.MovieWhere("m")
	var count int64
	if err := db.Table("movies m").Where(clause, args...).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("genre filter counted %d rows, want 1 (no fan-out)", count)
	}
}

// TestGenreSubstringIsNotAMatch proves the quoted-element pattern: "Action"
// must not match the TV genre "Action & Adventure".
func TestGenreSubstringIsNotAMatch(t *testing.T) {
	db := testDB(t)
	if err := db.Create(&database.Show{ID: 1, Title: "Adventurous", Genres: database.StringSlice{"Action & Adventure"}}).Error; err != nil {
		t.Fatal(err)
	}
	f := &CatalogueFilter{genres: []string{"action"}}
	clause, args := f.ShowWhere("s")
	var count int64
	if err := db.Table("shows s").Where(clause, args...).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("genre action matched %d rows, want 0", count)
	}
}

// TestSelectorsNeverDefaultSilently proves an unknown sort is an error naming
// the accepted values, not a fallback ordering.
func TestSelectorsNeverDefaultSilently(t *testing.T) {
	fields := map[string]OrderField{"rank": {Expr: "rank"}, "title": {Expr: "title"}}
	order, err := SQLOrder("random", "", fields, "asc")
	if err == nil {
		t.Fatalf("unknown sort resolved to %q, want an error", order)
	}
	if !strings.Contains(err.Error(), "rank") || !strings.Contains(err.Error(), "title") {
		t.Fatalf("error %q does not name the accepted sorts", err)
	}

	if _, err := SQLOrder("rank", "sideways", fields, "asc"); err == nil {
		t.Fatal("invalid direction accepted, want an error")
	}

	order, err = SQLOrder("title", "desc", fields, "asc")
	if err != nil || order != "title desc" {
		t.Fatalf("title desc = %q, %v", order, err)
	}

	named, err := NamedOrder("oldest", map[string]string{"oldest": "created_at ASC"})
	if err != nil || named != "created_at ASC" {
		t.Fatalf("NamedOrder = %q, %v", named, err)
	}
	if _, err := NamedOrder("sideways", map[string]string{"oldest": "created_at ASC"}); err == nil {
		t.Fatal("unknown named sort accepted")
	}
}

// TestParseRefusesBadValuesAndIgnoresUnstored covers the rule at the top of
// filters.go: a malformed value of a supported filter is a 400, an unstored but
// documented parameter is ignored (200) and named in the response header.
func TestParseRefusesBadValuesAndIgnoresUnstored(t *testing.T) {
	status, body, header := parseRequestFull(t, "/x?countries=us&genres=drama")
	if status != 200 {
		t.Fatalf("countries (unstored) status = %d, want 200 (%s)", status, body)
	}
	if header != "countries" {
		t.Errorf("%s = %q, want countries", IgnoredFiltersHeader, header)
	}
	if got := parseRequest(t, "/x?years=nope"); got != 400 {
		t.Fatalf("malformed years status = %d, want 400", got)
	}
	if got := parseRequest(t, "/x?genres="); got != 200 {
		t.Fatalf("empty genres status = %d, want 200", got)
	}
	if got := parseRequest(t, "/x?genres=drama&years=2010-2015&ratings=70-100"); got != 200 {
		t.Fatalf("computable filters status = %d, want 200", got)
	}
}

// TestYearSpansRejectUnrepresentableValues proves a year an endpoint cannot
// turn into a date literal is refused at parse time (400), not left to reach
// the database as an out-of-range date and surface as a 500. The upper
// endpoint is checked because yearPart formats hi+1 as the exclusive bound.
func TestYearSpansRejectUnrepresentableValues(t *testing.T) {
	for _, query := range []string{
		"years=99999999",
		"years=2010-99999999",
		"years=0",
		"years=2010.5",
		"years=NaN",
		"years=Inf",
	} {
		if got := parseRequest(t, "/x?"+query); got != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", query, got)
		}
	}
	for _, query := range []string{"years=2010", "years=2010-2015", "years=1900-2015,2020"} {
		if got := parseRequest(t, "/x?"+query); got != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", query, got)
		}
	}
	// Non-finite values are refused for the numeric spans too, before their
	// int conversion in runtimePart.
	for _, query := range []string{"runtimes=NaN", "ratings=Inf"} {
		if got := parseRequest(t, "/x?"+query); got != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", query, got)
		}
	}
}
