// Package obs owns the observability listeners: metrics, and later tracing and
// profiling (issue #9).
//
// The listener is deliberately separate from the API port. Metrics carry no
// application auth, so exposing them on the public listener — as the hand-rolled
// /metrics handler did — publishes traffic volume to anyone who can reach the
// app. A self-hosted operator points their scraper at a private port instead.
package obs

import (
	"context"
	"errors"
	"os"
	"strings"
)

// EnvMetricsListen names the address the metrics listener binds to. Empty
// disables it, which is the default outside a monitored deployment.
const EnvMetricsListen = "TKILLER_METRICS_LISTEN"

// ErrNotImplemented is returned when a listener is requested but not yet built.
// It exists so a deployment that asks for metrics fails loudly instead of
// silently scraping nothing.
var ErrNotImplemented = errors.New("metrics listener is not implemented yet (see issue #9)")

// MetricsAddr returns the configured metrics address, or "" when disabled.
func MetricsAddr() string {
	return strings.TrimSpace(os.Getenv(EnvMetricsListen))
}

// Start binds the observability listeners and returns once they are listening.
//
// It is called from the API and worker modes. When no listener is configured it
// returns nil without starting anything.
func Start(_ context.Context) error {
	if MetricsAddr() == "" {
		return nil
	}
	return ErrNotImplemented
}
