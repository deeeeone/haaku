package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- clock ---------------------------------------------------------------

// testClock is a clock a test moves by hand, so folder ids and grace windows are
// deterministic.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock(t time.Time) *testClock { return &testClock{t: t.UTC()} }

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// --- memory destination --------------------------------------------------

// memDestination is an in-memory Destination. It records the order of Put calls
// and can be told to fail a named key, so a test can drive the commit-point and
// bounded-listing paths without touching the disk or the network.
type memDestination struct {
	mu       sync.Mutex
	objects  map[string][]byte
	modTimes map[string]time.Time
	puts     []string
	putErr   func(key string) error
	// now stamps object modification times. Defaults to time.Now.
	now func() time.Time
	// list is a hostile listing, when set.
	list func(prefix, after string, limit int) ([]ObjectInfo, string, error)
}

func newMemDestination() *memDestination {
	return &memDestination{objects: map[string][]byte{}, modTimes: map[string]time.Time{}}
}

func (d *memDestination) clockNow() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

func (d *memDestination) Put(_ context.Context, key string, r io.Reader) (int64, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.putErr != nil {
		if err := d.putErr(key); err != nil {
			return 0, err
		}
	}
	d.objects[key] = data
	d.modTimes[key] = d.clockNow()
	d.puts = append(d.puts, key)
	return int64(len(data)), nil
}

func (d *memDestination) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	data, ok := d.objects[key]
	if !ok {
		return nil, 0, ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

func (d *memDestination) ListPage(_ context.Context, prefix, after string, limit int) ([]ObjectInfo, string, error) {
	if d.list != nil {
		return d.list(prefix, after, limit)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var keys []string
	for key := range d.objects {
		if strings.HasPrefix(key, prefix) && key > after {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) <= limit {
		limit = len(keys)
	}
	items := make([]ObjectInfo, 0, limit)
	for _, key := range keys[:limit] {
		items = append(items, ObjectInfo{Key: key, Size: int64(len(d.objects[key])), ModTime: d.modTimes[key]})
	}
	next := ""
	if len(keys) > len(items) && len(items) > 0 {
		next = items[len(items)-1].Key
	}
	return items, next, nil
}

func (d *memDestination) Delete(_ context.Context, keys ...string) error {
	if err := checkDeleteKeys(keys); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, key := range keys {
		delete(d.objects, key)
		delete(d.modTimes, key)
	}
	return nil
}

func (d *memDestination) DeletePrefix(_ context.Context, prefix string) error {
	if err := checkDeletePrefix(prefix); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for key := range d.objects {
		if strings.HasPrefix(key, prefix) {
			delete(d.objects, key)
			delete(d.modTimes, key)
		}
	}
	return nil
}

func (d *memDestination) Probe(context.Context) error { return nil }

func (d *memDestination) Location(key string) string { return "mem://" + key }

// keys returns the object keys, sorted, for assertions.
func (d *memDestination) keys() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var keys []string
	for key := range d.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// putOrder returns the keys in the order Put received them.
func (d *memDestination) putOrder() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.puts...)
}

// overwrite replaces an object's bytes, to simulate a tampered destination.
func (d *memDestination) overwrite(key string, data []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.objects[key] = data
}

// --- endless destination (a hostile listing) -----------------------------

// endlessDestination answers ListPage without end, with keys that keep
// increasing. A bounded listing must refuse it rather than read forever.
type endlessDestination struct {
	*memDestination
	mu sync.Mutex
	n  int
}

func newEndlessDestination() *endlessDestination {
	return &endlessDestination{memDestination: newMemDestination()}
}

func (d *endlessDestination) ListPage(_ context.Context, prefix, _ string, limit int) ([]ObjectInfo, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	items := make([]ObjectInfo, 0, limit)
	for range limit {
		id := FolderName(base.Add(time.Duration(d.n)*time.Millisecond), KindNightly)
		d.n++
		items = append(items, ObjectInfo{Key: prefix + id + "/db.dump", Size: 10, ModTime: base})
	}
	return items, items[len(items)-1].Key, nil
}

// --- fake dumper / restorer ----------------------------------------------

type fakeDumper struct {
	data      []byte
	tables    []TableCount
	dumpErr   error
	invErr    error
	dumpDelay time.Duration
}

func (d *fakeDumper) Dump(ctx context.Context, w io.Writer) (DumpInfo, error) {
	if d.dumpDelay > 0 {
		select {
		case <-time.After(d.dumpDelay):
		case <-ctx.Done():
			return DumpInfo{}, ctx.Err()
		}
	}
	if d.dumpErr != nil {
		return DumpInfo{}, d.dumpErr
	}
	if _, err := w.Write(d.data); err != nil {
		return DumpInfo{}, err
	}
	return DumpInfo{PgDumpVersion: "pg_dump (PostgreSQL) 17.11", DatabaseName: "trakt"}, nil
}

func (d *fakeDumper) Inventory(context.Context) ([]TableCount, error) {
	if d.invErr != nil {
		return nil, d.invErr
	}
	return d.tables, nil
}

type fakeRestorer struct {
	mu    sync.Mutex
	calls int
	got   []byte
	err   error
}

func (r *fakeRestorer) Restore(_ context.Context, in io.Reader) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.got = data
	return r.err
}

func (r *fakeRestorer) restored() (int, []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, append([]byte(nil), r.got...)
}

// --- helpers -------------------------------------------------------------

// discardLogger keeps test output clean.
func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// baseOptions is the shared starting point for a test Service, with a fixed
// clock and generous defaults a test narrows as needed.
func baseOptions(dest Destination, clock *testClock, dumper Dumper) Options {
	return Options{
		Destination:     dest,
		Dumper:          dumper,
		Restorer:        &fakeRestorer{},
		Locker:          NewMemoryLocker(),
		Now:             clock.now,
		Keep:            2,
		RequireTables:   []string{"users"},
		IncompleteGrace: time.Hour,
		AppVersion:      "test",
		Logger:          discardLogger(),
	}
}

// testService builds a Service over the fakes with a fixed clock at start.
func testService(t *testing.T, dest Destination, dumper Dumper, restorer Restorer, opts ...func(*Options)) (*Service, *testClock) {
	t.Helper()
	clock := newClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if md, ok := dest.(*memDestination); ok && md.now == nil {
		md.now = clock.now
	}
	if ed, ok := dest.(*endlessDestination); ok && ed.now == nil {
		ed.now = clock.now
	}
	o := baseOptions(dest, clock, dumper)
	o.Restorer = restorer
	for _, f := range opts {
		f(&o)
	}
	svc, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc, clock
}

// goodDumper returns a dumper whose inventory has a users table, so a backup is
// complete.
func goodDumper() *fakeDumper {
	return &fakeDumper{
		data:   []byte("PGDMP fake custom-format archive"),
		tables: []TableCount{{Name: "users", Present: true, Rows: 3}},
	}
}

var errBoom = errors.New("boom")

// requireStatus asserts a summary's status.
func requireStatus(t *testing.T, got Status, want Status) {
	t.Helper()
	if got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

// find lists and returns the summary for id.
func findSummary(t *testing.T, svc *Service, id string) Summary {
	t.Helper()
	listing, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, s := range listing.Backups {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("backup %s not listed", id)
	return Summary{}
}
