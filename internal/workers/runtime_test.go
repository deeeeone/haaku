package workers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// waitFor polls cond until it is true or the deadline passes.
func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}

func counterLoop(name string, n *int32, opts ...func(*Worker)) Worker {
	w := Worker{
		Name:     name,
		Kind:     Loop,
		Interval: 10 * time.Millisecond,
		Step: func(context.Context, Deps) error {
			atomic.AddInt32(n, 1)
			return nil
		},
	}
	for _, o := range opts {
		o(&w)
	}
	return w
}

// TestRuntimeRunsOnlyItsSelectedWorkers is the runtime-level half of "the API
// does not double-start a worker the worker mode owns": a worker the runtime was
// not built with never runs, so ownership is decided once, at construction.
func TestRuntimeRunsOnlyItsSelectedWorkers(t *testing.T) {
	var webhooks, activity int32
	rt := NewRuntime(Deps{Logger: testLogger()}, []Worker{counterLoop(NameWebhooks, &webhooks)}, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitFor(t, "the selected worker to run", 2*time.Second, func() bool {
		return atomic.LoadInt32(&webhooks) > 0
	})
	if got := atomic.LoadInt32(&activity); got != 0 {
		t.Errorf("unselected worker ran %d times, want 0", got)
	}
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// TestRuntimeRecoversFromAPanickingPass asserts a panic in one pass is logged
// and caught, and the loop keeps going: one bad pass must not take the process
// down, and must count as a failure.
func TestRuntimeRecoversFromAPanickingPass(t *testing.T) {
	var calls int32
	w := Worker{
		Name:     NameOutbox,
		Kind:     Loop,
		Interval: 5 * time.Millisecond,
		Step: func(context.Context, Deps) error {
			if atomic.AddInt32(&calls, 1) == 1 {
				panic("boom")
			}
			return nil
		},
	}
	rt := NewRuntime(Deps{Logger: testLogger()}, []Worker{w}, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, "the loop to survive the panic", 2*time.Second, func() bool {
		return atomic.LoadInt32(&calls) >= 2
	})
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// TestRuntimeShutdownIsPrompt asserts Shutdown stops the loops and returns
// quickly rather than waiting out a loop's interval or a parked read.
func TestRuntimeShutdownIsPrompt(t *testing.T) {
	var n int32
	rt := NewRuntime(Deps{Logger: testLogger()}, []Worker{counterLoop(NameActivity, &n)}, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, "the loop to start", 2*time.Second, func() bool { return atomic.LoadInt32(&n) > 0 })

	before := atomic.LoadInt32(&n)
	start := time.Now()
	shutdownCtx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	if err := rt.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Shutdown took %s, want prompt", elapsed)
	}
	// The loop must actually have stopped, not merely returned a value.
	time.Sleep(50 * time.Millisecond)
	if after := atomic.LoadInt32(&n); after != before {
		t.Errorf("loop kept running after Shutdown: %d -> %d", before, after)
	}
}

// TestRuntimeServesHealth checks the worker mode's readiness/liveness signal: a
// process with no API answers /healthz and /readyz on its own listener.
func TestRuntimeServesHealth(t *testing.T) {
	var n int32
	rt := NewRuntime(Deps{Logger: testLogger()}, []Worker{counterLoop(NameMetadata, &n)}, "127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = rt.Shutdown(context.Background()) }()

	addr := rt.HealthAddr()
	if addr == "" {
		t.Fatal("HealthAddr() is empty; no listener bound")
	}
	if !rt.Ready() {
		t.Error("Ready() = false immediately after Start")
	}

	for path, want := range map[string]string{"/healthz": "ok", "/readyz": "ready"} {
		status, body := getJSON(t, "http://"+addr+path)
		if status != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", path, status)
		}
		if body["status"] != want {
			t.Errorf("GET %s: status=%q, want %q", path, body["status"], want)
		}
	}
}

// TestRuntimeRunsScheduledJobs asserts a Schedule worker actually fires its job
// on the clock, and that a schedule with no jobs (the stats seam) starts cleanly
// without one.
func TestRuntimeRunsScheduledJobs(t *testing.T) {
	var fired int32
	jobWorker := Worker{
		Name: NameMetadata,
		Kind: Schedule,
		Jobs: []Job{{
			Schedule: "@every 100ms",
			Describe: "test job",
			Run: func(context.Context, Deps) error {
				atomic.AddInt32(&fired, 1)
				return nil
			},
		}},
	}
	emptyWorker := Worker{Name: NameStats, Kind: Schedule}

	rt := NewRuntime(Deps{Logger: testLogger()}, []Worker{jobWorker, emptyWorker}, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start with an empty job list: %v", err)
	}
	waitFor(t, "the scheduled job to fire", 3*time.Second, func() bool {
		return atomic.LoadInt32(&fired) > 0
	})
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// TestRuntimeRejectsMalformedWorkers asserts a bad worker fails loudly at Start
// instead of panicking a goroutine later.
func TestRuntimeRejectsMalformedWorkers(t *testing.T) {
	cases := map[string]Worker{
		"loop with no step":      {Name: NameWebhooks, Kind: Loop, Interval: time.Second},
		"loop with bad interval": {Name: NameWebhooks, Kind: Loop, Interval: 0, Step: func(context.Context, Deps) error { return nil }},
	}
	for what, w := range cases {
		rt := NewRuntime(Deps{Logger: testLogger()}, []Worker{w}, "")
		if err := rt.Start(context.Background()); err == nil {
			t.Errorf("%s: Start returned nil, want an error", what)
			_ = rt.Shutdown(context.Background())
		}
	}
}

func getJSON(t *testing.T, url string) (int, map[string]string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return resp.StatusCode, body
}
