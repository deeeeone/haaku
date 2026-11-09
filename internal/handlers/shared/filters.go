package shared

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Catalogue filters are the Trakt query parameters that narrow a catalogue read
// by properties of the media item. This file is the single applier: parse the
// declared parameters, reject the ones this store cannot honour with a 400 that
// names what is served, and turn the rest into SQL predicates.
//
// Every predicate is an EXISTS over the media table (or an expression on the
// row's own columns). None of them adds a JOIN to the outer query, so a media
// item with three matching genres cannot multiply the result rows and the
// pagination count is the count of distinct items. Genres are a JSON array
// column, so membership is a quoted-element LIKE rather than a join onto a
// genre table (there is no per-item join table to fan out on in the first
// place).

// The catalogue filter parameters the contract declares. Computable ones are
// parsed; the rest are refused rather than silently accepted.
const (
	FilterGenres         = "genres"
	FilterYears          = "years"
	FilterRatings        = "ratings"
	FilterRuntimes       = "runtimes"
	FilterStatuses       = "statuses"
	FilterCertifications = "certifications"
)

var catalogueParams = []string{
	FilterGenres, FilterYears, FilterRatings, FilterRuntimes,
	FilterStatuses, FilterCertifications,
}

// uncomputableParams are declared by the contract but have no stored column we
// could narrow by. Naming them explicitly means a client gets a 400 that says
// so, instead of a 200 containing everything it asked to exclude.
var uncomputableParams = []string{
	"watchnow", "subgenres", "countries",
	"parental_nudity", "parental_violence", "parental_profanity",
	"parental_alcohol", "parental_frightening", "parental_include_unrated",
}

// CatalogueFilter is the parsed, validated form of a request's catalogue
// filters. A nil *CatalogueFilter means "no filters supplied".
type CatalogueFilter struct {
	genres         []string
	years          []span
	ratings        []span
	runtimes       []span
	statuses       []string
	certifications []string
}

// span is an inclusive numeric range; lo == hi for a single value.
type span struct{ lo, hi float64 }

// HasAny reports whether any filter was supplied.
func (f *CatalogueFilter) HasAny() bool {
	return f != nil && (len(f.genres) > 0 || len(f.years) > 0 || len(f.ratings) > 0 ||
		len(f.runtimes) > 0 || len(f.statuses) > 0 || len(f.certifications) > 0)
}

// ParseCatalogueFilter reads the contract's catalogue parameters. allowed names
// the filters this particular collection can evaluate; a declared filter
// outside that set, or one the store has no column for at all, writes a 400
// naming what is served and returns ok=false so the caller stops with
// `if !ok { return nil }` instead of running an unfiltered query.
func ParseCatalogueFilter(c *fiber.Ctx, allowed ...string) (*CatalogueFilter, bool) {
	allowedSet := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allowedSet[a] = true
	}

	for _, name := range uncomputableParams {
		if strings.TrimSpace(c.Query(name)) != "" {
			reject(c, fmt.Sprintf(
				"filter %s is not supported: this store has no column to narrow by; supported catalogue filters: %s",
				name, strings.Join(sortedStrings(allowed), ", ")))
			return nil, false
		}
	}

	f := &CatalogueFilter{}
	for _, name := range catalogueParams {
		raw := strings.TrimSpace(c.Query(name))
		if raw == "" {
			continue
		}
		if !allowedSet[name] {
			reject(c, fmt.Sprintf(
				"filter %s is not supported on this collection; supported catalogue filters: %s",
				name, strings.Join(sortedStrings(allowed), ", ")))
			return nil, false
		}
		switch name {
		case FilterGenres:
			f.genres = splitList(raw)
		case FilterStatuses:
			f.statuses = lowerAll(splitList(raw))
		case FilterCertifications:
			f.certifications = splitList(raw)
		case FilterYears, FilterRatings, FilterRuntimes:
			spans, err := parseSpans(raw)
			if err != nil {
				reject(c, fmt.Sprintf("invalid %s filter: %v", name, err))
				return nil, false
			}
			switch name {
			case FilterYears:
				f.years = spans
			case FilterRatings:
				f.ratings = spans
			default:
				f.runtimes = spans
			}
		}
	}
	if !f.HasAny() {
		return nil, true
	}
	return f, true
}

