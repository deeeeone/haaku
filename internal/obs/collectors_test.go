package obs

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// --- a dependency-free sql driver -------------------------------------------
//
// The pool collector only reads (*sql.DB).Stats(), so the test needs a handle
// whose pool settings it controls — not a working database. A stub driver lets
// sql.DB exist without dialing anything.

type stubDriver struct{}

func (stubDriver) Open(string) (driver.Conn, error) { return stubConn{}, nil }

type stubConn struct{}

func (stubConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("stub: not implemented") }
func (stubConn) Close() error                        { return nil }
func (stubConn) Begin() (driver.Tx, error)           { return nil, errors.New("stub: not implemented") }

type stubConnector struct{}

func (stubConnector) Connect(context.Context) (driver.Conn, error) { return stubConn{}, nil }
func (stubConnector) Driver() driver.Driver                        { return stubDriver{} }

// gaugeByLabel gathers a collector and returns {labelValue: value} for the one
// metric family with the given name. Fails if the family is absent.
func gaugeByLabel(t *testing.T, reg *prometheus.Registry, family, label string) map[string]float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != family {
			continue
		}
		out := make(map[string]float64, len(mf.Metric))
		for _, m := range mf.Metric {
			key := ""
			for _, lp := range m.Label {
				if lp.GetName() == label {
					key = lp.GetValue()
				}
			}
			out[key] = m.GetGauge().GetValue()
		}
		return out
	}
	t.Fatalf("metric family %q not found", family)
	return nil
}

// TestDBPoolCollectorReportsSaturation proves the DB metric tracks real pool
// configuration: "max" must mirror SetMaxOpenConns, which is what an alert on
// pool exhaustion keys on.
func TestDBPoolCollectorReportsSaturation(t *testing.T) {
	pool := sql.OpenDB(stubConnector{})
	t.Cleanup(func() { _ = pool.Close() })
	pool.SetMaxOpenConns(7)
	pool.SetMaxIdleConns(3)

	reg := prometheus.NewRegistry()
	reg.MustRegister(newDBPoolCollectorFrom(func() (*sql.DB, bool) { return pool, true }))

	states := gaugeByLabel(t, reg, "tkiller_db_pool_connections", "state")
	if got := states["max"]; got != 7 {
		t.Errorf("max = %v, want 7", got)
	}
	if got := states["in_use"]; got != 0 {
		t.Errorf("in_use = %v, want 0", got)
	}
	if _, ok := states["idle"]; !ok {
		t.Error("idle state series missing")
	}
}

// TestDBPoolCollectorSilentWithoutDatabase pins that a service with no database
// reports no pool series rather than a misleading zero.
func TestDBPoolCollectorSilentWithoutDatabase(t *testing.T) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(newDBPoolCollectorFrom(func() (*sql.DB, bool) { return nil, false }))
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "tkiller_db_pool_connections" {
			t.Fatal("pool series emitted with no database configured")
		}
	}
}

// TestRedisCollectorExposesAvailabilityAndBreakerState proves the Redis series
// are present — availability 0 with no client, and a one-hot breaker state.
func TestRedisCollectorExposesAvailabilityAndBreakerState(t *testing.T) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(newRedisCollector())

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var available *dto.MetricFamily
	var breaker *dto.MetricFamily
	for _, mf := range mfs {
		switch mf.GetName() {
		case "tkiller_redis_available":
			available = mf
		case "tkiller_redis_circuit_breaker_state":
			breaker = mf
		}
	}
	if available == nil {
		t.Fatal("tkiller_redis_available missing")
	}
	if got := available.Metric[0].GetGauge().GetValue(); got != 0 {
		t.Errorf("redis available = %v, want 0 with no client configured", got)
	}
	if breaker == nil {
		t.Fatal("tkiller_redis_circuit_breaker_state missing")
	}
	ones := 0
	for _, m := range breaker.Metric {
		if m.GetGauge().GetValue() == 1 {
			ones++
		}
	}
	if ones != 1 {
		t.Errorf("breaker state is not one-hot: %d states set to 1", ones)
	}
}
