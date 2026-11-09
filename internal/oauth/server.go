package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strings"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Local error helpers (avoid import cycle with httpapi)
// ---------------------------------------------------------------------------

func writeOAuthError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{"error": fiber.Map{"code": code, "message": message}})
}

func oauthBadRequest(c *fiber.Ctx, message string) error {
	return writeOAuthError(c, fiber.StatusBadRequest, "invalid_request", message)
}

func oauthUnauthorized(c *fiber.Ctx) error {
	return writeOAuthError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or expired token")
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	accessTokenLength  = 32 // bytes → 64 hex chars
	refreshTokenLength = 32
	clientSecretLength = 32
	deviceCodeLength   = 32
	userCodeCharset    = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

	accessTokenTTL     = 7 * 24 * time.Hour
	refreshTokenTTL    = 90 * 24 * time.Hour
	authCodeTTL        = 10 * time.Minute
	deviceCodeTTL      = 15 * time.Minute
	devicePollInterval = 5
)

var validScopes = map[string]bool{
	"profile": true, "history:read": true, "history:write": true,
	"watchlist:read": true, "watchlist:write": true,
	"ratings:read": true, "ratings:write": true,
	"lists:read": true, "lists:write": true,
	"social:read": true, "social:write": true,
	"webhooks:manage": true, "offline_access": true,
}

