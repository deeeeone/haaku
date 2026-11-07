package openapi

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

// standardStatusCodes are the response codes included on every operation
// (see api-conventions.md §9).
var standardStatusCodes = map[int]string{
	400: "invalid request",
	401: "unauthorized",
	404: "not found",
	409: "conflict",
	422: "unprocessable",
	429: "rate limited",
	500: "internal server error",
}

// GenerateYAML walks the route registry, builds an OpenAPI 3.1 document,
// and writes it to the given path.
//
// The output is deterministic: every object is a Go map, and gopkg.in/yaml.v3
// marshals map keys in sorted order, so two runs over the same registry are
// byte-identical. That is what makes `git diff --exit-code` a meaningful
// freshness check.
func GenerateYAML(path string) error {
	out, err := GenerateYAMLBytes()
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, out, 0o644); err != nil {
		log.Printf("Warning: failed to write openapi.yaml to %s: %v", path, err)
		return fmt.Errorf("write openapi.yaml: %w", err)
	}

	return nil
}

// GenerateYAMLBytes builds the OpenAPI document from the current registry and
// returns it as marshalled YAML. Used by the freshness check so it can compare
// against the file on disk without writing anything.
func GenerateYAMLBytes() ([]byte, error) {
	out, err := yaml.Marshal(buildDocument(AllRoutes()))
	if err != nil {
		return nil, fmt.Errorf("marshal openapi yaml: %w", err)
	}
	return out, nil
}

// GenerateJSON returns the OpenAPI document, built from the current registry,
// as indented JSON. json.MarshalIndent sorts object keys, so it is
// deterministic. Served by GET /v1/openapi.json.
func GenerateJSON() ([]byte, error) {
	out, err := json.MarshalIndent(buildDocument(AllRoutes()), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal openapi json: %w", err)
	}
	return out, nil
}

// buildDocument constructs the full OpenAPI 3.1 map from route definitions.
func buildDocument(allRoutes []RouteDef) map[string]interface{} {
	doc := map[string]interface{}{
		"openapi": "3.1.0",
		"info": map[string]interface{}{
			"title":   "haaku",
			"version": "0.1.0",
		},
		"servers": []map[string]interface{}{
			{
				"url":         "http://localhost:8080",
				"description": "Local development",
			},
		},
		"paths": buildPaths(allRoutes),
		"components": map[string]interface{}{
			"securitySchemes": securitySchemes(),
			"schemas": map[string]interface{}{
				"ErrorEnvelope": errorEnvelopeSchema(),
			},
		},
	}

	return doc
}

// buildPaths groups routes and builds the paths object.
func buildPaths(routes []RouteDef) map[string]interface{} {
	paths := make(map[string]interface{})

	for _, r := range routes {
		// Unknown auth types → log warning and skip.
		switch r.Auth {
		case "none", "optional", "required":
			// allowed
		default:
			log.Printf("Warning: skipping route %s %s — unknown auth type %q",
				r.Method, r.Path, r.Auth)
			continue
		}

		op := map[string]interface{}{
			"summary":     r.Summary,
			"operationId": fmt.Sprintf("%s_%s", sanitizeOpID(r.Path), r.Method),
			"tags":        r.Tags,
			"security":    authToSecurity(r.Auth),
			"responses":   buildResponses(r),
		}

		if r.RequestBody != nil {
			op["requestBody"] = buildRequestBody(r.RequestBody)
		}

		// Nest under the path + lower-cased method.
		method := toLower(r.Method)
		if _, ok := paths[r.Path]; !ok {
			paths[r.Path] = make(map[string]interface{})
		}
		paths[r.Path].(map[string]interface{})[method] = op
	}

	return paths
}

// authToSecurity maps auth string to OpenAPI security requirements.
func authToSecurity(auth string) []map[string][]string {
	switch auth {
	case "none":
		return []map[string][]string{} // empty → public (no auth needed)
	case "optional":
		// Either API key or OAuth works.
		return []map[string][]string{
			{"apiKey": {}},
			{"oauth": {}},
		}
	case "required":
		return []map[string][]string{
			{"apiKey": {}},
			{"oauth": {}},
		}
	default:
		return nil
	}
}

// buildResponses produces the responses map for an operation.
func buildResponses(r RouteDef) map[string]interface{} {
	responses := make(map[string]interface{})

	// Add route-specific success response.
	for code, desc := range r.StatusCodes {
		resp := map[string]interface{}{
			"description": desc,
		}
		// For 200/201, include the response schema if a ResponseType is set.
		if (code == 200 || code == 201) && r.ResponseType != nil {
			resp["content"] = map[string]interface{}{
				"application/json": map[string]interface{}{
					"schema": r.ResponseType,
				},
			}
		}
		responses[fmt.Sprintf("%d", code)] = resp
	}

	// Add standard error responses (won't overwrite route-specific ones).
	for code, desc := range standardStatusCodes {
		key := fmt.Sprintf("%d", code)
		if _, exists := responses[key]; exists {
			continue
		}
		content := map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": map[string]interface{}{
					"$ref": "#/components/schemas/ErrorEnvelope",
				},
			},
		}
		responses[key] = map[string]interface{}{
			"description": desc,
			"content":     content,
		}
	}

	return responses
}

// securitySchemes defines apiKey (header) and oauth (bearer) schemes.
func securitySchemes() map[string]interface{} {
	return map[string]interface{}{
		"apiKey": map[string]interface{}{
			"type": "apiKey",
			"in":   "header",
			"name": "X-API-Key",
		},
		"oauth": map[string]interface{}{
			"type":         "http",
			"scheme":       "bearer",
			"bearerFormat": "JWT",
		},
	}
}

// errorEnvelopeSchema returns the standard error envelope JSON schema.
func errorEnvelopeSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"error": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"code": map[string]interface{}{
						"type": "string",
					},
					"message": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"code", "message"},
			},
		},
		"required": []string{"error"},
	}
}

// buildRequestBody creates a requestBody object from a Go type.
func buildRequestBody(bodyType interface{}) map[string]interface{} {
	return map[string]interface{}{
		"required": true,
		"content": map[string]interface{}{
			"application/json": map[string]interface{}{
				"schema": bodyType,
			},
		},
	}
}

// sanitizeOpID turns a path into a safe operationId fragment.
func sanitizeOpID(path string) string {
	result := make([]byte, 0, len(path))
	for _, b := range []byte(path) {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') {
			result = append(result, b)
		} else if b == '/' || b == '-' || b == '_' {
			result = append(result, '_')
		}
	}
	return string(result)
}

// toLower returns s lowercased.
func toLower(s string) string {
	b := make([]byte, len(s))
	for i, c := range []byte(s) {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		} else {
			b[i] = c
		}
	}
	return string(b)
}
