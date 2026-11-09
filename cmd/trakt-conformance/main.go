// Command trakt-conformance checks that every operation our manifest grades
// implemented or divergent actually conforms to the upstream Trakt contract — in
// both directions — against a live server on a seeded PostgreSQL.
//
// It is the gate issue #102 asks for: a row may only be graded implemented when
// a passing conformance case exists for it, and a handler answering 200 with
// "close enough" JSON is made measurable rather than asserted.
//
// Request side: for each graded operation it sends a request built from the
// contract's own path params, query params (extended/page/limit and every
// operation-specific filter), request body and headers.
//
// Response side: it validates the served payload against the contract's
// response schema at every level (types, required sets, nullability, the ids
// object, date-time formats, array vs object envelopes), the X-Pagination-*
// headers the contract carries, and the error envelope.
//
// Usage:
//
//	go run ./cmd/trakt-conformance -seed      # seed fixtures and exit
//	go run ./cmd/trakt-conformance            # run the cases against -base-url
//
// It prints a per-operation PASS/FAIL table and a "N of 336 conformant"
// summary, and exits non-zero when any case fails.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"

	"os"
	"sort"
	"strings"
	"time"

	"github.com/haaku-tv/haaku/internal/contract"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/httpapi"

	"github.com/haaku-tv/haaku/internal/parity"

	"github.com/gofiber/fiber/v2"
)

func main() {
	os.Exit(run())
}

type config struct {
	baseURL   string
	manifest  string
	indexPath string
	specPath  string
	fixtures  string
	seed      bool
	strict    bool
	timeout   time.Duration
}

