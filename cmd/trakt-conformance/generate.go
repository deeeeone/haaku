package main

import (
	"encoding/json"
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

	c.Path = resolvePath(c.Path, fx)
	c.Query = queryDefaults(op, fx)
	if body := op.RequestBodySchema(); body != nil {
		c.Body = sampleBody(body, op.Path, fx)
	}
	c.Variant = defaultVariant(op.Path, c.Auth)
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

// resolvePath substitutes fixture-backed values for every path parameter. The
// four Trakt id flavours (slug / numeric / imdb / tmdb / tvdb) are exercised by
// choosing a value the seeded rows actually carry.
func resolvePath(tmpl string, fx Fixtures) string {
	out := tmpl
	for _, ph := range paramRe.FindAllString(tmpl, -1) {
		name := strings.Trim(ph, "{}")
		out = strings.ReplaceAll(out, ph, paramValue(name, tmpl, fx))
	}
	return out
}

func paramValue(name, path string, fx Fixtures) string {
	// Context: the path segment immediately before the placeholder.
	seg := segmentBefore(path, name)
	switch name {
	case "start_date":
		return time.Now().UTC().Format("2006-01-02")
	case "days", "page", "limit":
		return "10"
	case "target":
		return "all"
	case "media_id", "item_id":
		return "1"
	case "media_type":
		return "movies"
	case "comment_type":
		return "shows"
	case "sort", "sort_by", "sort_how":
		return "newest"
	case "section":
		return "progress_watched"
	case "country":
		return "us"
	case "season", "number", "episode":
		return "1"
	case "lang":
		return "en"
	case "list_id":
		return fmt.Sprintf("%d", fx.ListID)
	case "rating":
		return "9"
	case "id":
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
			return fx.UserID
		case "comments":
			return fmt.Sprintf("%d", fx.CommentID)
		case "lists", "smart-lists":
			return fmt.Sprintf("%d", fx.ListID)
		}
		return "1"
	case "type":
		switch {
		case strings.Contains(path, "/genres/"), strings.Contains(path, "/countries/"),
			strings.Contains(path, "/languages/"), strings.Contains(path, "/certifications/"):
			return "movies"
		case strings.Contains(path, "/likes/"):
			return "comments"
		case strings.Contains(path, "/notes/"):
			return "movies"
		case strings.Contains(path, "/collection/"), strings.Contains(path, "/history/"):
			return "movies"
		}
		return "movies"
	}
	return "1"
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
			q.Set(p.Name, "breaking")
		case "response_type":
			q.Set("response_type", "code")
		case "client_id":
			q.Set("client_id", "conformance")
		case "redirect_uri":
			q.Set("redirect_uri", "https://example.test/callback")
		case "state":
			q.Set("state", "conformance")
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

func defaultVariant(path, auth string) string {
	if auth == "required" {
		return "authenticated"
	}
	return "public"
}

// sampleBody synthesises a minimal body that conforms to the contract's request
// schema, using the seeded fixture ids so the items resolve to real rows.
func sampleBody(schema map[string]any, opPath string, fx Fixtures) []byte {
	v := sampleValue(schema, opPath, fx, "")
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func sampleValue(schema map[string]any, opPath string, fx Fixtures, prop string) any {
	if schema == nil {
		return nil
	}
	if all, ok := schema["allOf"].([]any); ok {
		merged := map[string]any{}
		for _, sub := range all {
			if m := asMap(sub); m != nil {
				for k, v := range m {
					merged[k] = v
				}
			}
		}
		if len(merged) > 0 {
			return sampleValue(merged, opPath, fx, prop)
		}
	}
	// The contract's request bodies express "one of movie/episode/show" as
	// oneOf. Send the first alternative so the body is a valid instance.
	for _, key := range []string{"oneOf", "anyOf"} {
		if subs, ok := schema[key].([]any); ok && len(subs) > 0 {
			return sampleValue(asMap(subs[0]), opPath, fx, prop)
		}
	}
	// A string with a documented enum must carry one of its own values.
	if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
		for _, e := range enum {
			if e != nil {
				return e
			}
		}
	}
	types := schemaTypesOf(schema)
	// Prefer a concrete type over null.
	use := ""
	for _, t := range types {
		if t != "null" {
			use = t
			break
		}
	}
	switch use {
	case "object":
		out := map[string]any{}
		props := asMap(schema["properties"])
		for name, ps := range props {
			out[name] = sampleValue(asMap(ps), opPath, fx, name)
		}
		if len(out) == 0 {
			return map[string]any{}
		}
		return out
	case "array":
		items := asMap(schema["items"])
		if items == nil {
			return []any{}
		}
		return []any{sampleValue(items, opPath, fx, prop)}
	case "integer", "number":
		return sampleInt(prop, fx)
	case "boolean":
		return true
	case "string":
		return sampleString(prop, opPath)
	}
	if props := asMap(schema["properties"]); len(props) > 0 {
		out := map[string]any{}
		for name, ps := range props {
			out[name] = sampleValue(asMap(ps), opPath, fx, name)
		}
		return out
	}
	return nil
}

// sampleInt fills an integer leaf with the seeded id the field names, so a
// request that addresses media by tmdb/trakt id resolves to a real row.
func sampleInt(prop string, fx Fixtures) int {
	switch prop {
	case "tmdb":
		return fx.MovieTMDB
	case "tvdb":
		return 1022
	case "trakt", "id":
		return 1
	case "rating":
		return 9
	case "progress":
		return 50
	}
	return 1
}

// sampleString fills a string leaf with a value the API will accept: an id set
// resolves to the seeded tmdb id, and a date field gets a real timestamp.
func sampleString(prop, opPath string) string {
	switch prop {
	case "ids", "tmdb":
		return "550"
	case "imdb":
		return "tt0137523"
	case "tvdb":
		return "1022"
	case "slug":
		return "fight-club"
	case "watched_at", "collected_at", "rated_at", "listed_at", "favorited_at", "paused_at":
		return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	case "comment", "body", "note", "notes", "message", "name", "title":
		return "conformance"
	case "email":
		return "conformance@example.test"
	case "username":
		return "conformance"
	case "password":
		return "conformance-password-123"
	case "type":
		return "movies"
	}
	if strings.HasSuffix(prop, "_at") {
		return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return "conformance"
}

func schemaTypesOf(schema map[string]any) []string {
	switch t := schema["type"].(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, v := range t {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
