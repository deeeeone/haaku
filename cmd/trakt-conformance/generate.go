package main

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/haaku-tv/haaku/internal/contract"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/parity"
)

var paramRe = regexp.MustCompile(`\{[^}]+\}`)

// normPath replaces every path parameter with a single placeholder, so a row's
// instance path (/calendars/all/movies/{start_date}/{days}) and the contract's
// template (/calendars/{target}/movies/{start_date}/{days}) compare equal.
func normPath(p string) string { return paramRe.ReplaceAllString(p, "{}") }

// routeIndex maps "METHOD /path/template" to the registered route, so the
// harness reads our auth posture from the same registry openapi.yaml is
// generated from — never from a hand-kept list.
func routeIndex() map[string]openapi.RouteDef {
	idx := map[string]openapi.RouteDef{}
	for _, r := range openapi.AllRoutes() {
		idx[strings.ToUpper(r.Method)+" "+r.Path] = r
	}
	return idx
}

// Case is one conformance case: a request the contract says is valid, aimed at
// the route we registered for that operation, with the response schema to
// validate against.
type Case struct {
	Op           string // "GET /movies/{id}"
	Method       string // our method
	Path         string // resolved request path
	Query        url.Values
	Body         []byte
	Auth         string // our route's declared auth: none|optional|required
	SuccessCodes []string
	Schemas      map[string]map[string]any // status code -> response schema
	Variant      string                    // which id flavour the case exercises
	NoCaseReason string                    // non-empty: no case could be built
}

// scope holds the manifest's graded rows resolved onto contract operations.
type scope struct {
	// Graded maps a contract operation key to the manifest row that grades it.
	Graded map[string]parity.Row
	// KeyOf maps a normalised path template to contract operations, for rows
	// that name an instance path rather than the template.
	byNorm map[string][]string
}

// buildScope resolves the manifest's implemented/divergent rows onto the
// contract's operations.
func buildScope(m *parity.Manifest, doc *contract.Document) scope {
	sc := scope{Graded: map[string]parity.Row{}, byNorm: map[string][]string{}}
	for _, op := range doc.Operations() {
		key := op.Method + " " + op.Path
		sc.byNorm[normPath(op.Path)] = append(sc.byNorm[normPath(op.Path)], key)
	}
	for _, s := range m.Sections {
		for _, row := range s.Rows {
			if row.Status != parity.StatusImplemented && row.Status != parity.StatusDivergent {
				continue
			}
			if strings.TrimSpace(row.Trakt) == "" {
				continue
			}
			key := resolveOpKey(row.Trakt, doc)
			if key == "" {
				continue
			}
			// First row wins; a later duplicate instance row must not clobber a
			// template row with a more specific `ours`.
			if _, seen := sc.Graded[key]; !seen {
				sc.Graded[key] = row
			}
		}
	}
	return sc
}

// resolveOpKey maps a manifest Trakt path onto the contract's exact key, or a
// template match when the row names an instance path.
func resolveOpKey(trakt string, doc *contract.Document) string {
	parts := strings.SplitN(strings.TrimSpace(trakt), " ", 2)
	if len(parts) != 2 {
		return ""
	}
	method, path := strings.ToUpper(parts[0]), parts[1]
	if _, ok := doc.Operation(method, path); ok {
		return method + " " + path
	}
	m := normPath(path)
	for _, op := range doc.Operations() {
		if op.Method == method && normPath(op.Path) == m {
			return op.Method + " " + op.Path
		}
	}
	return ""
}

// generateCase builds the request and expectations for a graded operation.
func generateCase(doc *contract.Document, op contract.Operation, row parity.Row, routes map[string]openapi.RouteDef, fx Fixtures) Case {
	c := Case{Op: op.Method + " " + op.Path}

	ours := strings.SplitN(strings.TrimSpace(row.Ours), " ", 2)
	if len(ours) != 2 {
		c.NoCaseReason = "manifest row names no route we serve"
		return c
	}
	c.Method, c.Path = strings.ToUpper(ours[0]), ours[1]
	if _, ok := routes[c.Method+" "+c.Path]; !ok {
		c.NoCaseReason = fmt.Sprintf("route %s %s is not in the registry", c.Method, c.Path)
		return c
	}
	route := routes[c.Method+" "+c.Path]
	c.Auth = route.Auth

	c.SuccessCodes, c.Schemas = successCodes(op)
	if len(c.SuccessCodes) == 0 {
		c.NoCaseReason = "contract declares no 2xx response"
		return c
	}

	c.Path = resolvePath(op, c.Method, c.Path, fx)
	c.Query = queryDefaults(op, fx)
	if body := op.RequestBodySchema(); body != nil {
		c.Body = sampleBody(body, op.Path, fx)
	}
	c.Variant = defaultVariant(op.Path, c.Auth, fx)
	return c
}

