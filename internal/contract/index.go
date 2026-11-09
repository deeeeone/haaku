package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Index is the derived per-operation summary written by
// scripts/parity-reconcile.py (docs/parity/schema-index.json).
//
// It is LOSSLESS for parameters and bodies only in the common case: union
// types collapse (`extended` comes out as {"type":["null"]}) and response
// schemas are reduced to a type or an object key list. The harness therefore
// uses it as a fast path — to know which operations have a body, and to detect
// that an entry is too thin to trust — and falls back to the vendored document
// for every schema it actually validates against.
type Index struct {
	Ops map[string]IndexEntry
}

// IndexEntry is one operation's summary.
type IndexEntry struct {
	OperationID string           `json:"operationId"`
	Tags        []string         `json:"tags"`
	Summary     string           `json:"summary"`
	Parameters  []IndexParameter `json:"parameters"`
	Body        *IndexBody       `json:"body"`
	Responses   map[string]any   `json:"responses"`
}

// IndexParameter is one request parameter as the index records it.
type IndexParameter struct {
	Name     string          `json:"name"`
	In       string          `json:"in"`
	Required bool            `json:"required"`
	TypeRaw  json.RawMessage `json:"type"`
	Enum     []string        `json:"enum"`
}

// Types returns the parameter's declared type(s). The index generator emits a
// union type as a list and a single type as a bare string, so both are accepted.
func (p IndexParameter) Types() []string {
	if len(p.TypeRaw) == 0 {
		return nil
	}
	var list []string
	if err := json.Unmarshal(p.TypeRaw, &list); err == nil {
		return list
	}
	var single string
	if err := json.Unmarshal(p.TypeRaw, &single); err == nil {
		return []string{single}
	}
	return nil
}

// IndexBody is the request body summary.
type IndexBody struct {
	ContentType string         `json:"contentType"`
	Schema      map[string]any `json:"schema"`
}

// LoadIndex reads and parses the schema index at path.
func LoadIndex(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read schema index: %w", err)
	}
	var ops map[string]IndexEntry
	if err := json.Unmarshal(data, &ops); err != nil {
		return nil, fmt.Errorf("parse schema index %s: %w", path, err)
	}
	return &Index{Ops: ops}, nil
}

// Entry returns the index entry for a method+path key ("GET /movies/{id}").
func (i *Index) Entry(method, path string) (IndexEntry, bool) {
	e, ok := i.Ops[method+" "+path]
	return e, ok
}

// Thin reports whether an index entry is too lossy to validate against — a
// parameter whose type collapsed to null-only, or a body/responses summary that
// carries no shape. When it is true the caller must use the vendored document.
func (e IndexEntry) Thin() bool {
	for _, p := range e.Parameters {
		if t := p.Types(); len(t) == 1 && t[0] == "null" {
			return true
		}
	}
	if e.Body != nil {
		if t := TypeName(e.Body.Schema); t == "unknown" || t == "null" {
			return true
		}
	}
	for code, shape := range e.Responses {
		if strings.HasPrefix(code, "2") && shape == nil {
			return true
		}
	}
	return false
}
