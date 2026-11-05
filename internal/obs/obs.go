// Package obs owns the observability listeners: metrics, and later tracing and
// profiling (issue #9).
//
// The listener is deliberately separate from the API port. Metrics carry no
// application auth, so exposing them on the public listener — as the hand-rolled
// /metrics handler did — publishes traffic volume to anyone who can reach the
// app. A self-hosted operator points their scraper at a private port instead,
// the same shape Silo uses for SILO_METRICS_LISTEN.
//
// Metrics are process-local by design: the counters and histograms in this
// package reset when the process restarts, which is exactly what a Prometheus
// counter is expected to do (a restart is a new series, not a data loss). The
// exporter is Prometheus text format via github.com/prometheus/client_golang.
package obs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// EnvMetricsListen names the address the metrics listener binds to. Empty
// disables it, which is the default outside a monitored deployment.
const EnvMetricsListen = "TKILLER_METRICS_LISTEN"

// MetricsAddr returns the configured metrics address, or "" when disabled.
func MetricsAddr() string {
	return strings.TrimSpace(os.Getenv(EnvMetricsListen))
}

// Gatherer exposes the process metrics registry for tests and future exporters.
func Gatherer() prometheus.Gatherer { return registry }

// Handler returns the Prometheus handler that renders the registry. The listener
// serves it at /metrics; it is exported so a test can scrape without a socket.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

var (
	mu      sync.Mutex
	current *metricsServer
)

type metricsServer struct {
	http    *http.Server
	ln      net.Listener
	stopSig context.CancelFunc
}

// Start binds the observability listeners and returns once they are listening.
//
// It is called from the API and worker modes. When no listener is configured it
// returns nil without starting anything.
//
// The returned error is a bind failure (address in use, malformed address): the
// caller chooses whether that is fatal. Serving happens on a background
// goroutine, so a bound-but-busy port still fails here, promptly, rather than
// silently scraping nothing.
func Start(ctx context.Context) error {
	addr := MetricsAddr()
	if addr == "" {
		return nil
	}
	return startOn(addr, ctx)
}

// startOn is the testable core of Start: an explicit address instead of the
// environment, and a context that cancels the listener.
func startOn(addr string, ctx context.Context) error {
	mu.Lock()
	defer mu.Unlock()
	if current != nil {
		return fmt.Errorf("obs: metrics listener already started on %s", current.ln.Addr())
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("obs: metrics listener bind %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Shut down on the caller's context or a termination signal. cmd/api passes a
	// background context (the seam it was given), so the signal watcher is what
	// lets the listener drain on SIGINT/SIGTERM instead of being cut off at
	// process exit. signal.NotifyContext is additive: it does not steal the
	// signal from the process's own handler.
	sigCtx, stopSig := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)

	s := &metricsServer{http: srv, ln: ln, stopSig: stopSig}
	current = s

	go func() {
		<-sigCtx.Done()
		shutdown(context.Background())
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("obs: metrics listener stopped unexpectedly", "error", err)
		}
	}()

	slog.Info("obs: metrics listener started", "addr", ln.Addr().String())
	return nil
}

// Shutdown stops the metrics listener, if one is running, and waits for in-flight
// scrapes to finish or ctx to expire. Safe to call when nothing is listening.
func Shutdown(ctx context.Context) error {
	return shutdown(ctx)
}

func shutdown(ctx context.Context) error {
	mu.Lock()
	s := current
	current = nil
	mu.Unlock()
	if s == nil {
		return nil
	}
	if s.stopSig != nil {
		s.stopSig()
	}
	return s.http.Shutdown(ctx)
}

// activeAddr reports the bound address, or "" when nothing is listening. Used by
// tests (and useful for diagnostics) to discover an ephemeral port.
func activeAddr() string {
	mu.Lock()
	defer mu.Unlock()
	if current == nil {
		return ""
	}
	return current.ln.Addr().String()
}
