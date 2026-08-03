package redis

import "sync"

// singleFlightGroup implements the single-flight pattern to prevent
// thundering herd when multiple goroutines fetch the same cache key concurrently.
type singleFlightGroup struct {
	calls map[string]*call
	mu    sync.Mutex
}

// call represents one in-flight request.
type call struct {
	wg  sync.WaitGroup
	val interface{}
	err error
}

// newSingleFlightGroup creates a new single-flight group.
func newSingleFlightGroup() *singleFlightGroup {
	return &singleFlightGroup{
		calls: make(map[string]*call),
	}
}

// Do executes fn once for key. Concurrent callers for the same key block
// and receive the result of the first call.
func (g *singleFlightGroup) Do(key string, fn func() (interface{}, error)) (interface{}, error) {
	g.mu.Lock()
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}

	c := new(call)
	c.wg.Add(1)
	g.calls[key] = c
	g.mu.Unlock()

	c.val, c.err = fn()
	c.wg.Done()

	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()

	return c.val, c.err
}

// Global single-flight group for cache operations.
var cacheSingleFlight = newSingleFlightGroup()

// NewSingleFlightGroupForTest is exposed for unit tests.
func NewSingleFlightGroupForTest() *singleFlightGroup {
	return newSingleFlightGroup()
}
