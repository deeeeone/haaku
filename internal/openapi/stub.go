// Package openapi provides the code-first OpenAPI generator (T0.6 — in
// progress). The real implementation lives in generator.go + registry.go;
// this stub compiles while the agent builds those files.
package openapi

import "log"

// GenerateYAML walks the route registry and writes openapi.yaml.
// Non-fatal — callers log the error and continue.
// TODO(T0.6): replace with real generator.
func GenerateYAML(path string) error {
	log.Printf("[openapi] generator not yet implemented (T0.6 agent in progress) — skipping %s", path)
	return nil
}
