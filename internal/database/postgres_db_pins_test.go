//go:build postgres

// Query budget pins (issue #14 §4).
//
// The pins live in scripts/ci/db-pins.txt and name the hot queries that already
// have a dedicated index in migrations/sql. This test migrates a throwaway
// database, seeds the fixture the pins assume, and asks PostgreSQL for the plan.
// A pin fails when the plan no longer uses the named index, or when it
// sequentially scans the base table — the shape a non-indexable rewrite takes.
//
// It is part of the `postgres` suite, so `make test-db-pins` (and CI's postgres
// job) runs it and `go test ./...` does not.

package database

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// dbPin is one [section] of scripts/ci/db-pins.txt.
type dbPin struct {
	Name  string
	Index string // an "Index Name" the plan must contain
	Table string // the base table the plan must not sequentially scan
	SQL   string // the query, as the planner sees it
}

// planNode is the slice of an EXPLAIN (FORMAT JSON) plan this test reads.
type planNode struct {
	NodeType     string     `json:"Node Type"`
	IndexName    string     `json:"Index Name"`
	RelationName string     `json:"Relation Name"`
	Plans        []planNode `json:"Plans"`
}

func loadDBPins(t *testing.T) []dbPin {
	t.Helper()
	// Tests run in the package directory; the pins file is a committed repo
	// artifact, so a missing one is a failure, not a skip.
	path := filepath.Join("..", "..", "scripts", "ci", "db-pins.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the pins file %s: %v", path, err)
	}

	var pins []dbPin
	cur := -1
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			pins = append(pins, dbPin{Name: strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")})
			cur = len(pins) - 1
			continue
		}
		if cur < 0 {
			t.Fatalf("db-pins.txt line %d: %q is outside a [section]", i+1, line)
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("db-pins.txt line %d: expected `key = value`, got %q", i+1, line)
		}
		switch strings.TrimSpace(key) {
		case "index":
			pins[cur].Index = strings.TrimSpace(value)
		case "table":
			pins[cur].Table = strings.TrimSpace(value)
		case "sql":
			pins[cur].SQL = strings.TrimSpace(value)
		default:
			t.Fatalf("db-pins.txt line %d: unknown key %q", i+1, strings.TrimSpace(key))
		}
	}

	if len(pins) == 0 {
		t.Fatal("db-pins.txt has no pins")
	}
	for _, p := range pins {
		if p.Name == "" || p.Index == "" || p.Table == "" || p.SQL == "" {
			t.Fatalf("db-pins.txt pin %q is incomplete (need index, table and sql)", p.Name)
		}
	}
	return pins
}

func flattenPlan(n planNode) []planNode {
	out := []planNode{n}
	for _, child := range n.Plans {
		out = append(out, flattenPlan(child)...)
	}
	return out
}

// TestQueryBudgetPins is the Makefile's `test-db-pins` target.
func TestQueryBudgetPins(t *testing.T) {
	pins := loadDBPins(t)

	dsn := newTestDatabase(t)
	g := openGorm(t, dsn)
	if err := runMigrate(t, g, testOptions(t)); err != nil {
		t.Fatalf("migrating the pin fixture database: %v", err)
	}
	seedPinFixture(t, g)

	for _, pin := range pins {
		t.Run(pin.Name, func(t *testing.T) {
			assertPin(t, g, pin)
		})
	}
}

func assertPin(t *testing.T, g *gorm.DB, pin dbPin) {
	t.Helper()

	var raw []byte
	if err := g.Raw("EXPLAIN (FORMAT JSON) " + pin.SQL).Row().Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN for pin %q: %v", pin.Name, err)
	}
	var plans []struct {
		Plan planNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil {
		t.Fatalf("parsing the plan JSON for pin %q: %v\n%s", pin.Name, err, raw)
	}
	if len(plans) != 1 {
		t.Fatalf("pin %q: expected one top-level plan, got %d", pin.Name, len(plans))
	}
	nodes := flattenPlan(plans[0].Plan)

	var usedIndexes, scannedTables []string
	indexFound := false
	for _, n := range nodes {
		if n.IndexName != "" {
			usedIndexes = append(usedIndexes, n.IndexName)
			if n.IndexName == pin.Index {
				indexFound = true
			}
		}
		if n.NodeType == "Seq Scan" && n.RelationName == pin.Table {
			scannedTables = append(scannedTables, n.RelationName)
		}
	}

	if !indexFound {
		t.Fatalf("pin %q: the plan does not use %s; indexes used: %v\nquery: %s",
			pin.Name, pin.Index, usedIndexes, pin.SQL)
	}
	if len(scannedTables) > 0 {
		t.Fatalf("pin %q: the plan sequentially scans %s; query: %s",
			pin.Name, strings.Join(scannedTables, ", "), pin.SQL)
	}

	t.Logf("pin %q ok: served by %s (indexes in plan: %s)", pin.Name, pin.Index, strings.Join(usedIndexes, ", "))
}

// seedPinFixture lays down the data the pins assume: enough rows that the
// planner has real statistics, the trending window's rows inside the last 7
// days, and a selective genre for the containment pin.
func seedPinFixture(t *testing.T, g *gorm.DB) {
	t.Helper()

	// Comments: created_at spread over 7 days (so the trending window keeps
	// rows) with distinct comment_type/media_type values.
	if err := g.Exec(`INSERT INTO comments (user_id, media_type, media_id, comment_type, body, like_count, reply_count, created_at)
		SELECT 'u'||(i%100),
		       CASE WHEN i%2 = 0 THEN 'movie' ELSE 'show' END,
		       i,
		       CASE WHEN i%3 = 0 THEN 'review' ELSE 'shout' END,
		       'body',
		       (i*7919)%200, (i*17)%50,
		       now() - (interval '1 hour') * (i%168)
		FROM generate_series(1, 20000) AS i`).Error; err != nil {
		t.Fatalf("seeding comments: %v", err)
	}

	// Movies: one selective genre every 2000 rows, so the jsonb containment
	// predicate is index-selective (~0.05%), which is what idx_movies_genres_gin
	// is for.
	if err := g.Exec(`INSERT INTO movies (tmdb_id, title, genres, popularity)
		SELECT i, 'Movie '||i,
		       CASE WHEN i%2000 = 0 THEN jsonb_build_array('Arthouse') ELSE jsonb_build_array('Drama') END,
		       (i*37)%1000
		FROM generate_series(1, 20000) AS i`).Error; err != nil {
		t.Fatalf("seeding movies: %v", err)
	}

	for _, table := range []string{"comments", "movies"} {
		if err := g.Exec(fmt.Sprintf("ANALYZE %s", table)).Error; err != nil {
			t.Fatalf("ANALYZE %s: %v", table, err)
		}
	}
}
