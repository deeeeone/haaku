package openapi

import "sync"

// RouteDef describes a single API endpoint for the OpenAPI generator.
type RouteDef struct {
	Method       string        // HTTP method: GET, POST, PUT, PATCH, DELETE
	Path         string        // URL path, e.g. "/healthz", "/v1/movies"
	Summary      string        // Short description for the operation
	Tags         []string      // Grouping tags; first tag used for path grouping
	Auth         string        // "none" | "optional" | "required" — anything else is skipped with a warning
	RequestBody  interface{}   // nil = no body; otherwise a Go type for docs
	ResponseType interface{}   // Go type for the success response body (200/201)
	StatusCodes  map[int]string // HTTP status → description (e.g. 200:"OK", 404:"Not Found")
}

var (
	mu     sync.RWMutex
	routes []RouteDef
)

// Register appends a route definition to the global registry (thread-safe).
func Register(route RouteDef) {
	mu.Lock()
	defer mu.Unlock()
	routes = append(routes, route)
}

// AllRoutes returns a snapshot of all registered routes (thread-safe).
func AllRoutes() []RouteDef {
	mu.RLock()
	defer mu.RUnlock()
	// Return a copy so callers can't mutate the internal slice.
	snapshot := make([]RouteDef, len(routes))
	copy(snapshot, routes)
	return snapshot
}

// Reset clears the registry. Exported for tests only — never call from production code.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	routes = nil
}