var defaultScopes = []string{"profile"}

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "POST", Path: "/oauth/applications", Summary: "Register a new OAuth2 application", Tags: []string{"OAuth"}, Auth: "required", StatusCodes: map[int]string{201: "Client created", 400: "Invalid request"}},
		{Method: "GET", Path: "/oauth/authorize", Summary: "OAuth2 authorization endpoint (authorization code + PKCE)", Tags: []string{"OAuth"}, Auth: "required", StatusCodes: map[int]string{302: "Redirect with authorization code", 400: "Invalid parameters"}},
		{Method: "POST", Path: "/oauth/token", Summary: "OAuth2 token endpoint", Tags: []string{"OAuth"}, Auth: "none", StatusCodes: map[int]string{200: "Token response", 400: "Invalid grant"}},
		{Method: "POST", Path: "/oauth/revoke", Summary: "Revoke an OAuth2 token", Tags: []string{"OAuth"}, Auth: "none", StatusCodes: map[int]string{200: "Token revoked", 400: "Invalid request"}},
		{Method: "POST", Path: "/oauth/device/code", Summary: "Device flow — generate user & device codes", Tags: []string{"OAuth"}, Auth: "none", StatusCodes: map[int]string{200: "Device code response", 400: "Invalid request"}},
		{Method: "POST", Path: "/oauth/device/token", Summary: "Device flow — poll for access token", Tags: []string{"OAuth"}, Auth: "none", StatusCodes: map[int]string{200: "Token response", 400: "authorization_pending"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires all OAuth route handlers at the app level.
func RegisterRoutes(app *fiber.App) {
	oauthGroup := app.Group("/oauth")
	oauthGroup.Post("/applications", handleCreateApplication)
	oauthGroup.Get("/authorize", handleAuthorize)
	oauthGroup.Post("/token", handleToken)
	oauthGroup.Post("/revoke", handleRevoke)

	device := oauthGroup.Group("/device")
	device.Post("/code", handleDeviceCode)
	device.Post("/token", handleDeviceToken)
	device.Post("/verify", handleDeviceVerify)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func generateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func generateToken(length int) (string, error) {
	b, err := generateRandomBytes(length)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateUserCode() (string, error) {
	b, err := generateRandomBytes(4)
	if err != nil {
		return "", err
	}
	code := make([]byte, 9)
	for i := 0; i < 4; i++ {
		code[i] = userCodeCharset[int(b[i])%len(userCodeCharset)]
	}
	code[4] = '-'
	for i := 0; i < 4; i++ {
		code[5+i] = userCodeCharset[int(b[i])%len(userCodeCharset)]
	}
	return string(code), nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func hashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

func verifyPKCE(codeVerifier, codeChallenge string) bool {
	if codeChallenge == "" {
		return false
	}
	h := sha256.Sum256([]byte(codeVerifier))
	encoded := base64.RawURLEncoding.EncodeToString(h[:])
	return subtle.ConstantTimeCompare([]byte(encoded), []byte(codeChallenge)) == 1
}

func validateScopes(requested []string) []string {
	if len(requested) == 0 {
		return defaultScopes
	}
	seen := make(map[string]bool)
	var result []string
	for _, s := range requested {
		s = strings.TrimSpace(s)
		if validScopes[s] && !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	if len(result) == 0 {
		return defaultScopes
	}
	return result
}

func scopeString(scopes []string) string {
	return strings.Join(scopes, " ")
}

func requireAuth(c *fiber.Ctx) (string, error) {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return "", oauthUnauthorized(c)
	}
	return userID, nil
}

// ---------------------------------------------------------------------------
// POST /oauth/applications — Client registration
// ---------------------------------------------------------------------------

type createAppRequest struct {
	Name         string   `json:"name"`
	RedirectURIs []string `json:"redirect_uris"`
}

type createAppResponse struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURIs []string `json:"redirect_uris"`
	CreatedAt    string   `json:"created_at"`
}

func handleCreateApplication(c *fiber.Ctx) error {
	userID, err := requireAuth(c)
	if err != nil {
		return err
	}

	var req createAppRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Name) == "" {
		return oauthBadRequest(c, "name is required")
	}

	secret, err := generateToken(clientSecretLength)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to generate client secret")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	client := database.OAuthClient{
		Name:         strings.TrimSpace(req.Name),
		RedirectURIs: database.StringArray(req.RedirectURIs),
		SecretHash:   hashSecret(secret),
		UserID:       userID,
	}

	if err := db.Create(&client).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create application")
	}

	return c.Status(fiber.StatusCreated).JSON(createAppResponse{
		ID:           client.ID,
		Name:         client.Name,
		ClientID:     client.ID,
		ClientSecret: secret,
		RedirectURIs: req.RedirectURIs,
		CreatedAt:    client.CreatedAt.Format(time.RFC3339),
	})
}

// ---------------------------------------------------------------------------
// GET /oauth/authorize — Authorization code + PKCE
// ---------------------------------------------------------------------------

func handleAuthorize(c *fiber.Ctx) error {
	userID, err := requireAuth(c)
	if err != nil {
		return err
	}

	responseType := c.Query("response_type")
	clientID := c.Query("client_id")
	redirectURI := c.Query("redirect_uri")
	scope := c.Query("scope")
	state := c.Query("state")
	codeChallenge := c.Query("code_challenge")
	codeChallengeMethod := c.Query("code_challenge_method", "S256")

	if responseType != "code" {
		return oauthBadRequest(c, "response_type must be 'code'")
	}
	if clientID == "" {
		return oauthBadRequest(c, "client_id is required")
	}
	if redirectURI == "" {
		return oauthBadRequest(c, "redirect_uri is required")
	}
	if codeChallenge == "" {
		return oauthBadRequest(c, "code_challenge is required (PKCE)")
	}
	if codeChallengeMethod != "S256" {
		return oauthBadRequest(c, "code_challenge_method must be 'S256'")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var client database.OAuthClient
	if err := db.First(&client, "id = ?", clientID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return oauthBadRequest(c, "invalid client_id")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database error")
	}

	validURI := false
	for _, uri := range client.RedirectURIs {
		if uri == redirectURI {
			validURI = true
			break
		}
	}
	if !validURI {
		return oauthBadRequest(c, "redirect_uri not registered for this client")
	}

	scopes := validateScopes(strings.Fields(scope))

	code, err := generateToken(32)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to generate authorization code")
	}

	grant := database.OAuthGrant{
		ClientID:            clientID,
		UserID:              userID,
		CodeHash:            hashToken(code),
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		RedirectURI:         redirectURI,
		Scopes:              database.StringArray(scopes),
		ExpiresAt:           time.Now().Add(authCodeTTL),
	}

	if err := db.Create(&grant).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create grant")
	}

	redirectURL, err := url.Parse(redirectURI)
	if err != nil {
		return oauthBadRequest(c, "invalid redirect_uri")
	}
	q := redirectURL.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	redirectURL.RawQuery = q.Encode()

	return c.Redirect(redirectURL.String(), fiber.StatusFound)
}

// ---------------------------------------------------------------------------
// POST /oauth/token — Token endpoint
// ---------------------------------------------------------------------------

type tokenRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code,omitempty"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
	CodeVerifier string `json:"code_verifier,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope"`
}

func handleToken(c *fiber.Ctx) error {
	var req tokenRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}

	switch req.GrantType {
	case "authorization_code":
		return handleAuthCodeGrant(c, &req)
	case "refresh_token":
		return handleRefreshGrant(c, &req)
	case "client_credentials":
		return handleClientCredentialsGrant(c, &req)
	default:
		return oauthBadRequest(c, "unsupported grant_type")
	}
}

