// Command contract-diff compares the frozen public-API contract
// (contracts/api/v1/openapi-baseline.yaml) against the working tree's generated
// specification (openapi.yaml) and fails when the difference includes a breaking
// change that has not been acknowledged in
// contracts/api/v1/breaking-changes.yaml.
//
// It exists because the freshness gate (cmd/openapi) proves the spec matches the
// code and the parity gate proves the routes match the Trakt inventory, but
// neither stops a pull request from silently dropping a path, a response field,
// a status code or an enum value that a third-party client — the Jellyfin plugin
// — depends on. The baseline is the contract as clients know it; a break must be
// written down before the gate will pass. See docs/architecture/api-versioning.md.
//
// Usage:
//
//	go run ./cmd/contract-diff            # print the diff, warn on an unacknowledged break
//	go run ./cmd/contract-diff -check     # exit 1 on an unacknowledged break (make contract-diff)
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/haaku-tv/haaku/internal/contractdiff"
)

func main() {
	baselinePath := flag.String("baseline", "contracts/api/v1/openapi-baseline.yaml",
		"frozen contract to compare against")
	currentPath := flag.String("current", "openapi.yaml",
		"the working tree's generated spec (never written by this tool)")
	acksPath := flag.String("acks", "contracts/api/v1/breaking-changes.yaml",
		"the acknowledged breaking-change record")
	check := flag.Bool("check", false,
		"exit non-zero on a breaking change that is not acknowledged (what make contract-diff runs)")
	flag.Parse()

	baseline, err := contractdiff.LoadDocument(*baselinePath)
	if err != nil {
		fatal(err)
	}
	current, err := contractdiff.LoadDocument(*currentPath)
	if err != nil {
		fatal(err)
	}
	acks, err := contractdiff.LoadAcknowledgements(*acksPath)
	if err != nil {
		fatal(err)
	}

	report := contractdiff.Evaluate(baseline, current, acks, *baselinePath, *currentPath)
	fmt.Print(report.Summary())

	if !report.Failure() {
		fmt.Println("\ncontract ok: no unacknowledged breaking change")
		return
	}

	fmt.Fprintf(os.Stderr, "%s", report.UnacknowledgedMessage())
	if *check {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
