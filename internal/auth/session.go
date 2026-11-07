package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/secret"

	"gorm.io/gorm"
)

// Opaque server-side sessions for local accounts (issue #53 §2).
//
// A session is a row, not a signed token. The token handed to the client is 32
// random bytes with a "hs_" prefix; what the database stores is an HMAC-SHA256
// blind index of it under a key derived from the existing root key
// (HAAKU_SECRET_KEY) with secret.DeriveKey under the versioned purpose
// "haaku/local-session-token/v1". No second key, no second crypto scheme: the
// same HKDF root that protects at-rest ciphertext protects session lookup,
// domain-separated so the two can never be cross-used.
//
// Self-contained tokens were rejected deliberately: banning a user has to kill
// the sessions they are already holding, and only a server-side row can be
// revoked. See docs/architecture/local-auth.md.

const (
	// SessionTokenPrefix marks a bearer token as a local session token, so the
	// auth middleware can route it without trying Clerk first. It is also the
	// thing that cannot exist in clerk mode.
	SessionTokenPrefix = "hs_"

	// sessionTokenPurpose domain-separates the session blind-index key from the
	// at-rest data-encryption key derived from the same root. Never reuse it.
	sessionTokenPurpose = "haaku/local-session-token/v1"

	sessionTokenBytes = 32
	sessionIDBytes    = 16

	// SessionTTL is how long a login lasts. Self-hosted instances are personal
	// boxes, so a long session beats re-authenticating every day.
	SessionTTL = 30 * 24 * time.Hour

	// sessionTouchInterval is how often a session's last_seen_at is refreshed.
	// Every request would write a row per request on a hot path; once every
	// few minutes is enough for an admin panel to show "last active".
	sessionTouchInterval = 5 * time.Minute
)

// ErrSessionToken is the single error for every reason a session token is not
// usable (unknown, revoked, expired, or belonging to a non-active account).
// Callers must not distinguish them to the client.
var ErrSessionToken = errors.New("auth: invalid session token")

// localTokenKey derives (and caches nothing of) the HMAC key used to blind
// session and one-time tokens before they reach the database.
func localTokenKey() ([]byte, error) {
	root, err := secret.RootKeyFromEnv()
	if err != nil {
		return nil, err
	}
	return secret.DeriveKey(root, sessionTokenPurpose, 32)
}

// localTokenHash returns the hex HMAC-SHA256 blind index of a raw token. The
// HMAC key is derived from the root key, so a database dump without
// HAAKU_SECRET_KEY cannot be turned back into usable tokens.
func localTokenHash(token string) (string, error) {
	key, err := localTokenKey()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// newRandomToken returns prefix + base64url(32 random bytes).
func newRandomToken(prefix string, n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: read random token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// newSessionID returns a random opaque row id.
func newSessionID() (string, error) { return newRandomToken("", sessionIDBytes) }

// CreateSession issues a session for userID and returns the raw token. Only
// this return value can be used by the client; the stored hash cannot.
func CreateSession(db *gorm.DB, userID, ip, userAgent string) (token string, sess *database.LocalSession, err error) {
	token, err = newRandomToken(SessionTokenPrefix, sessionTokenBytes)
	if err != nil {
		return "", nil, err
	}
	hash, err := localTokenHash(token)
	if err != nil {
		return "", nil, err
	}
	id, err := newSessionID()
	if err != nil {
		return "", nil, err
	}
	now := time.Now().UTC()
	sess = &database.LocalSession{
		ID:         id,
		UserID:     userID,
		TokenHash:  hash,
		ExpiresAt:  now.Add(SessionTTL),
		LastSeenAt: now,
		IP:         ip,
		UserAgent:  truncate(userAgent, 512),
		CreatedAt:  now,
	}
	if err := db.Create(sess).Error; err != nil {
		return "", nil, fmt.Errorf("auth: store session: %w", err)
	}
	return token, sess, nil
}

// ResolveSessionToken maps a raw session token to its user id, enforcing
// revocation, expiry and account status. It is the read path the auth
// middleware uses; it returns ErrSessionToken for every failure.
func ResolveSessionToken(db *gorm.DB, token string) (string, error) {
	hash, err := localTokenHash(token)
	if err != nil {
		return "", ErrSessionToken
	}
	var sess database.LocalSession
	if err := db.Where("token_hash = ?", hash).First(&sess).Error; err != nil {
		return "", ErrSessionToken
	}
	now := time.Now().UTC()
	if sess.RevokedAt != nil || !now.Before(sess.ExpiresAt) {
		return "", ErrSessionToken
	}
	var user database.User
	if err := db.Select("id", "status").First(&user, "id = ?", sess.UserID).Error; err != nil {
		return "", ErrSessionToken
	}
	// A pending or banned account cannot use a session it already holds. This
	// is the second half of "ban must lock the door behind them": the ban
	// handler revokes the rows, and this check catches anything already in
	// flight or revoked by a different path.
	if user.Status != database.UserStatusActive {
		return "", ErrSessionToken
	}
	touchSession(db, &sess, user.ID, now)
	return user.ID, nil
}

// touchSession refreshes last_seen_at (throttled) so the admin panel's
// last_active_at is meaningful without a write on every request. Failures are
// ignored: a stale timestamp must never fail a request.
func touchSession(db *gorm.DB, sess *database.LocalSession, userID string, now time.Time) {
	if now.Sub(sess.LastSeenAt) < sessionTouchInterval {
		return
	}
	_ = db.Model(&database.LocalSession{}).Where("id = ?", sess.ID).
		Update("last_seen_at", now).Error
	_ = db.Model(&database.User{}).Where("id = ?", userID).
		Update("last_active_at", now).Error
}

// RevokeSessionByToken revokes the session a raw token names. A token that is
// unknown or already revoked is not an error: the revoke is idempotent (the
// logout route is separately gated by AuthRequired, so a dead token cannot
// reach it).
func RevokeSessionByToken(db *gorm.DB, token string) error {
	hash, err := localTokenHash(token)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return db.Model(&database.LocalSession{}).
		Where("token_hash = ? AND revoked_at IS NULL", hash).
		Update("revoked_at", now).Error
}

// RevokeSessionsForUser revokes every live session a user holds. This is what a
// ban, a lock and a completed password reset all call.
func RevokeSessionsForUser(db *gorm.DB, userID string) error {
	now := time.Now().UTC()
	return db.Model(&database.LocalSession{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", now).Error
}

// truncate bounds a stored string; the user-agent is attacker-controlled.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