func handleAuthCodeGrant(c *fiber.Ctx, req *tokenRequest) error {
	if req.Code == "" {
		return oauthBadRequest(c, "code is required")
	}
	if req.CodeVerifier == "" {
		return oauthBadRequest(c, "code_verifier is required")
	}
	if req.RedirectURI == "" {
		return oauthBadRequest(c, "redirect_uri is required")
	}
	if req.ClientID == "" {
		return oauthBadRequest(c, "client_id is required")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	codeHash := hashToken(req.Code)
	var grant database.OAuthGrant
	if err := db.Where("code_hash = ?", codeHash).First(&grant).Error; err != nil {
		return oauthBadRequest(c, "invalid_grant: authorization code not found")
	}

	if grant.Used {
		return oauthBadRequest(c, "invalid_grant: authorization code already used")
	}
	if time.Now().After(grant.ExpiresAt) {
		return oauthBadRequest(c, "invalid_grant: authorization code expired")
	}
	if grant.ClientID != req.ClientID {
		return oauthBadRequest(c, "invalid_grant: client_id mismatch")
	}
	if grant.RedirectURI != req.RedirectURI {
		return oauthBadRequest(c, "invalid_grant: redirect_uri mismatch")
	}

	if grant.CodeChallenge != "" {
		if !verifyPKCE(req.CodeVerifier, grant.CodeChallenge) {
			return oauthBadRequest(c, "invalid_grant: code_verifier does not match code_challenge")
		}
	}

	if err := db.Model(&grant).Update("used", true).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update grant")
	}

	return issueTokens(c, db, grant.ClientID, grant.UserID, &grant.ID, []string(grant.Scopes), req.ClientSecret)
}

func handleRefreshGrant(c *fiber.Ctx, req *tokenRequest) error {
	if req.RefreshToken == "" {
		return oauthBadRequest(c, "refresh_token is required")
	}
	if req.ClientID == "" {
		return oauthBadRequest(c, "client_id is required")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	refreshHash := hashToken(req.RefreshToken)
	var oldToken database.OAuthToken
	if err := db.Where("refresh_token_hash = ?", refreshHash).First(&oldToken).Error; err != nil {
		return oauthBadRequest(c, "invalid_grant: refresh token not found")
	}

	if oldToken.Revoked {
		if oldToken.RefreshTokenFamily != "" {
			db.Model(&database.OAuthToken{}).
				Where("refresh_token_family = ?", oldToken.RefreshTokenFamily).
				Update("revoked", true)
		}
		return oauthBadRequest(c, "invalid_grant: refresh token has been revoked (reuse detected)")
	}

	if oldToken.RefreshExpiresAt != nil && time.Now().After(*oldToken.RefreshExpiresAt) {
		return oauthBadRequest(c, "invalid_grant: refresh token expired")
	}

	if oldToken.ClientID != req.ClientID {
		return oauthBadRequest(c, "invalid_grant: client_id mismatch")
	}

	if err := db.Model(&oldToken).Update("revoked", true).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to revoke old token")
	}

	// NOTE: the previous token's refresh-token family is not propagated to the
	// new token — issueTokens has no family parameter — so `RefreshTokenFamily`
	// is dropped on rotation and family-wide revocation on reuse cannot work
	// yet. Left as-is here (a behaviour change, not a lint fix).
	return issueTokens(c, db, oldToken.ClientID, oldToken.UserID, oldToken.GrantID, []string(oldToken.Scopes), req.ClientSecret)
}

func handleClientCredentialsGrant(c *fiber.Ctx, req *tokenRequest) error {
	if req.ClientID == "" || req.ClientSecret == "" {
		return oauthBadRequest(c, "client_id and client_secret are required for client_credentials")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var client database.OAuthClient
	if err := db.First(&client, "id = ?", req.ClientID).Error; err != nil {
		return oauthBadRequest(c, "invalid_client: client not found")
	}

	if subtle.ConstantTimeCompare([]byte(client.SecretHash), []byte(hashSecret(req.ClientSecret))) != 1 {
		return oauthBadRequest(c, "invalid_client: invalid client_secret")
	}

	scopes := []string{"profile"}

	return issueTokens(c, db, client.ID, client.UserID, nil, scopes, req.ClientSecret)
}

func issueTokens(c *fiber.Ctx, db *gorm.DB, clientID, userID string, grantID *string, scopes []string, _ string) error {
	family := ""
	if grantID != nil {
		family = *grantID
	}

	accessToken, err := generateToken(accessTokenLength)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to generate access token")
	}

	refreshToken, err := generateToken(refreshTokenLength)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to generate refresh token")
	}

	now := time.Now()
	accessExpires := now.Add(accessTokenTTL)
	refreshExpires := now.Add(refreshTokenTTL)

	token := database.OAuthToken{
		ClientID:           clientID,
		UserID:             userID,
		GrantID:            grantID,
		TokenHash:          hashToken(accessToken),
		RefreshTokenHash:   hashToken(refreshToken),
		RefreshTokenFamily: family,
		Scopes:             database.StringArray(scopes),
		ExpiresAt:          accessExpires,
		RefreshExpiresAt:   &refreshExpires,
	}

	if err := db.Create(&token).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create token")
	}

	return c.JSON(tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(accessTokenTTL.Seconds()),
		RefreshToken: refreshToken,
		Scope:        scopeString(scopes),
	})
}