// MovieWhere returns a parenthesised SQL predicate over a movies alias.
func (f *CatalogueFilter) MovieWhere(alias string) (string, []any) {
	if !f.HasAny() {
		return "", nil
	}
	p := &predicate{}
	f.genrePart(p, alias+".genres")
	f.yearPart(p, alias+".released")
	f.ratingPart(p, alias+".rating_avg")
	f.runtimePart(p, alias+".runtime")
	f.statusPart(p, alias+".status")
	f.certPart(p, alias+".certification")
	return p.where()
}

// ShowWhere returns a parenthesised SQL predicate over a shows alias. Shows
// carry no runtime column, so callers must not allow runtimes for a show-scoped
// collection; ParseCatalogueFilter enforces that.
func (f *CatalogueFilter) ShowWhere(alias string) (string, []any) {
	if !f.HasAny() {
		return "", nil
	}
	p := &predicate{}
	f.genrePart(p, alias+".genres")
	f.yearPart(p, alias+".first_aired")
	f.ratingPart(p, alias+".rating_avg")
	f.statusPart(p, alias+".status")
	f.certPart(p, alias+".certification")
	return p.where()
}

// EpisodeWhere returns a parenthesised SQL predicate over an episodes alias.
// Runtime and air year come from the episode; genre, rating, status and
// certification come from its parent show through an EXISTS — no join.
func (f *CatalogueFilter) EpisodeWhere(alias string) (string, []any) {
	if !f.HasAny() {
		return "", nil
	}
	p := &predicate{}
	f.runtimePart(p, alias+".runtime")
	f.yearPart(p, alias+".aired_at")

	sp := &predicate{}
	f.genrePart(sp, "fs.genres")
	f.ratingPart(sp, "fs.rating_avg")
	f.statusPart(sp, "fs.status")
	f.certPart(sp, "fs.certification")
	if clause, args := sp.where(); clause != "" {
		p.add("EXISTS (SELECT 1 FROM shows fs WHERE fs.id = "+alias+".show_id AND "+clause+")", args...)
	}
	return p.where()
}