// successCodes returns the contract's 2xx codes for an operation and their
// response schemas.
func successCodes(op contract.Operation) ([]string, map[string]map[string]any) {
	var codes []string
	schemas := map[string]map[string]any{}
	for _, code := range op.ResponseCodes() {
		if !strings.HasPrefix(code, "2") {
			continue
		}
		codes = append(codes, code)
		s, _ := op.ResponseSchema(code)
		schemas[code] = s
	}
	sort.Strings(codes)
	return codes, schemas
}

// resolvePath substitutes fixture-backed values for every path parameter of the
// route we serve, using the contract's own declaration for that parameter where
// it has one (an enum, an integer) and the fixture catalogue otherwise.
//
// The values are read from the fixture set, never pinned: changing a seeded id
// changes the request (TestCasesFollowFixtureIDs proves it).
func resolvePath(op contract.Operation, method, tmpl string, fx Fixtures) string {
	out := tmpl
	declared := map[string]contract.Parameter{}
	for _, p := range op.Parameters() {
		if p.In == "path" {
			declared[p.Name] = p
		}
	}
	for _, ph := range paramRe.FindAllString(tmpl, -1) {
		name := strings.Trim(ph, "{}")
		out = strings.ReplaceAll(out, ph, paramValue(name, method, tmpl, declared[name], fx))
	}
	return out
}

// paramValue returns one path segment.
//
// Precedence: an enum the contract publishes (that is the only value the
// contract itself admits), then a fixture id for the parameter's meaning, then
// a documented non-empty default. A literal "1" is never used for media: local
// sequences are separate per table, so 1 is a movie and a show and a person at
// once, and a handler resolving it type-agnostically measures the collision.
func paramValue(name, method, path string, p contract.Parameter, fx Fixtures) string {
	if v := firstEnumValue(p.Schema); v != "" {
		// id_type is not enum-constrained but the id beside it must match the
		// flavour, so it is handled below rather than by this shortcut.
		if name != "id" && name != "item_id" && name != "list_item_id" {
			return v
		}
	}
	switch name {
	case "start_date":
		return time.Now().UTC().Format("2006-01-02")
	case "days", "page", "limit", "month":
		return "10"
	case "year", "year_end":
		return "2024"
	case "target":
		return "all"
	case "media_id":
		if strings.Contains(path, "episodes") {
			return fmt.Sprintf("%d", fx.EpisodeID)
		}
		return fmt.Sprintf("%d", fx.MovieID)
	case "item_id":
		switch {
		case strings.Contains(path, "/episodes"):
			return fmt.Sprintf("%d", fx.EpisodeID)
		case strings.Contains(path, "/shows"):
			return fmt.Sprintf("%d", fx.ShowID)
		case strings.Contains(path, "/seasons"):
			return fmt.Sprintf("%d", fx.SeasonID)
		default:
			return fmt.Sprintf("%d", fx.MovieID)
		}
	case "media_type":
		if strings.Contains(path, "episodes") {
			return "episodes"
		}
		return "movies"
	case "comment_type":
		if v := firstEnumValue(p.Schema); v != "" {
			return v
		}
		return "all"
	case "section":
		return "calendar"
	case "country", "countryCode":
		return "us"
	case "lang":
		return "en"
	case "season", "number":
		// A report is unique per (reporter, media). The flat
		// /seasons/{id}/report case already reports season one, so a
		// contract-path report of the same season would legitimately answer
		// 409; point those cases at the second season's premiere instead.
		if method == "POST" && strings.HasSuffix(path, "/report") {
			return fmt.Sprintf("%d", fx.Season2Num)
		}
		return fmt.Sprintf("%d", fx.SeasonNum)
	case "episode":
		if method == "POST" && strings.HasSuffix(path, "/report") {
			return fmt.Sprintf("%d", fx.Episode3Num)
		}
		return fmt.Sprintf("%d", fx.EpisodeNum)
	case "list_id":
		if strings.Contains(path, "/smart-lists") {
			return fmt.Sprintf("%d", fx.SmartListID)
		}
		return fmt.Sprintf("%d", fx.ListID)
	case "list_item_id":
		return fmt.Sprintf("%d", fx.ListItemID)
	case "rating":
		return "9"
	case "sort":
		if v := firstEnumValue(p.Schema); v != "" {
			return v
		}
		if strings.Contains(path, "/comments/") {
			return "newest"
		}
		return "newest"
	case "sort_by":
		return "rank"
	case "sort_how":
		return "asc"
	case "id_type":
		return "tmdb"
	case "type":
		switch {
		case strings.Contains(path, "/search/"):
			return "movie"
		case strings.Contains(path, "/comments/"):
			return "movies"
		}
		return "movies"
	case "reaction_type":
		return "like"
	case "all_data":
		return "true"
	case "server_id", "service_id":
		return "1"
	case "id":
		return idValue(method, path, fx)
	}
	return "conformance"
}

