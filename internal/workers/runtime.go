package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/haaku-tv/haaku/internal/obs"
)

// DefaultHealthListen is where a worker-only process answers /healthz and
// /readyz. It is a loopback port away from the API's 8080 because the omnibus
// image runs a worker service beside the API in one network namespace.
const DefaultHealthListen = "127.0.0.1:9090"

// HealthListenEnv overrides DefaultHealthListen.
const HealthListenEnv = "HAAKU_WORKER_HEALTH_LISTEN"

// healthShutdownTimeout bounds how long the health listener may take to drain.
const healthShutdownTimeout = 2 * time.Second

// HealthListenAddr returns the address a worker-only process should serve
// health on: $HAAKU_WORKER_HEALTH_LISTEN, or DefaultHealthListen.
func HealthListenAddr() string {
	if v := strings.TrimSpace(os.Getenv(HealthListenEnv)); v != "" {
		return v
	}
	return DefaultHealthListen
}

// Runtime owns the selected workers: it starts their loops and schedules, emits
// their heartbeats, recovers a panic per pass and per job, exposes a readiness
// signal, and stops them gracefully when it is shut down.
//
// One Runtime lives per process. The workers it was built with are the only
// ones it can run, so selecting a worker in two processes is what double-runs
// it — which is why a dedicated worker deployment sets HAAKU_WORKER=none on
// the API (see docs/architecture/workers.md).
type Runtime struct {
	logger  *slog.Logger
	deps    Deps
	workers []Worker
	health  string

	cron    *cron.Cron
	baseCtx context.Context
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	ready   atomic.Bool

	healthSrv  *http.Server
	healthAddr atomic.Value // string: the bound address, once listening.
}

// NewRuntime builds a runtime for an explicit set of workers. Use Catalog to
// build that set from a Selection.
func NewRuntime(deps Deps, ws []Worker, healthListen string) *Runtime {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Runtime{logger: deps.Logger, deps: deps, workers: ws, health: healthListen}
}

