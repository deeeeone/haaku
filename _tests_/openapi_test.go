package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haaku-tv/haaku/internal/openapi"
	"gopkg.in/yaml.v3"
)

// TestRouteRegistration verifies that a registered route appears in generated YAML.
func TestRouteRegistration(t *testing.T) {
	// Save existing routes and restore after test so other tests aren't affected.
	saved := openapi.AllRoutes()
	defer func() {
		openapi.Reset()
		for _, r := range saved {
			openapi.Register(r)
		}
	}()
	openapi.Reset()

	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/healthz",
		Summary: "Health check",
		Tags:    []string{"Health"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "Service is healthy",
		},
	})

	tmp := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := openapi.GenerateYAML(tmp); err != nil {
		t.Fatalf("GenerateYAML failed: %v", err)
	}

	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("failed to read generated YAML: %v", err)
	}

	// Parse the YAML.
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("generated file is not valid YAML: %v\nContent:\n%s", err, string(data))
	}

	// Verify the OpenAPI version.
	if v, ok := doc["openapi"].(string); !ok || v != "3.1.0" {
		t.Errorf("expected openapi '3.1.0', got %v", doc["openapi"])
	}

	// Verify info.
	info, ok := doc["info"].(map[string]interface{})
	if !ok {
		t.Fatal("missing 'info' section")
	}
	if info["title"] != "haaku" {
		t.Errorf("expected title 'haaku', got %v", info["title"])
	}

	// Verify servers.
	servers, ok := doc["servers"].([]interface{})
	if !ok || len(servers) == 0 {
		t.Fatal("missing or empty 'servers' section")
	}

	// Verify /healthz path exists.
	paths, ok := doc["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("missing 'paths' section")
	}
	hz, ok := paths["/healthz"].(map[string]interface{})
	if !ok {
		t.Fatalf("path /healthz not found in generated spec. Paths: %v", paths)
	}
	get, ok := hz["get"].(map[string]interface{})
	if !ok {
		t.Fatal("missing GET method for /healthz")
	}
	if get["summary"] != "Health check" {
		t.Errorf("unexpected summary: %v", get["summary"])
	}
	if tags, ok := get["tags"].([]interface{}); !ok || len(tags) == 0 || tags[0] != "Health" {
		t.Errorf("unexpected tags: %v", get["tags"])
	}

	// Verify security is empty (public route).
	sec, _ := get["security"].([]interface{})
	if len(sec) != 0 {
		t.Errorf("expected empty security for public route, got %v", sec)
	}

	// Verify components exist.
	components, ok := doc["components"].(map[string]interface{})
	if !ok {
		t.Fatal("missing 'components' section")
	}
	if _, ok := components["securitySchemes"]; !ok {
		t.Fatal("missing securitySchemes")
	}
	if _, ok := components["schemas"]; !ok {
		t.Fatal("missing schemas")
	}
}

// TestUnknownAuthSkipped verifies that routes with unknown auth types are silently skipped.
func TestUnknownAuthSkipped(t *testing.T) {
	saved := openapi.AllRoutes()
	defer func() {
		openapi.Reset()
		for _, r := range saved {
			openapi.Register(r)
		}
	}()
	openapi.Reset()

	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/secret-admin",
		Summary: "VIP admin panel",
		Tags:    []string{"Admin"},
		Auth:    "magic-ring", // not a recognized auth type
		StatusCodes: map[int]string{
			200: "OK",
		},
	})
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/healthz",
		Summary: "Health",
		Tags:    []string{"Health"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "OK",
		},
	})

	tmp := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := openapi.GenerateYAML(tmp); err != nil {
		t.Fatalf("GenerateYAML failed: %v", err)
	}

	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("failed to read generated YAML: %v", err)
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}

	paths, ok := doc["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("missing 'paths'")
	}

	// /healthz should be present.
	if _, ok := paths["/healthz"]; !ok {
		t.Error("/healthz not found — known auth 'none' route should be present")
	}

	// /secret-admin should NOT be present (unknown auth → skipped).
	if _, ok := paths["/secret-admin"]; ok {
		t.Error("/secret-admin found — unknown auth route should have been skipped")
	}
}

// TestEmptyRegistry verifies that an empty registry produces a valid skeleton.
func TestEmptyRegistry(t *testing.T) {
	saved := openapi.AllRoutes()
	defer func() {
		openapi.Reset()
		for _, r := range saved {
			openapi.Register(r)
		}
	}()
	openapi.Reset()

	tmp := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := openapi.GenerateYAML(tmp); err != nil {
		t.Fatalf("GenerateYAML failed: %v", err)
	}

	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("failed to read generated YAML: %v", err)
	}

	content := string(data)
	if len(strings.TrimSpace(content)) == 0 {
		t.Fatal("generated YAML is empty")
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("empty registry generated invalid YAML: %v\nContent:\n%s", err, content)
	}

	// Must have the required top-level keys.
	for _, key := range []string{"openapi", "info", "servers", "paths", "components"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("missing top-level key '%s' in empty-registry skeleton", key)
		}
	}

	// paths should be empty (no routes).
	paths, _ := doc["paths"].(map[string]interface{})
	if len(paths) != 0 {
		t.Errorf("expected empty paths, got %d entries", len(paths))
	}
}
