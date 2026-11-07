package parity_test

import (
	"os"
	"testing"

	"github.com/haaku-tv/haaku/internal/httpapi"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/parity"

	"github.com/gofiber/fiber/v2"
)

// manifestPath is the committed manifest, relative to this package directory.
const manifestPath = "../../docs/parity/parity.yaml"

// TestCommittedManifestMatchesTheRegistry is the standing invariant: the
// manifest shipped in docs/parity/parity.yaml agrees with the code-first route
// registry — no row points at a route that does not exist, no row calls a 501
// route implemented, and every 501 route is recorded.
//
// It runs with `make test-go`, so drift is caught by the test suite as well as
// by `make parity-check`.
func TestCommittedManifestMatchesTheRegistry(t *testing.T) {
	// cmd/openapi pins APP_ENV so the committed artefact describes the full
	// surface; the grader must see the same surface.
	t.Setenv("APP_ENV", "development")
	httpapi.RegisterRoutes(fiber.New())

	m, err := parity.Load(manifestPath)
	if err != nil {
		t.Fatalf("load %s: %v", manifestPath, err)
	}

	problems := parity.Check(m, parity.NewRouteSet(openapi.AllRoutes()))
	for _, p := range problems {
		t.Errorf("manifest problem: %s", p)
	}

	coverage := parity.Compute(m)
	t.Log("\n" + coverage.Table())

	if coverage.Trakt == 0 {
		t.Fatal("manifest has no Trakt rows")
	}
	if coverage.Implemented() == 0 {
		t.Fatal("manifest records no implemented Trakt endpoint at all — the inventory cannot be empty")
	}
	if coverage.Implemented()+coverage.Divergent() > coverage.Trakt {
		t.Fatalf("implementation counts exceed the inventory: %d + %d > %d",
			coverage.Implemented(), coverage.Divergent(), coverage.Trakt)
	}
}

// TestManifestFileIsWhereTheDocsSay keeps docs/parity/README.md honest.
func TestManifestFileIsWhereTheDocsSay(t *testing.T) {
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("the documented manifest path is missing: %v", err)
	}
}
