package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"trakt-api/internal/config"
	"trakt-api/internal/database"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ClerkClient wraps the Clerk API for JWT verification, user fetching, and webhooks.
type ClerkClient struct {
	BaseURL       string
	SecretKey     string
	WebhookSecret string
	httpClient    *http.Client
	jwks          *jwksCache
}

// ClerkUser is the Clerk user profile as returned by the Clerk API.
type ClerkUser struct {
	ID                    string         `json:"id"`
	Username              string         `json:"username"`
	FirstName             string         `json:"first_name"`
	LastName              string         `json:"last_name"`
	ImageURL              string         `json:"image_url"`
	EmailAddresses        []clerkEmail   `json:"email_addresses"`
	PrimaryEmailAddressID string         `json:"primary_email_address_id"`
	CreatedAt             int64          `json:"created_at"`
	UpdatedAt             int64          `json:"updated_at"`
	PublicMetadata        map[string]any `json:"public_metadata"`
	UnsafeMetadata        map[string]any `json:"unsafe_metadata"`
}

type clerkEmail struct {
	ID           string `json:"id"`
	EmailAddress string `json:"email_address"`
}

// jwksCache holds JWKS keys in memory with a TTL.
type jwksCache struct {
	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
	ttl       time.Duration
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// ---------------------------------------------------------------------------
// Package-level singleton
// ---------------------------------------------------------------------------

var clerkClient *ClerkClient

// identityRefreshInterval bounds how often the lazy on-auth refresh calls
// the Clerk Backend API per user. Real-time updates come from the Clerk
// webhook; this is the zero-config fallback that keeps the identity mirror
// fresh even when the webhook isn't configured.
const identityRefreshInterval = time.Hour

// InitClerk initializes the Clerk client singleton. Call once at startup.
func InitClerk(cfg *config.Config) {
	if cfg.ClerkSecret == "" {
		slog.Warn("Clerk secret key not set — auth will reject all tokens")
	}
	webhookSecret := cfg.ClerkWebhookSecret
	if webhookSecret == "" {
		// Dev convenience: clerk-emulator signs webhooks with the same
		// secret as the API. Production MUST set CLERK_WEBHOOK_SECRET.
		webhookSecret = cfg.ClerkSecret
	}
	clerkClient = &ClerkClient{
		BaseURL:       strings.TrimRight(cfg.ClerkBaseURL, "/"),
		SecretKey:     cfg.ClerkSecret,
		WebhookSecret: webhookSecret,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		jwks: &jwksCache{
			keys: make(map[string]*rsa.PublicKey),
			ttl:  5 * time.Minute,
		},
	}
}

// GetClerk returns the initialized Clerk client. May be nil if InitClerk wasn't called.
func GetClerk() *ClerkClient {
	return clerkClient
}

// ---------------------------------------------------------------------------
// JWKS
// ---------------------------------------------------------------------------

// fetchJWKS fetches the JWKS endpoint and caches the keys.
func (c *ClerkClient) fetchJWKS() error {
	url := c.BaseURL + "/jwks"
	slog.Debug("fetching JWKS", "url", url)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("jwks fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks fetch returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("jwks read body failed: %w", err)
	}

	var jwksResp jwksResponse
	if err := json.Unmarshal(body, &jwksResp); err != nil {
		return fmt.Errorf("jwks parse failed: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey)
	for _, k := range jwksResp.Keys {
		pubKey, err := jwkToRSA(k)
		if err != nil {
			slog.Warn("skipping JWK key", "kid", k.Kid, "error", err)
			continue
		}
		newKeys[k.Kid] = pubKey
	}

	c.jwks.mu.Lock()
	c.jwks.keys = newKeys
	c.jwks.fetchedAt = time.Now()
	c.jwks.mu.Unlock()

	slog.Debug("JWKS refreshed", "keys", len(newKeys))
	return nil
}

// getJWK returns the RSA public key for the given kid, refreshing the cache if needed.
func (c *ClerkClient) getJWK(kid string) (*rsa.PublicKey, error) {
	c.jwks.mu.RLock()
	key, ok := c.jwks.keys[kid]
	stale := time.Since(c.jwks.fetchedAt) > c.jwks.ttl
	c.jwks.mu.RUnlock()

	if ok && !stale {
		return key, nil
	}

	// Refresh and retry
	if err := c.fetchJWKS(); err != nil {
		// If we had a stale key, still return it on refresh failure
		if key != nil {
			slog.Warn("JWKS refresh failed, using stale key", "kid", kid, "error", err)
			return key, nil
		}
		return nil, fmt.Errorf("jwks fetch: %w", err)
	}

	c.jwks.mu.RLock()
	key, ok = c.jwks.keys[kid]
	c.jwks.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown kid: %s", kid)
	}
	return key, nil
}

// jwkToRSA converts a JWK key to an RSA public key.
func jwkToRSA(k jwkKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes).Int64()

	return &rsa.PublicKey{N: n, E: int(e)}, nil
}

// ---------------------------------------------------------------------------
// JWT verification
// ---------------------------------------------------------------------------

// VerifySessionToken verifies a Clerk session JWT and returns the user ID (sub claim).
// On success, also performs self-healing: if the user doesn't exist in our DB,
// it fetches from Clerk and upserts.
func (c *ClerkClient) VerifySessionToken(token string) (string, error) {
	if c.SecretKey == "" {
		return "", fmt.Errorf("clerk not configured (missing secret key)")
	}

	// Parse without verification to extract the kid
	parser := jwt.NewParser()
	unverified, _, err := parser.ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		return "", fmt.Errorf("jwt parse: %w", err)
	}

	kid, ok := unverified.Header["kid"].(string)
	if !ok {
		return "", fmt.Errorf("jwt missing kid header")
	}

	// Get the JWK
	pubKey, err := c.getJWK(kid)
	if err != nil {
		return "", fmt.Errorf("jwks: %w", err)
	}

	// Verify the token with the key
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return pubKey, nil
	})
	if err != nil {
		return "", fmt.Errorf("jwt verify: %w", err)
	}

	if !parsed.Valid {
		return "", fmt.Errorf("jwt invalid")
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return "", fmt.Errorf("unexpected claims type")
	}

	userID, err := claims.GetSubject()
	if err != nil {
		return "", fmt.Errorf("jwt missing sub claim")
	}

	// Self-healing: ensure user exists in our DB
	c.ensureUserInDB(userID)

	return userID, nil
}