// ---------------------------------------------------------------------------
// POST /oauth/revoke — Revoke token
// ---------------------------------------------------------------------------

type revokeRequest struct {
	Token         string `json:"token"`
	TokenTypeHint string `json:"token_type_hint,omitempty"`
}

func handleRevoke(c *fiber.Ctx) error {
	var req revokeRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}
	if req.Token == "" {
		return oauthBadRequest(c, "token is required")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	tokenHash := hashToken(req.Token)

	result := db.Model(&database.OAuthToken{}).
		Where("(token_hash = ? OR refresh_token_hash = ?) AND revoked = false", tokenHash, tokenHash).
		Update("revoked", true)

	if result.Error != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to revoke token")
	}

	return c.JSON(fiber.Map{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Device flow
// ---------------------------------------------------------------------------

type deviceCodeRequest struct {
	ClientID string `json:"client_id"`
	Scope    string `json:"scope,omitempty"`
}

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

var deviceGrantStore = make(map[string]*deviceGrant)

type deviceGrant struct {
	ClientID   string
	UserID     string
	Scopes     []string
	UserCode   string
	ExpiresAt  time.Time
	Authorized bool
}

func handleDeviceCode(c *fiber.Ctx) error {
	var req deviceCodeRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}
	if req.ClientID == "" {
		return oauthBadRequest(c, "client_id is required")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var client database.OAuthClient
	if err := db.First(&client, "id = ?", req.ClientID).Error; err != nil {
		return oauthBadRequest(c, "invalid client_id")
	}

	deviceCode, err := generateToken(deviceCodeLength)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to generate device code")
	}

	userCode, err := generateUserCode()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to generate user code")
	}

	scopes := validateScopes(strings.Fields(req.Scope))

	grant := &deviceGrant{
		ClientID:   req.ClientID,
		Scopes:     scopes,
		UserCode:   userCode,
		ExpiresAt:  time.Now().Add(deviceCodeTTL),
		Authorized: false,
	}

	deviceGrantStore[deviceCode] = grant

	return c.JSON(deviceCodeResponse{
		DeviceCode:      deviceCode,
		UserCode:        userCode,
		VerificationURI: "https://haaku.tv/activate",
		ExpiresIn:       int(deviceCodeTTL.Seconds()),
		Interval:        devicePollInterval,
	})
}

type deviceTokenRequest struct {
	GrantType  string `json:"grant_type"`
	DeviceCode string `json:"device_code"`
	ClientID   string `json:"client_id"`
}

func handleDeviceToken(c *fiber.Ctx) error {
	var req deviceTokenRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}

	if req.GrantType != "urn:ietf:params:oauth:grant-type:device_code" {
		return oauthBadRequest(c, "grant_type must be 'urn:ietf:params:oauth:grant-type:device_code'")
	}
	if req.DeviceCode == "" {
		return oauthBadRequest(c, "device_code is required")
	}
	if req.ClientID == "" {
		return oauthBadRequest(c, "client_id is required")
	}

	grant, ok := deviceGrantStore[req.DeviceCode]
	if !ok {
		return oauthBadRequest(c, "invalid_grant: device code not found")
	}

	if time.Now().After(grant.ExpiresAt) {
		delete(deviceGrantStore, req.DeviceCode)
		return oauthBadRequest(c, "expired_token: device code has expired")
	}

	if grant.ClientID != req.ClientID {
		return oauthBadRequest(c, "invalid_grant: client_id mismatch")
	}

	if !grant.Authorized {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":             "authorization_pending",
			"error_description": "The user has not yet authorized the device.",
		})
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	delete(deviceGrantStore, req.DeviceCode)

	return issueTokens(c, db, grant.ClientID, grant.UserID, nil, grant.Scopes, "")
}

// handleDeviceVerify allows a user to authorize a device by entering the user_code.
// POST /oauth/device/verify (requires auth)
func handleDeviceVerify(c *fiber.Ctx) error {
	userID, err := requireAuth(c)
	if err != nil {
		return err
	}

	var req struct {
		UserCode string `json:"user_code"`
	}
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}
	if req.UserCode == "" {
		return oauthBadRequest(c, "user_code is required")
	}

	for _, grant := range deviceGrantStore {
		if grant.UserCode == req.UserCode {
			if time.Now().After(grant.ExpiresAt) {
				return oauthBadRequest(c, "device code has expired")
			}
			grant.UserID = userID
			grant.Authorized = true
			return c.JSON(fiber.Map{"status": "authorized"})
		}
	}

	return oauthBadRequest(c, "invalid user_code")
}
