// Command parity grades docs/parity/parity.yaml — the machine-checkable Trakt
// parity manifest — against the code-first route registry, and prints coverage.
//
// It exists so "how much of Trakt do we serve?" is measured from the registry
// rather than asserted in prose. The manifest is data (one row per Trakt
// endpoint, graded honest/divergent/blocked); the registry is the only source of
// truth for what we actually serve, so the two are diffed on every run.
//
// TODO remove this once we reach parity, we'll branch and no longer need to check
// 
// Usage:
//
//	go run ./cmd/parity            # print the coverage table, warn on problems
//	go run ./cmd/parity -check     # same, but exit 1 on any problem (make parity-check)
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/haaku-tv/haaku/internal/httpapi"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/parity"

	"github.com/gofiber/fiber/v2"
)

func main() {
	manifestPath := flag.String("manifest", "docs/parity/parity.yaml", "path to the parity manifest")
	check := flag.Bool("check", false,
		"exit non-zero when the manifest disagrees with the route registry (what make parity-check runs)")
	flag.Parse()

	// Pin the environment exactly as cmd/openapi does: the manifest is graded
	// against the full registered surface, so the result does not depend on the
	// caller's APP_ENV.
	if err := os.Setenv("APP_ENV", "development"); err != nil {
		fatal(err)
	}

	// Populate the registry through the same code path the server uses.
	httpapi.RegisterRoutes(fiber.New())

	m, err := parity.Load(*manifestPath)
	if err != nil {
		fatal(err)
	}

	registry := parity.NewRouteSet(openapi.AllRoutes())
	problems := parity.Check(m, registry)

	fmt.Print(parity.Compute(m).Table())

	if len(problems) == 0 {
		fmt.Printf("\nmanifest ok: every row agrees with the route registry\n")
		return
	}

	fmt.Fprintf(os.Stderr, "\n%d manifest problem(s):\n", len(problems))
	for _, p := range problems {
		fmt.Fprintf(os.Stderr, "  %s\n", p)
	}
	fmt.Fprintf(os.Stderr, "\nfix %s (or the route registry) — see docs/parity/README.md\n", *manifestPath)
	if *check {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