// HistoryMediaWhere narrows a user_history query: movie rows are tested against
// movies, episode rows against their episode (which in turn tests the parent
// show). Both branches are EXISTS, so each history row is kept or dropped
// exactly once.
func (f *CatalogueFilter) HistoryMediaWhere(db *gorm.DB) (string, []any) {
	if !f.HasAny() {
		return "", nil
	}
	movie, movieArgs := f.MovieWhere("hm")
	episode, episodeArgs := f.EpisodeWhere("he")

	var parts []string
	var args []any
	if movie != "" {
		parts = append(parts, "(user_history.media_type = 'movie' AND EXISTS "+
			"(SELECT 1 FROM movies hm WHERE hm.id = user_history.media_id AND "+movie+"))")
		args = append(args, movieArgs...)
	}
	if episode != "" {
		parts = append(parts, "(user_history.media_type = 'episode' AND EXISTS "+
			"(SELECT 1 FROM episodes he WHERE he.id = COALESCE(user_history.episode_id, user_history.media_id) AND "+episode+"))")
		args = append(args, episodeArgs...)
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// WatchlistWhere narrows a user_watchlist query for the kinds it serves.
func (f *CatalogueFilter) WatchlistWhere(db *gorm.DB, movie, show bool) (string, []any) {
	if !f.HasAny() {
		return "", nil
	}
	var parts []string
	var args []any
	if movie {
		if clause, a := f.MovieWhere("wm"); clause != "" {
			parts = append(parts, "(user_watchlist.media_type = 'movie' AND EXISTS "+
				"(SELECT 1 FROM movies wm WHERE wm.id = user_watchlist.media_id AND "+clause+"))")
			args = append(args, a...)
		}
	}
	if show {
		if clause, a := f.ShowWhere("ws"); clause != "" {
			parts = append(parts, "(user_watchlist.media_type = 'show' AND EXISTS "+
				"(SELECT 1 FROM shows ws WHERE ws.id = user_watchlist.media_id AND "+clause+"))")
			args = append(args, a...)
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// WatchlistVisibility applies the contract's hide/ignore_* narrowing to a
// user_watchlist query. hide is a closed enum; a value the store cannot evaluate
// is a 400 naming the accepted values rather than a silently ignored filter.
func WatchlistVisibility(c *fiber.Ctx) (string, []any, bool) {
	var parts []string
	var args []any

	hideRaw := strings.TrimSpace(c.Query("hide"))
	for _, hide := range splitList(hideRaw) {
		switch hide {
		case "", "listed":
			// Every row of a watchlist read is, by definition, listed.
		case "unwatchlisted":
			parts = append(parts, "1 = 0")
		case "notes":
			parts = append(parts, "user_watchlist.notes IS NOT NULL")
		case "nonotes":
			parts = append(parts, "user_watchlist.notes IS NULL")
		case "unwatched":
			parts = append(parts, "NOT "+watchlistRelation("user_history"))
		case "uncollected":
			parts = append(parts, "NOT "+watchlistRelation("user_collection"))
		case "unrated":
			parts = append(parts, "NOT "+watchlistRelation("user_ratings"))
		case "collected":
			parts = append(parts, watchlistRelation("user_collection"))
		case "rated":
			parts = append(parts, watchlistRelation("user_ratings"))
		default:
			reject(c, "hide must be one of: collected, listed, nonotes, notes, rated, uncollected, unrated, unwatched, unwatchlisted")
			return "", nil, false
		}
	}

	for _, flag := range []struct {
		param string
		table string
	}{
		{"ignore_watched", "user_history"},
		{"ignore_collected", "user_collection"},
		{"ignore_watchlisted", "user_watchlist"},
	} {
		if !QueryBool(c, flag.param) {
			continue
		}
		parts = append(parts, "NOT "+watchlistRelation(flag.table))
	}
	if len(parts) == 0 {
		return "", nil, true
	}
	return "(" + strings.Join(parts, " AND ") + ")", args, true
}

// watchlistRelation is the EXISTS predicate for a user_watchlist row having a
// matching row in one of the per-user relation tables.
func watchlistRelation(table string) string {
	return "(EXISTS (SELECT 1 FROM " + table + " r WHERE r.user_id = user_watchlist.user_id" +
		" AND r.media_type = user_watchlist.media_type AND r.media_id = user_watchlist.media_id))"
}

// QueryBool reads a contract boolean query flag (Trakt spells it true/false).
func QueryBool(c *fiber.Ctx, name string) bool {
	v := strings.ToLower(strings.TrimSpace(c.Query(name)))
	return v == "true" || v == "1"
}

// ---------------------------------------------------------------------------
// predicate construction
// ---------------------------------------------------------------------------

type predicate struct {
	parts []string
	args  []any
}

func (p *predicate) add(clause string, args ...any) {
	p.parts = append(p.parts, clause)
	p.args = append(p.args, args...)
}

func (p *predicate) where() (string, []any) {
	if len(p.parts) == 0 {
		return "", nil
	}
	return "(" + strings.Join(p.parts, " AND ") + ")", p.args
}

// genrePart matches a genre element inside the JSON array column. The quotes
// are part of the pattern so "action" cannot match "Action & Adventure"; the
// column is not joined, so several matching genres still yield one row.
//
// Stored genres are TMDB display names ("Science Fiction") while the filter's
// vocabulary is Trakt's lowercase slugs ("science-fiction"), so the comparison
// lowercases both sides and also tries the slug with its hyphens as spaces.
func (f *CatalogueFilter) genrePart(p *predicate, column string) {
	if len(f.genres) == 0 {
		return
	}
	or := make([]string, 0, len(f.genres)*2)
	args := make([]any, 0, len(f.genres)*2)
	for _, g := range f.genres {
		seen := map[string]bool{}
		for _, candidate := range []string{g, strings.ReplaceAll(g, "-", " ")} {
			candidate = strings.ToLower(candidate)
			if seen[candidate] {
				continue
			}
			seen[candidate] = true
			or = append(or, "LOWER(CAST("+column+" AS TEXT)) LIKE ?")
			args = append(args, `%"`+candidate+`"%`)
		}
	}
	p.add("("+strings.Join(or, " OR ")+")", args...)
}

// yearPart turns calendar years into a date range on the row's release date, so
// the comparison is a plain DATE comparison on both PostgreSQL and sqlite
// (no EXTRACT/strftime dialect split).
func (f *CatalogueFilter) yearPart(p *predicate, column string) {
	if len(f.years) == 0 {
		return
	}
	or := make([]string, 0, len(f.years))
	var args []any
	for _, s := range f.years {
		lo := int(s.lo)
		hi := int(s.hi)
		or = append(or, "("+column+" >= ? AND "+column+" < ?)")
		args = append(args, fmt.Sprintf("%04d-01-01", lo), fmt.Sprintf("%04d-01-01", hi+1))
	}
	p.add("("+strings.Join(or, " OR ")+")", args...)
}

// ratingPart compares the stored 0–10 average against Trakt's 0–100 filter.
func (f *CatalogueFilter) ratingPart(p *predicate, column string) {
	if len(f.ratings) == 0 {
		return
	}
	or := make([]string, 0, len(f.ratings))
	var args []any
	for _, s := range f.ratings {
		or = append(or, "("+column+" * 10 >= ? AND "+column+" * 10 <= ?)")
		args = append(args, s.lo, s.hi)
	}
	p.add("("+strings.Join(or, " OR ")+")", args...)
}

func (f *CatalogueFilter) runtimePart(p *predicate, column string) {
	if len(f.runtimes) == 0 {
		return
	}
	or := make([]string, 0, len(f.runtimes))
	var args []any
	for _, s := range f.runtimes {
		or = append(or, "("+column+" >= ? AND "+column+" <= ?)")
		args = append(args, int(s.lo), int(s.hi))
	}
	p.add("("+strings.Join(or, " OR ")+")", args...)
}

func (f *CatalogueFilter) statusPart(p *predicate, column string) {
	if len(f.statuses) == 0 {
		return
	}
	p.add("LOWER("+column+") IN ?", f.statuses)
}

func (f *CatalogueFilter) certPart(p *predicate, column string) {
	if len(f.certifications) == 0 {
		return
	}
	p.add(column+" IN ?", f.certifications)
}

// ---------------------------------------------------------------------------
// parsing helpers
// ---------------------------------------------------------------------------

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func lowerAll(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = strings.ToLower(v)
	}
	return out
}

// parseSpans reads Trakt's comma-separated value or range syntax ("2014-2016",
// "30-90,120"). A malformed value is an error, not an empty filter.
func parseSpans(raw string) ([]span, error) {
	var out []span
	for _, part := range splitList(raw) {
		lo, hi, ok := strings.Cut(part, "-")
		if !ok {
			v, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
			if err != nil {
				return nil, fmt.Errorf("%q is not a number or range", part)
			}
			out = append(out, span{v, v})
			continue
		}
		lv, err := strconv.ParseFloat(strings.TrimSpace(lo), 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number or range", part)
		}
		hv, err := strconv.ParseFloat(strings.TrimSpace(hi), 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number or range", part)
		}
		if hv < lv {
			return nil, fmt.Errorf("%q has its upper bound below its lower bound", part)
		}
		out = append(out, span{lv, hv})
	}
	return out, nil
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// reject writes the 400 envelope. The caller returns ok=false so the handler
// stops carrying on with an unfiltered query.
func reject(c *fiber.Ctx, message string) {
	_ = response.BadRequest(c, message)
}
