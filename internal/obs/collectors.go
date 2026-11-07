package obs

import (
	"database/sql"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/redis"
)

// The dependency collectors read live state from the owning packages at scrape
// time rather than being incremented from deep inside them. Database pools and
// Redis availability are process singletons, so a scrape is the natural place to
// sample them, and it keeps instrumentation out of the query and cache paths.

// --- Database pool -----------------------------------------------------------

// sqlPoolReader returns the live *sql.DB pool, or ok=false when no database is
// configured. The indirection is what lets the collector be tested without a
// running Postgres.
type sqlPoolReader func() (pool *sql.DB, ok bool)

// dbPoolCollector reports database/sql pool saturation from one pool. It owns
// only the exposition; the pool configuration stays in internal/database.
type dbPoolCollector struct {
	read        sqlPoolReader
	connections *prometheus.Desc
	waitCount   *prometheus.Desc
	waitSeconds *prometheus.Desc
}

// newDBPoolCollector wires the collector to the process database singleton.
func newDBPoolCollector() *dbPoolCollector {
	return newDBPoolCollectorFrom(func() (*sql.DB, bool) {
		gormDB, err := database.GetDB()
		if err != nil || gormDB == nil {
			return nil, false
		}
		sqlDB, err := gormDB.DB()
		if err != nil || sqlDB == nil {
			return nil, false
		}
		return sqlDB, true
	})
}

func newDBPoolCollectorFrom(read sqlPoolReader) *dbPoolCollector {
	return &dbPoolCollector{
		read: read,
		connections: prometheus.NewDesc(
			metricsNamespace+"_db_pool_connections",
			"Database pool connections by state: \"open\" and \"in_use\" against \"max\" show saturation.",
			[]string{"state"}, nil),
		waitCount: prometheus.NewDesc(
			metricsNamespace+"_db_pool_wait_total",
			"Total number of times a request had to wait for a database connection.",
			nil, nil),
		waitSeconds: prometheus.NewDesc(
			metricsNamespace+"_db_pool_wait_seconds_total",
			"Total time spent waiting for a database connection, in seconds.",
			nil, nil),
	}
}

func (c *dbPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.connections
	ch <- c.waitCount
	ch <- c.waitSeconds
}

func (c *dbPoolCollector) Collect(ch chan<- prometheus.Metric) {
	pool, ok := c.read()
	if !ok || pool == nil {
		return // no database configured — report nothing rather than a fake zero
	}
	s := pool.Stats()
	for _, state := range []struct {
		name  string
		value float64
	}{
		{"max", float64(s.MaxOpenConnections)},
		{"open", float64(s.OpenConnections)},
		{"in_use", float64(s.InUse)},
		{"idle", float64(s.Idle)},
	} {
		ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, state.value, state.name)
	}
	ch <- prometheus.MustNewConstMetric(c.waitCount, prometheus.CounterValue, float64(s.WaitCount))
	ch <- prometheus.MustNewConstMetric(c.waitSeconds, prometheus.CounterValue, s.WaitDuration.Seconds())
}

// --- Redis -------------------------------------------------------------------

// redisCollector reports whether Redis is usable and, when it is not, why: the
// circuit breaker state is the operator-visible symptom of a Redis outage.
type redisCollector struct {
	available *prometheus.Desc
	cbState   *prometheus.Desc
}

func newRedisCollector() *redisCollector {
	return &redisCollector{
		available: prometheus.NewDesc(
			metricsNamespace+"_redis_available",
			"1 when a Redis client is configured and the circuit breaker is healthy, 0 otherwise.",
			nil, nil),
		cbState: prometheus.NewDesc(
			metricsNamespace+"_redis_circuit_breaker_state",
			"Redis circuit breaker state; 1 on the active state, 0 on the others.",
			[]string{"state"}, nil),
	}
}

func (c *redisCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.available
	ch <- c.cbState
}

func (c *redisCollector) Collect(ch chan<- prometheus.Metric) {
	state := redis.GetCircuitBreakerState()

	available := 0.0
	if redis.GetClient() != nil && redis.IsCircuitBreakerHealthy() {
		available = 1
	}
	ch <- prometheus.MustNewConstMetric(c.available, prometheus.GaugeValue, available)

	// one-hot so an alert can key on the state name; GetState returns a fixed
	// vocabulary (closed / open / half-open / unknown).
	for _, s := range []string{"closed", "open", "half-open", "unknown"} {
		v := 0.0
		if s == state {
			v = 1
		}
		ch <- prometheus.MustNewConstMetric(c.cbState, prometheus.GaugeValue, v, s)
	}
}
