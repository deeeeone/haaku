package obs

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// scrape fetches a URL and returns the body, failing the test on any non-200.
func scrape(t *testing.T, url string) (int, string, http.Header) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp.StatusCode, string(b), resp.Header
}

// TestStartDisabledWhenUnset pins the default: with no address configured the
// listener is off and Start is a no-op.
func TestStartDisabledWhenUnset(t *testing.T) {
	t.Setenv(EnvMetricsListen, "")
	if err := Start(context.Background()); err != nil {
		t.Fatalf("Start with no address: %v", err)
	}
	if addr := activeAddr(); addr != "" {
		t.Fatalf("listener bound %q, want disabled", addr)
	}
}

// TestMetricsListenerServesLabeledSeries is the core regression: the listener
// binds, serves Prometheus text, and exposes the labelled series. It fails if the
// obs implementation is reverted to ErrNotImplemented.
func TestMetricsListenerServesLabeledSeries(t *testing.T) {
	t.Setenv(EnvMetricsListen, "127.0.0.1:0")
	if err := Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})

	addr := activeAddr()
	if addr == "" {
		t.Fatal("Start returned nil but nothing is listening")
	}

	// Emit one HTTP observation per status and one worker's worth of series.
	ObserveHTTPRequest("/v1/users/:id", "GET", 200, 0.012)
	ObserveHTTPRequest("/v1/users/:id", "GET", 404, 0.003)
	IncRateLimited()
	w := NewWorker("stats_worker")
	w.Heartbeat()
	w.Processed(2)
	w.Failed(1)

	status, body, header := scrape(t, "http://"+addr+"/metrics")
	if status != http.StatusOK {
		t.Fatalf("GET /metrics: status %d, want 200", status)
	}
	if ct := header.Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want Prometheus text/plain", ct)
	}

	// Labels are rendered in alphabetical order: method, route, status.
	wants := []string{
		`haaku_http_requests_total{method="GET",route="/v1/users/:id",status="200"} 1`,
		`haaku_http_requests_total{method="GET",route="/v1/users/:id",status="404"} 1`,
		`haaku_http_rate_limited_total 1`,
		`haaku_worker_heartbeat_timestamp_seconds{worker="stats_worker"}`,
		`haaku_worker_processed_total{worker="stats_worker"} 2`,
		`haaku_worker_failed_total{worker="stats_worker"} 1`,
		`haaku_redis_available`,
		`haaku_redis_circuit_breaker_state{state=`,
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("scrape is missing %q", want)
		}
	}

	// The latency histogram must be present, and it must carry the route and
	// status labels — that is the whole point of the per-route series.
	if !strings.Contains(body, "# TYPE haaku_http_request_duration_seconds histogram") {
		t.Error("latency histogram type line missing")
	}
	if !strings.Contains(body, `haaku_http_request_duration_seconds_count{method="GET",route="/v1/users/:id",status="200"} 1`) {
		t.Errorf("latency histogram is not labelled by route and status:\n%s", grepLines(body, "haaku_http_request_duration_seconds_count"))
	}
}

// grepLines returns the lines of body containing substr, for readable failures.
func grepLines(body, substr string) string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, substr) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// TestStartReportsBindFailure proves a bad address fails at Start instead of
// silently serving nothing.
func TestStartReportsBindFailure(t *testing.T) {
	t.Setenv(EnvMetricsListen, "256.256.256.256:0")
	if err := Start(context.Background()); err == nil {
		_ = Shutdown(context.Background())
		t.Fatal("Start accepted an invalid address")
	}
}

// TestWorkerHeartbeatRecordsNow pins the semantics an alert depends on: the
// heartbeat gauge is the worker's wall-clock liveness, so a stale value (or a
// missing series) means the worker stopped beating.
func TestWorkerHeartbeatRecordsNow(t *testing.T) {
	const name = "heartbeat_probe"
	before := time.Now().Unix()
	NewWorker(name).Heartbeat()

	mfs, err := Gatherer().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "haaku_worker_heartbeat_timestamp_seconds" {
			continue
		}
		for _, m := range mf.Metric {
			for _, lp := range m.Label {
				if lp.GetName() == "worker" && lp.GetValue() == name {
					got := int64(m.GetGauge().GetValue())
					if got < before {
						t.Fatalf("heartbeat %d predates the call (%d)", got, before)
					}
					return
				}
			}
		}
	}
	t.Fatal("heartbeat series for the probe worker not found")
}