// idValue picks the id flavour the route's leading segment names.
func idValue(method, path string, fx Fixtures) string {
	seg := segmentBefore(path, "id")
	switch seg {
	case "movies":
		return fmt.Sprintf("%d", fx.MovieID)
	case "shows":
		return fmt.Sprintf("%d", fx.ShowID)
	case "seasons":
		return fmt.Sprintf("%d", fx.SeasonID)
	case "episodes":
		return fmt.Sprintf("%d", fx.EpisodeID)
	case "people":
		return fmt.Sprintf("%d", fx.PersonID)
	case "users":
		// Follow/block cannot target the caller, and the destructive cases use
		// their own accounts so the seeded read graph survives them.
		switch {
		case strings.HasSuffix(path, "/follow"):
			return fx.FollowUserID
		case strings.HasSuffix(path, "/block"):
			return fx.BlockUserID
		}
		return fx.UserID
	case "comments":
		// The reply/like assertions need a comment with rows; the plain comment
		// GET keeps the reply-free row so DELETE is not a 409, and DELETE uses a
		// third row so it cannot consume the GET fixture.
		if strings.Contains(path, "/replies") || strings.Contains(path, "/likes") {
			return fmt.Sprintf("%d", fx.ThreadCommentID)
		}
		if method == "DELETE" {
			return fmt.Sprintf("%d", fx.DeleteCommentID)
		}
		return fmt.Sprintf("%d", fx.CommentID)
	case "notes":
		if method == "DELETE" {
			return fmt.Sprintf("%d", fx.DeleteNoteID)
		}
		return fmt.Sprintf("%d", fx.NoteID)
	case "lists":
		return fmt.Sprintf("%d", fx.ListID)
	case "smart-lists":
		return fmt.Sprintf("%d", fx.SmartListID)
	case "{id_type}":
		return externalIDFor(path, fx)
	}
	// /search/{id_type}/{id}: the preceding segment decides the flavour.
	if strings.Contains(path, "/search/") {
		return externalIDFor(path, fx)
	}
	return fmt.Sprintf("%d", fx.MovieID)
}

// externalIDFor returns the id in the flavour named by the /search/{id_type}
// segment immediately before it.
func externalIDFor(path string, fx Fixtures) string {
	switch {
	case strings.Contains(path, "/search/tmdb/"):
		return fmt.Sprintf("%d", fx.MovieTMDB)
	case strings.Contains(path, "/search/imdb/"):
		return fx.MovieIMDB
	case strings.Contains(path, "/search/tvdb/"):
		return fmt.Sprintf("%d", fx.MovieTVDB)
	case strings.Contains(path, "/search/trakt/"):
		return fmt.Sprintf("%d", fx.MovieID)
	}
	return fmt.Sprintf("%d", fx.MovieTMDB)
}

// segmentBefore returns the path segment that precedes a "{name}" placeholder.
func segmentBefore(path, name string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if s == "{"+name+"}" && i > 0 {
			return segs[i-1]
		}
	}
	return ""
}

// queryDefaults builds the query string the contract advertises: the shared
// extended/page/limit pair plus any operation-specific filter with a documented
// enum value.
func queryDefaults(op contract.Operation, fx Fixtures) url.Values {
	q := url.Values{}
	for _, p := range op.Parameters() {
		if p.In != "query" {
			continue
		}
		switch p.Name {
		case "extended":
			q.Set("extended", "full")
		case "page":
			q.Set("page", "1")
		case "limit":
			q.Set("limit", "10")
		case "id_type":
			q.Set("id_type", "tmdb")
		case "query", "q":
			q.Set(p.Name, "fight")
		case "response_type":
			q.Set("response_type", "code")
		case "client_id":
			q.Set("client_id", fx.AppClientID)
		case "redirect_uri":
			q.Set("redirect_uri", fx.RedirectURI)
		case "state":
			q.Set("state", "conformance")
		case "code_challenge":
			q.Set("code_challenge", fx.CodeChallenge)
		case "code_challenge_method":
			q.Set("code_challenge_method", "S256")
		default:
			if v := firstEnumValue(p.Schema); v != "" {
				q.Set(p.Name, v)
			}
		}
	}
	return q
}

func firstEnumValue(schema map[string]any) string {
	if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
		if s, ok := enum[0].(string); ok {
			return s
		}
	}
	return ""
}

func defaultVariant(path, auth string, fx Fixtures) string {
	if auth == "required" {
		return "authenticated"
	}
	return "public"
}