// ensureUserInDB checks if a user exists locally; if not, fetches from Clerk
// and upserts. Existing users get a lazy identity refresh (name/username/
// avatar are Clerk-managed) at most once per identityRefreshInterval.
func (c *ClerkClient) ensureUserInDB(userID string) {
	db, err := database.GetDB()
	if err != nil {
		slog.Warn("self-healing: database unavailable", "user_id", userID, "error", err)
		return
	}

	var existing database.User
	result := db.Where("id = ?", userID).First(&existing)
	if result.Error != nil {
		if result.Error != gorm.ErrRecordNotFound {
			slog.Warn("self-healing: db lookup failed", "user_id", userID, "error", result.Error)
			return
		}
		// Never seen before — fetch from Clerk and create.
		cu, err := c.GetUser(userID)
		if err != nil {
			slog.Warn("self-healing: Clerk fetch failed", "user_id", userID, "error", err)
			return
		}
		if err := upsertUserFromClerk(db, cu); err != nil {
			slog.Warn("self-healing: upsert failed", "user_id", userID, "error", err)
			return
		}
		slog.Info("self-healing: user synced from Clerk", "user_id", userID)
		return
	}

	// Existing user — refresh identity when stale (or never synced: NULL
	// ClerkSyncedAt covers every pre-existing row after deploy).
	lastSync := existing.ClerkSyncedAt
	if lastSync != nil && time.Since(*lastSync) < identityRefreshInterval {
		return
	}
	cu, err := c.GetUser(userID)
	if err != nil {
		slog.Warn("self-healing: Clerk refresh failed", "user_id", userID, "error", err)
		return
	}
	if err := upsertUserFromClerk(db, cu); err != nil {
		slog.Warn("self-healing: refresh upsert failed", "user_id", userID, "error", err)
	}
}

// ---------------------------------------------------------------------------
// Clerk API
// ---------------------------------------------------------------------------

// GetUser fetches a user profile from the Clerk API.
func (c *ClerkClient) GetUser(userID string) (*ClerkUser, error) {
	url := c.BaseURL + "/v1/users/" + userID
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.SecretKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("user not found: %s", userID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("clerk api error %d: %s", resp.StatusCode, string(body))
	}

	var user ClerkUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &user, nil
}

// UpdateUserMetadata writes to Clerk's unsafe_metadata for the given user.
func (c *ClerkClient) UpdateUserMetadata(userID string, metadata map[string]any) error {
	url := c.BaseURL + "/v1/users/" + userID
	body := map[string]any{
		"unsafe_metadata": metadata,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPatch, url, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.SecretKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("clerk api error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// DeleteUser permanently deletes a Clerk user via the Backend API. Called
// when a user deletes their account in-app — the user.deleted webhook then
// wipes the local mirror (and is a no-op if we already wiped it).
func (c *ClerkClient) DeleteUser(userID string) error {
	url := c.BaseURL + "/v1/users/" + userID
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.SecretKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("clerk api error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ExtractPrimaryEmail returns the primary email address from a ClerkUser, or empty string.
func (cu *ClerkUser) ExtractPrimaryEmail() string {
	if cu.PrimaryEmailAddressID == "" {
		if len(cu.EmailAddresses) > 0 {
			return cu.EmailAddresses[0].EmailAddress
		}
		return ""
	}
	for _, e := range cu.EmailAddresses {
		if e.ID == cu.PrimaryEmailAddressID {
			return e.EmailAddress
		}
	}
	return ""
}

// DisplayName returns the best available display name (first+last or username or empty).
func (cu *ClerkUser) DisplayName() string {
	if cu.FirstName != "" || cu.LastName != "" {
		return strings.TrimSpace(cu.FirstName + " " + cu.LastName)
	}
	return cu.Username
}

// ---------------------------------------------------------------------------
// Helper functions (used by both clerk.go and webhooks.go)
// ---------------------------------------------------------------------------

// upsertUserFromClerk creates or updates a local User record from a ClerkUser.
// Identity fields (username, email, name, avatar) are mirrors of Clerk and
// must never be edited through the app; bio/privacy/role/plan are ours.
func upsertUserFromClerk(db *gorm.DB, cu *ClerkUser) error {
	user := database.User{
		ID:            cu.ID,
		Username:      cu.Username,
		Email:         cu.ExtractPrimaryEmail(),
		Name:          cu.DisplayName(),
		AvatarURL:     cu.ImageURL,
		Status:        "active", // resurrection-safe: re-created accounts reactivate
		ClerkSyncedAt: timePtr(time.Now()),
	}
	// If username is empty, derive from email or use ID
	if user.Username == "" {
		if user.Email != "" {
			parts := strings.SplitN(user.Email, "@", 2)
			user.Username = parts[0]
		} else {
			user.Username = cu.ID
		}
	}
	return db.Where(database.User{ID: cu.ID}).Assign(user).FirstOrCreate(&user).Error
}

// timePtr returns a pointer to t.
func timePtr(t time.Time) *time.Time { return &t }
