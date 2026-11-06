package obs

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// metricsNamespace prefixes every metric this service publishes, so a scrape
// that also collects runtime and process metrics stays readable.
const metricsNamespace = "tkiller"

// registry holds only what this service registers. A dedicated registry (rather
// than prometheus.DefaultRegisterer) keeps the exposition free of anything an
// imported library registers implicitly, and keeps tests deterministic.
var registry = prometheus.NewRegistry()

var (
	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "HTTP requests processed, by route pattern, method and response status.",
	}, []string{"route", "method", "status"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request latency in seconds, by route pattern, method and response status.",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"route", "method", "status"})

	httpInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: "http",
		Name:      "requests_in_flight",
		Help:      "HTTP requests currently being served.",
	})

	httpRateLimited = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "http",
		Name:      "rate_limited_total",
		Help:      "HTTP requests rejected by the rate limiter (counted before routing, so globally).",
	})

	workerHeartbeat = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: "worker",
		Name:      "heartbeat_timestamp_seconds",
		Help:      "Unix time of the last heartbeat emitted by a background worker; a scrape that sees this stop advancing means the worker is dead.",
	}, []string{"worker"})

	workerProcessed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "worker",
		Name:      "processed_total",
		Help:      "Items a background worker processed successfully.",
	}, []string{"worker"})

	workerFailed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "worker",
		Name:      "failed_total",
		Help:      "Items a background worker failed to process.",
	}, []string{"worker"})
)

func init() {
	registry.MustRegister(
		// Runtime + process metrics, so a scrape can answer "is it up and is it
		// slow" without a second exporter.
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		// Service metrics.
		httpRequests,
		httpDuration,
		httpInFlight,
		httpRateLimited,
		workerHeartbeat,
		workerProcessed,
		workerFailed,
		// Dependency metrics, read from the owning packages at scrape time.
		newDBPoolCollector(),
		newRedisCollector(),
	)
}

// Observers — the write side of the metrics above. Every label value passed here
// must be bounded: see internal/httpapi for how route patterns (not raw paths)
// and a method allowlist keep cardinality from growing with client input.

// ObserveHTTPRequest records one completed request.
//
// route must be a registered route pattern or the "unmatched" sentinel, method a
// normalised HTTP verb, and status the status code the client actually received.
func ObserveHTTPRequest(route, method string, status int, seconds float64) {
	code := strconv.Itoa(status)
	httpRequests.WithLabelValues(route, method, code).Inc()
	httpDuration.WithLabelValues(route, method, code).Observe(seconds)
}

// IncInFlight / DecInFlight track requests currently being served.
func IncInFlight() { httpInFlight.Inc() }
func DecInFlight() { httpInFlight.Dec() }

// IncRateLimited counts a request rejected by the rate limiter.
func IncRateLimited() { httpRateLimited.Inc() }

// Worker is a handle on one background worker's health metrics. A worker beats
// once per loop iteration and reports what it processed or failed; a scraper
// alerts when the heartbeat stops advancing.
type Worker struct{ name string }

// NewWorker returns the metric handle for a named worker. The name is a fixed
// label, never user input.
func NewWorker(name string) Worker { return Worker{name: name} }

// Heartbeat records that the worker is alive as of now.
func (w Worker) Heartbeat() {
	workerHeartbeat.WithLabelValues(w.name).Set(float64(time.Now().Unix()))
}

// Processed adds n successfully processed items.
func (w Worker) Processed(n int) {
	if n > 0 {
		workerProcessed.WithLabelValues(w.name).Add(float64(n))
	}
}

// Failed adds n items the worker could not process.
func (w Worker) Failed(n int) {
	if n > 0 {
		workerFailed.WithLabelValues(w.name).Add(float64(n))
	}
}
