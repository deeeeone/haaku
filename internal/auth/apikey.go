package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"trakt-api/internal/openapi"
	tredis "trakt-api/redis"

	"github.com/gofiber/fiber/v2"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	apiKeyPrefix      = "tk_"
	apiKeyRandomBytes = 32 // 32 random bytes → 64 hex chars
	apiKeyCacheTTL    = 1 * time.Hour
	apiKeyRedisPrefix = "apikey:"
)

// ---------------------------------------------------------------------------
// Local error helpers (avoid import cycle with httpapi)
// ---------------------------------------------------------------------------

func writeError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{"error": fiber.Map{"code": code, "message": message}})
}

func apiBadRequest(c *fiber.Ctx, message string) error {
	return writeError(c, fiber.StatusBadRequest, "invalid_request", message)
}

func apiUnauthorized(c *fiber.Ctx) error {
	return writeError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or expired token")
}

func apiInternalError(c *fiber.Ctx, message string) error {
	return writeError(c, fiber.StatusInternalServerError, "internal", message)
}

// ---------------------------------------------------------------------------
// OpenAPI route registration
// ---------------------------------------------------------------------------

func init() {
	routes := []openapi.RouteDef{
		{Method: "POST", Path: "/v1/users/me/api_keys", Summary: "Generate a new API key", Tags: []string{"API Keys"}, Auth: "required", StatusCodes: map[int]string{201: "API key created", 400: "Invalid request"}},
		{Method: "GET", Path: "/v1/users/me/api_keys", Summary: "List user's API keys (masked)", Tags: []string{"API Keys"}, Auth: "required", StatusCodes: map[int]string{200: "List of API keys"}},
		{Method: "DELETE", Path: "/v1/users/me/api_keys/{key_id}", Summary: "Revoke an API key", Tags: []string{"API Keys"}, Auth: "required", StatusCodes: map[int]string{200: "Key revoked", 404: "Key not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterAPIKeyRoutes wires all API key routes.
func RegisterAPIKeyRoutes(api fiber.Router) {
	me := api.Group("/users/me/api_keys")
	me.Post("/", handleCreateAPIKey)
	me.Get("/", handleListAPIKeys)
	me.Delete("/:key_id", handleRevokeAPIKey)
}

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------

// APIKeyEntry stored in Clerk unsafe_metadata.api_keys.
type APIKeyEntry struct {
	ID        string `json:"id"`
	Prefix    string `json:"prefix"`   // tk_<first 8 chars>
	KeyHash   string `json:"key_hash"` // SHA-256 of the full key
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	LastUsed  string `json:"last_used,omitempty"`
}

type createAPIKeyRequest struct {
	Name string `json:"name"`
}

type createAPIKeyResponse struct {
	ID     string `json:"id"`
	Key    string `json:"key"` // Only returned once!
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
}

type listAPIKeyResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Prefix    string `json:"prefix"`
	CreatedAt string `json:"created_at"`
	LastUsed  string `json:"last_used,omitempty"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// POST /v1/users/me/api_keys — generate new API key
func handleCreateAPIKey(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return apiUnauthorized(c)
	}

	var req createAPIKeyRequest
	if err := c.BodyParser(&req); err != nil {
		return apiBadRequest(c, "invalid JSON body")
	}

	// Generate tk_ prefix + 32 random bytes hex
	randomBytes, err := generateAPIKeyBytes()
	if err != nil {
		return apiInternalError(c, "failed to generate API key")
	}

	fullKey := apiKeyPrefix + hex.EncodeToString(randomBytes)
	prefix := fullKey[:10] // "tk_" + first 6 hex chars
	keyHash := hashAPIKey(fullKey)

	entry := APIKeyEntry{
		ID:        generateShortID(),
		Prefix:    prefix,
		KeyHash:   keyHash,
		Name:      strings.TrimSpace(req.Name),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Store in Clerk user metadata
	if err := addAPIKeyToClerk(userID, &entry); err != nil {
		log.Printf("[apikey] failed to store API key in Clerk: %v", err)
		return apiInternalError(c, "failed to store API key")
	}

	return c.Status(fiber.StatusCreated).JSON(createAPIKeyResponse{
		ID:     entry.ID,
		Key:    fullKey,
		Name:   entry.Name,
		Prefix: prefix,
	})
}

// GET /v1/users/me/api_keys — list user's keys (masked)
func handleListAPIKeys(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return apiUnauthorized(c)
	}

	keys, err := getAPIKeysFromClerk(userID)
	if err != nil {
		log.Printf("[apikey] failed to list API keys from Clerk: %v", err)
		return apiInternalError(c, "failed to retrieve API keys")
	}

	result := make([]listAPIKeyResponse, 0, len(keys))
	for _, k := range keys {
		result = append(result, listAPIKeyResponse{
			ID:        k.ID,
			Name:      k.Name,
			Prefix:    maskKey(k.Prefix),
			CreatedAt: k.CreatedAt,
			LastUsed:  k.LastUsed,
		})
	}

	return c.JSON(result)
}

// DELETE /v1/users/me/api_keys/:key_id — revoke an API key
func handleRevokeAPIKey(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return apiUnauthorized(c)
	}

	keyID := c.Params("key_id")
	if keyID == "" {
		return apiBadRequest(c, "key_id is required")
	}

	if err := removeAPIKeyFromClerk(userID, keyID); err != nil {
		log.Printf("[apikey] failed to revoke API key from Clerk: %v", err)
		return apiInternalError(c, "failed to revoke API key")
	}

	// Invalidate Redis cache for all prefixes associated with this user
	if err := invalidateAPIKeyCacheForUser(userID); err != nil {
		log.Printf("[apikey] failed to invalidate cache for user %s: %v", userID, err)
	}

	return c.JSON(fiber.Map{"status": "revoked"})
}

// ---------------------------------------------------------------------------
// LookupUserByAPIKey — resolves an API key to a user ID
// ---------------------------------------------------------------------------

// LookupUserByAPIKey resolves a full API key string to the owning Clerk user ID.
// Uses Redis cache (1h TTL) with Clerk API fallback.
func LookupUserByAPIKey(key string) (string, error) {
	if !strings.HasPrefix(key, apiKeyPrefix) {
		return "", fmt.Errorf("key does not have tk_ prefix")
	}

	// Strip prefix for lookup
	strippedKey := strings.TrimPrefix(key, apiKeyPrefix)
	if len(strippedKey) == 0 {
		return "", fmt.Errorf("empty key")
	}

	// Compute key hash for matching
	keyHash := hashAPIKey(apiKeyPrefix + strippedKey)
	prefix := key[:10] // tk_ + first 6 hex chars

	// Try Redis cache first
	cacheKey := apiKeyRedisPrefix + prefix
	rdb := tredis.GetClient()
	if rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if cachedUserID, err := rdb.Get(ctx, cacheKey).Result(); err == nil && cachedUserID != "" {
			return cachedUserID, nil
		}
	}

	// Cache miss — query Clerk API
	userID, err := lookupUserByKeyInClerk(prefix, keyHash)
	if err != nil {
		return "", fmt.Errorf("clerk lookup failed: %w", err)
	}
	if userID == "" {
		return "", fmt.Errorf("api key not found")
	}

	// Cache back (fail-open)
	if rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := rdb.Set(ctx, cacheKey, userID, apiKeyCacheTTL).Err(); err != nil {
			log.Printf("[apikey] failed to cache API key lookup: %v", err)
		}
	}

	return userID, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func generateAPIKeyBytes() ([]byte, error) {
	b := make([]byte, apiKeyRandomBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func hashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func generateShortID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func maskKey(prefix string) string {
	if len(prefix) < 10 {
		return prefix + "\u2026"
	}
	return prefix + "\u2026"
}

// ---------------------------------------------------------------------------
// Clerk API integration
// ---------------------------------------------------------------------------

func clerkBaseURL() string {
	return os.Getenv("CLERK_API_BASE_URL")
}

func clerkSecretKey() string {
	return os.Getenv("CLERK_SECRET_KEY")
}

func clerkHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// getAPIKeysFromClerk retrieves the api_keys array from Clerk user metadata.
func getAPIKeysFromClerk(userID string) ([]APIKeyEntry, error) {
	baseURL := clerkBaseURL()
	if baseURL == "" {
		baseURL = "https://api.clerk.com"
	}
	secret := clerkSecretKey()
	if secret == "" {
		return nil, fmt.Errorf("CLERK_SECRET_KEY not set")
	}

	url := fmt.Sprintf("%s/v1/users/%s", strings.TrimRight(baseURL, "/"), userID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := clerkHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clerk API returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		UnsafeMetadata struct {
			APIKeys json.RawMessage `json:"api_keys"`
		} `json:"unsafe_metadata"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result.UnsafeMetadata.APIKeys == nil {
		return []APIKeyEntry{}, nil
	}

	var keys []APIKeyEntry
	if err := json.Unmarshal(result.UnsafeMetadata.APIKeys, &keys); err != nil {
		return nil, err
	}
	if keys == nil {
		return []APIKeyEntry{}, nil
	}
	return keys, nil
}

// addAPIKeyToClerk appends an API key entry to Clerk user metadata.
func addAPIKeyToClerk(userID string, entry *APIKeyEntry) error {
	existing, err := getAPIKeysFromClerk(userID)
	if err != nil {
		return fmt.Errorf("failed to fetch existing keys: %w", err)
	}

	existing = append(existing, *entry)

	return updateClerkAPIKeys(userID, existing)
}

// removeAPIKeyFromClerk removes an API key entry from Clerk user metadata by ID.
func removeAPIKeyFromClerk(userID, keyID string) error {
	existing, err := getAPIKeysFromClerk(userID)
	if err != nil {
		return fmt.Errorf("failed to fetch existing keys: %w", err)
	}

	var filtered []APIKeyEntry
	found := false
	for _, k := range existing {
		if k.ID == keyID {
			found = true
			continue
		}
		filtered = append(filtered, k)
	}

	if !found {
		return fmt.Errorf("key %s not found for user %s", keyID, userID)
	}

	return updateClerkAPIKeys(userID, filtered)
}

// updateClerkAPIKeys writes the api_keys array to Clerk user metadata.
func updateClerkAPIKeys(userID string, keys []APIKeyEntry) error {
	baseURL := clerkBaseURL()
	if baseURL == "" {
		baseURL = "https://api.clerk.com"
	}
	secret := clerkSecretKey()
	if secret == "" {
		return fmt.Errorf("CLERK_SECRET_KEY not set")
	}

	body := map[string]interface{}{
		"unsafe_metadata": map[string]interface{}{
			"api_keys": keys,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/v1/users/%s", strings.TrimRight(baseURL, "/"), userID)
	req, err := http.NewRequest("PATCH", url, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := clerkHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("clerk API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// lookupUserByKeyInClerk searches all users to find one with a matching API key.
// This is the fallback when Redis cache is empty.
func lookupUserByKeyInClerk(prefix, keyHash string) (string, error) {
	baseURL := clerkBaseURL()
	if baseURL == "" {
		baseURL = "https://api.clerk.com"
	}
	secret := clerkSecretKey()
	if secret == "" {
		return "", fmt.Errorf("CLERK_SECRET_KEY not set")
	}

	const limit = 100
	offset := 0

	for {
		url := fmt.Sprintf("%s/v1/users?limit=%d&offset=%d&order_by=created_at",
			strings.TrimRight(baseURL, "/"), limit, offset)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("Content-Type", "application/json")

		resp, err := clerkHTTPClient().Do(req)
		if err != nil {
			return "", err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", err
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("clerk API returned %d", resp.StatusCode)
		}

		var users []struct {
			ID             string `json:"id"`
			UnsafeMetadata struct {
				APIKeys json.RawMessage `json:"api_keys"`
			} `json:"unsafe_metadata"`
		}
		if err := json.Unmarshal(body, &users); err != nil {
			return "", err
		}

		if len(users) == 0 {
			break
		}

		for _, user := range users {
			if user.UnsafeMetadata.APIKeys == nil {
				continue
			}
			var keys []APIKeyEntry
			if err := json.Unmarshal(user.UnsafeMetadata.APIKeys, &keys); err != nil {
				continue
			}
			for _, k := range keys {
				if k.Prefix == prefix && k.KeyHash == keyHash {
					return user.ID, nil
				}
			}
		}

		if len(users) < limit {
			break
		}
		offset += limit
	}

	return "", nil
}

// invalidateAPIKeyCacheForUser clears Redis cache entries for a user's API keys.
func invalidateAPIKeyCacheForUser(userID string) error {
	rdb := tredis.GetClient()
	if rdb == nil {
		return nil
	}

	keys, err := getAPIKeysFromClerk(userID)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, k := range keys {
		cacheKey := apiKeyRedisPrefix + k.Prefix
		rdb.Del(ctx, cacheKey)
	}

	return nil
}