// Start launches every worker and returns once they are running. The workers
// stop when ctx is cancelled, or when Shutdown is called. A returned error is
// a startup failure — currently only the health listener failing to bind.
func (r *Runtime) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.baseCtx = ctx

	r.logger.Info("worker runtime starting", "workers", r.names(), "health_listen", r.health)

	// One heartbeat per worker up front, so every selected worker has a series
	// from the first scrape even if its schedule runs rarely (or, like the
	// stats seam, not at all yet).
	for _, w := range r.workers {
		obs.NewWorker(w.Name).Heartbeat()
	}

	for _, w := range r.workers {
		switch w.Kind {
		case Loop:
			if w.Step == nil {
				return fmt.Errorf("worker %q is a loop with no step", w.Name)
			}
			if w.Interval <= 0 {
				return fmt.Errorf("worker %q has a non-positive interval %s", w.Name, w.Interval)
			}
			r.wg.Add(1)
			go r.runLoop(ctx, w)
		case Schedule:
			if len(w.Jobs) == 0 {
				r.logger.Warn("worker has no jobs registered yet; nothing is scheduled",
					"worker", w.Name, "hint", "the recompute engine lands in a later change")
			}
		default:
			return fmt.Errorf("worker %q has an unknown kind %d", w.Name, w.Kind)
		}
	}

	r.startSchedules()
	r.ready.Store(true)

	if r.health != "" {
		if err := r.startHealthListener(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Shutdown stops the workers and waits for them, bounded by ctx. It is safe to
// call once; a second call is a no-op.
func (r *Runtime) Shutdown(ctx context.Context) error {
	if r.cancel != nil {
		r.cancel()
	}
	var err error

	// cron.Stop stops scheduling new runs and returns a context that is done
	// once the runs already in flight return. Cancelling the runtime context
	// asked those runs to stop; this waits for them to notice.
	if r.cron != nil {
		done := r.cron.Stop()
		select {
		case <-done.Done():
		case <-ctx.Done():
			err = ctx.Err()
		}
	}

	loops := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(loops)
	}()
	select {
	case <-loops:
	case <-ctx.Done():
		err = ctx.Err()
	}

	if r.healthSrv != nil {
		drain, cancel := context.WithTimeout(context.Background(), healthShutdownTimeout)
		defer cancel()
		_ = r.healthSrv.Shutdown(drain)
	}
	return err
}

// Ready reports whether the runtime has finished starting: every loop is
// running and the schedule is up. It is the readiness signal /readyz serves.
func (r *Runtime) Ready() bool { return r.ready.Load() }

// HealthAddr returns the address the health listener bound, or "" when none is
// configured (or it never bound). Useful for diagnostics and tests.
func (r *Runtime) HealthAddr() string {
	if v, ok := r.healthAddr.Load().(string); ok {
		return v
	}
	return ""
}

// runLoop runs one Loop worker until ctx is cancelled.
func (r *Runtime) runLoop(ctx context.Context, w Worker) {
	defer r.wg.Done()

	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	for {
		r.runPass(ctx, obs.NewWorker(w.Name), w.Name, func(ctx context.Context) error {
			return w.Step(ctx, r.deps)
		})
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runPass runs one pass of a Loop worker: it beats the worker's heartbeat,
// runs the step, and turns a panic into a logged failure instead of a dead
// process. The step owns its own processed/failed counting; the runtime only
// records the failure it had to catch.
func (r *Runtime) runPass(ctx context.Context, m obs.Worker, name string, step func(context.Context) error) {
	m.Heartbeat()
	defer func() {
		if rec := recover(); rec != nil {
			m.Failed(1)
			r.logger.Error("worker pass panicked; recovered",
				"worker", name, "panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
		}
	}()
	if err := step(ctx); err != nil {
		r.logger.Warn("worker pass failed", "worker", name, "error", err)
	}
}

// startSchedules builds the single UTC cron that drives every Schedule worker.
// It is a no-op when nothing is scheduled, so a stats-only selection does not
// pay for a scheduler it never uses.
func (r *Runtime) startSchedules() {
	scheduled := false
	for _, w := range r.workers {
		if w.Kind == Schedule && len(w.Jobs) > 0 {
			scheduled = true
			break
		}
	}
	if !scheduled {
		return
	}

	c := cron.New(cron.WithLocation(time.UTC))
	for _, w := range r.workers {
		if w.Kind != Schedule {
			continue
		}
		for _, job := range w.Jobs {
			name := w.Name
			if _, err := c.AddFunc(job.Schedule, func() { r.runJob(name, job) }); err != nil {
				r.logger.Error("worker job has an invalid schedule",
					"worker", name, "schedule", job.Schedule, "error", err)
				continue
			}
			r.logger.Info("worker job scheduled",
				"worker", name, "schedule", job.Schedule, "timezone", "UTC", "does", job.Describe)
		}
	}
	c.Start()
	r.cron = c
}

// runJob runs one scheduled job with the same heartbeat and panic recovery a
// loop pass gets. The job's context is the runtime's, so a shutdown stops it.
func (r *Runtime) runJob(name string, job Job) {
	m := obs.NewWorker(name)
	m.Heartbeat()
	defer func() {
		if rec := recover(); rec != nil {
			m.Failed(1)
			r.logger.Error("worker job panicked; recovered",
				"worker", name, "schedule", job.Schedule, "panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
		}
	}()

	ctx := r.baseCtx
	if ctx == nil {
		ctx = context.Background()
	}
	r.logger.Info("worker job starting", "worker", name, "schedule", job.Schedule, "does", job.Describe)
	if err := job.Run(ctx, r.deps); err != nil {
		r.logger.Warn("worker job failed", "worker", name, "schedule", job.Schedule, "error", err)
		return
	}
	r.logger.Info("worker job finished", "worker", name, "schedule", job.Schedule)
}

// startHealthListener serves the worker mode's liveness and readiness signals on
// a listener of its own, so an orchestrator can probe a process that serves no
// API: /healthz is liveness (the process is up), /readyz is readiness (the
// workers are running).
func (r *Runtime) startHealthListener(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !r.Ready() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "starting"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	ln, err := net.Listen("tcp", r.health)
	if err != nil {
		return fmt.Errorf("worker health listener bind %s: %w", r.health, err)
	}

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	r.healthSrv = srv
	r.healthAddr.Store(ln.Addr().String())

	go func() {
		<-ctx.Done()
		drain, cancel := context.WithTimeout(context.Background(), healthShutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(drain)
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.logger.Error("worker health listener stopped unexpectedly", "error", err)
		}
	}()

	r.logger.Info("worker health listener started", "addr", ln.Addr().String())
	return nil
}

// names returns the runtime's worker names, for logs.
func (r *Runtime) names() []string {
	out := make([]string, len(r.workers))
	for i, w := range r.workers {
		out[i] = w.Name
	}
	return out
}

// writeJSON writes a small JSON body with a status code, matching the shape the
// API's /healthz and /readyz answer with.
func writeJSON(w http.ResponseWriter, status int, body map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