func run() int {
	var cfg config
	flag.StringVar(&cfg.baseURL, "base-url", "http://127.0.0.1:18090", "base URL of the live server")
	flag.StringVar(&cfg.manifest, "manifest", "docs/parity/parity.yaml", "path to the parity manifest")
	flag.StringVar(&cfg.indexPath, "index", "docs/parity/schema-index.json", "path to the derived schema index")
	flag.StringVar(&cfg.specPath, "spec", "docs/parity/upstream/trakt-openapi.json", "path to the vendored Trakt contract")
	flag.StringVar(&cfg.fixtures, "fixtures", ".conformance-fixtures.json", "path to the seeded fixture ids")
	flag.BoolVar(&cfg.seed, "seed", false, "seed the fixture rows into DATABASE_URL and exit")
	flag.BoolVar(&cfg.strict, "strict", false, "fail when a graded operation has no case yet, not just when a case fails")
	flag.DurationVar(&cfg.timeout, "timeout", 20*time.Second, "per-request timeout")
	flag.Parse()

	// Pin APP_ENV exactly as cmd/parity does, so the registry the harness reads
	// is the full one regardless of the caller's environment.
	_ = os.Setenv("APP_ENV", "development")
	httpapi.RegisterRoutes(fiber.New())

	if cfg.seed {
		if err := seed(cfg.fixtures); err != nil {
			fmt.Fprintln(os.Stderr, "seed:", err)
			return 1
		}
		return 0
	}

	if err := conform(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// seed opens the database and inserts the fixture catalogue and viewer.
func seed(fixturesPath string) error {
	db, err := database.InitDB()
	if err != nil {
		return err
	}
	var fx Fixtures
	if err := seedFixtures(db, &fx); err != nil {
		return err
	}
	if err := writeFixtures(fixturesPath, fx); err != nil {
		return err
	}
	fmt.Printf("seeded fixtures: movie=%d show=%d season=%d episode=%d list=%d comment=%d user=%s\n",
		fx.MovieID, fx.ShowID, fx.SeasonID, fx.EpisodeID, fx.ListID, fx.CommentID, fx.UserID)
	return nil
}

// result is one operation's outcome.
type result struct {
	Op     string
	Status string // PASS | FAIL | NO-CASE | OUT-OF-SCOPE
	Detail []string
}

func conform(cfg config) error {
	doc, err := contract.Load(cfg.specPath)
	if err != nil {
		return err
	}
	index, err := contract.LoadIndex(cfg.indexPath)
	if err != nil {
		return err
	}
	m, err := parity.Load(cfg.manifest)
	if err != nil {
		return err
	}
	fx, err := loadFixtures(cfg.fixtures)
	if err != nil {
		return err
	}

	routes := routeIndex()
	sc := buildScope(m, doc)
	allOps := doc.Operations()

	client, err := newClient(cfg.baseURL, cfg.timeout, fx, index, doc)
	if err != nil {
		return err
	}

	var results []result
	pass, fail, noCase := 0, 0, 0
	var gradedKeys []string
	for k := range sc.Graded {
		gradedKeys = append(gradedKeys, k)
	}
	sort.Strings(gradedKeys)

	for _, key := range gradedKeys {
		row := sc.Graded[key]
		op := findOp(allOps, key)
		c := generateCase(doc, op, row, routes, fx)
		if c.NoCaseReason != "" {
			noCase++
			results = append(results, result{Op: key, Status: "NO-CASE", Detail: []string{c.NoCaseReason}})
			continue
		}
		detail := client.runCase(c)
		if len(detail) == 0 {
			pass++
			results = append(results, result{Op: key, Status: "PASS"})
		} else {
			fail++
			results = append(results, result{Op: key, Status: "FAIL", Detail: detail})
		}
	}

	// Operations the contract carries that no implemented/divergent row covers.
	gradedSet := map[string]bool{}
	for _, k := range gradedKeys {
		gradedSet[k] = true
	}
	outOfScope := 0
	for _, op := range allOps {
		if !gradedSet[op.Method+" "+op.Path] {
			outOfScope++
		}
	}

	// Request-side probes (issue #88): malformed vs wrong-shape vs flexible ids.
	probeResults := client.runProbes()

	printTable(results)
	printProbes(probeResults)

	total := len(allOps)
	fmt.Printf("\n%d of %d conformant\n", pass, total)
	fmt.Printf("graded operations: %d  (pass %d, fail %d, no case %d)\n", len(gradedKeys), pass, fail, noCase)
	fmt.Printf("not graded (out of scope): %d\n", outOfScope)
	if failed := countFailed(probeResults); failed > 0 {
		fmt.Printf("request-side probes failed: %d\n", failed)
	}

	if n := reportDefects(results); n > 0 {
		fmt.Printf("defects found: %d (listed above; these become follow-up issues, not drive-by fixes)\n", n)
	}

	hardFail := fail > 0 || countFailed(probeResults) > 0
	if cfg.strict && noCase > 0 {
		hardFail = true
	}
	if hardFail {
		return fmt.Errorf("conformance FAILED")
	}
	return nil
}

func findOp(ops []contract.Operation, key string) contract.Operation {
	for _, op := range ops {
		if op.Method+" "+op.Path == key {
			return op
		}
	}
	return contract.Operation{}
}

func printTable(results []result) {
	fmt.Printf("%-52s %-6s %s\n", "OPERATION", "RESULT", "DETAIL")
	sort.Slice(results, func(i, j int) bool { return results[i].Op < results[j].Op })
	for _, r := range results {
		detail := ""
		if len(r.Detail) > 0 {
			detail = r.Detail[0]
			if len(r.Detail) > 1 {
				detail = fmt.Sprintf("%s (+%d more)", detail, len(r.Detail)-1)
			}
		}
		fmt.Printf("%-52s %-6s %s\n", truncate(r.Op, 52), r.Status, detail)
	}
}

func printProbes(results []result) {
	fmt.Printf("\nrequest-side probes (issue #88):\n")
	for _, r := range results {
		detail := ""
		if len(r.Detail) > 0 {
			detail = r.Detail[0]
		}
		fmt.Printf("  %-40s %-6s %s\n", truncate(r.Op, 40), r.Status, detail)
	}
}

func countFailed(results []result) int {
	n := 0
	for _, r := range results {
		if r.Status == "FAIL" {
			n++
		}
	}
	return n
}

func reportDefects(results []result) int {
	n := 0
	fmt.Printf("\n--- defects (operation: field/value that failed) ---\n")
	for _, r := range results {
		if r.Status != "FAIL" {
			continue
		}
		n++
		for _, d := range r.Detail {
			fmt.Printf("  %s: %s\n", r.Op, d)
		}
	}
	return n
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// --- HTTP client ------------------------------------------------------------

// client drives a live server: it logs in as the seeded viewer, sends the
// contract's headers, and validates responses against the contract.
type client struct {
	base    string
	http    *http.Client
	token   string
	fx      Fixtures
	index   *contract.Index
	doc     *contract.Document
	headers map[string]string
}

func newClient(base string, timeout time.Duration, fx Fixtures, index *contract.Index, doc *contract.Document) (*client, error) {
	c := &client{
		base:  strings.TrimRight(base, "/"),
		http:  &http.Client{Timeout: timeout},
		fx:    fx,
		index: index,
		doc:   doc,
		// The contract's own request headers: every Trakt operation carries the
		// API key and API version, and user-scoped ones additionally carry a
		// bearer token. We send all three; our server consumes the bearer.
		headers: map[string]string{
			"trakt-api-key":     "conformance-key",
			"trakt-api-version": "2",
			"Content-Type":      "application/json",
		},
	}
	token, err := c.login()
	if err != nil {
		return nil, err
	}
	c.token = token
	return c, nil
}

// login exchanges the seeded viewer's credentials for a session token over
// HTTP, the same way a client does.
func (c *client) login() (string, error) {
	body, _ := json.Marshal(map[string]string{"email": c.fx.Email, "password": c.fx.Password})
	req, _ := http.NewRequest("POST", c.base+"/v1/auth/local/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("login: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Session struct {
			Token string `json:"token"`
		} `json:"session"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Session.Token == "" {
		return "", fmt.Errorf("login: no token in response: %s", strings.TrimSpace(string(raw)))
	}
	return out.Session.Token, nil
}

// runCase sends one case and returns the violations it found (empty = PASS).
func (c *client) runCase(cs Case) []string {
	u := c.base + cs.Path
	if len(cs.Query) > 0 {
		u += "?" + cs.Query.Encode()
	}
	var body io.Reader
	if len(cs.Body) > 0 {
		body = bytes.NewReader(cs.Body)
	}
	req, err := http.NewRequest(cs.Method, u, body)
	if err != nil {
		return []string{"build request: " + err.Error()}
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if cs.Auth == "required" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return []string{"request failed: " + err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)

	var problems []string
	code := fmt.Sprintf("%d", resp.StatusCode)
	declared := false
	for _, ok := range cs.SuccessCodes {
		if code == ok {
			declared = true
			break
		}
	}
	if !declared {
		return []string{fmt.Sprintf("status %d is not a contract 2xx %v: %s", resp.StatusCode, cs.SuccessCodes, truncate(strings.TrimSpace(string(raw)), 200))}
	}

	// The contract's pagination headers, where the response is a list.
	if strings.HasPrefix(code, "2") && looksLikePagination(cs) {
		for _, h := range []string{"X-Pagination-Page", "X-Pagination-Limit", "X-Pagination-Total-Pages", "X-Pagination-Total-Items", "X-Pagination-Items-Count"} {
			if resp.Header.Get(h) == "" {
				problems = append(problems, "missing header "+h)
			}
		}
	}

	if len(raw) == 0 {
		return problems
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return append(problems, "response is not JSON: "+err.Error())
	}
	if schema := cs.Schemas[code]; schema != nil {
		problems = append(problems, c.doc.Validate(schema, value)...)
	}
	// Errors must still be a JSON object envelope.
	if isErrorCode(resp.StatusCode) {
		if _, ok := value.(map[string]any); !ok {
			problems = append(problems, "error body is not a JSON object envelope")
		}
	}
	return problems
}

func isErrorCode(code int) bool {
	switch code {
	case 400, 401, 403, 404, 409, 422, 429, 500:
		return true
	}
	return false
}

// looksLikePagination reports whether the contract's response for this case is
// an array — the case a client pages.
func looksLikePagination(cs Case) bool {
	for _, schema := range cs.Schemas {
		if contract.TypeName(schema) == "array" {
			return true
		}
	}
	return false
}

// runProbes exercises the request-side behaviour issue #88 fixed: a malformed
// body keeps the syntax message, a valid-JSON-wrong-shape body names the field,
// and a numeric id sent as a string is accepted.
func (c *client) runProbes() []result {
	target := c.base + "/v1/sync/watchlist"
	probes := []struct {
		name string
		body string
		want func(status int, msg string) []string
	}{
		{
			name: "malformed JSON -> syntax message",
			body: `{"movies":[`,
			want: func(status int, msg string) []string {
				if status != 400 {
					return []string{fmt.Sprintf("status %d, want 400", status)}
				}
				if msg != "invalid JSON body" {
					return []string{fmt.Sprintf("message %q, want the syntax message", msg)}
				}
				return nil
			},
		},
		{
			name: "wrong-shape JSON -> field named",
			body: `{"movies":{"ids":{"tmdb":550}}}`,
			want: func(status int, msg string) []string {
				if status != 400 {
					return []string{fmt.Sprintf("status %d, want 400", status)}
				}
				if msg == "invalid JSON body" {
					return []string{"a shape failure reported a syntax failure"}
				}
				return nil
			},
		},
		{
			name: "numeric id as string -> accepted",
			body: fmt.Sprintf(`{"movies":[{"ids":{"tmdb":"%d"}}]}`, c.fx.MovieTMDB),
			want: func(status int, msg string) []string {
				if status == 400 && msg == "invalid JSON body" {
					return []string{"a numeric string id was rejected as malformed JSON"}
				}
				return nil
			},
		},
	}
	var out []result
	for _, p := range probes {
		status, msg := c.post(target, p.body)
		detail := p.want(status, msg)
		if len(detail) == 0 {
			out = append(out, result{Op: p.name, Status: "PASS"})
		} else {
			out = append(out, result{Op: p.name, Status: "FAIL", Detail: detail})
		}
	}
	return out
}

// post sends a body and returns the status and the envelope message.
func (c *client) post(target, body string) (int, string) {
	req, err := http.NewRequest("POST", target, strings.NewReader(body))
	if err != nil {
		return 0, err.Error()
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &env)
	return resp.StatusCode, env.Error.Message
}

// asMap is a local helper the generator shares.
func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
