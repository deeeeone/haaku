// Package mode models the server run modes.
//
// One binary serves every role, so that a single image can run the API, a
// background worker, or a one-shot migration (see the omnibus image design,
// issue #13). Keeping the modes in one place is what lets a worker be incapable
// of migrating the schema by construction rather than by convention.
package mode

import (
	"fmt"
	"os"
	"strings"
)

// Mode is a server run mode.
type Mode string

const (
	// API serves HTTP and runs the in-process background workers.
	API Mode = "api"
	// Worker runs background workers only and serves no HTTP. It never touches
	// the schema.
	Worker Mode = "worker"
	// Migrate applies pending schema migrations and exits. Only this mode (and
	// API) may change the schema.
	Migrate Mode = "migrate"
)

// EnvVar is the environment variable that selects the mode.
const EnvVar = "HAAKU_MODE"

// Parse converts a raw value to a Mode, defaulting to API when empty.
func Parse(raw string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(API):
		return API, nil
	case string(Worker):
		return Worker, nil
	case string(Migrate):
		return Migrate, nil
	default:
		return "", fmt.Errorf("unknown mode %q (want %s, %s or %s)", raw, API, Worker, Migrate)
	}
}

// FromEnv reads the mode from the environment.
func FromEnv() (Mode, error) {
	return Parse(os.Getenv(EnvVar))
}

// ServesHTTP reports whether the mode starts an HTTP listener.
func (m Mode) ServesHTTP() bool { return m == API }

// RunsWorkers reports whether the mode starts the background workers.
func (m Mode) RunsWorkers() bool { return m == API || m == Worker }

// MayMigrate reports whether the mode is allowed to change the schema.
//
// A worker must never migrate: several of them run against one database, and a
// schema change under a running API is a deployment accident, not a feature.
func (m Mode) MayMigrate() bool { return m == API || m == Migrate }

// String implements fmt.Stringer.
func (m Mode) String() string { return string(m) }
