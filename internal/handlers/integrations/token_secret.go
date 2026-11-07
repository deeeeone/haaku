package integrations

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"trakt-api/internal/database"
	"trakt-api/internal/secret"

	"gorm.io/gorm"
)

// Integration tokens are bearer credentials the inbound receiver looks up on
// every authenticated request, so their at-rest protection needs BOTH halves of
// the standard "searchable encryption" pattern:
//
//  1. an HMAC-SHA256 blind index (token_hash) so the raw token can still be
//     matched with an equality query under a unique index — a random-nonce
//     AES-GCM ciphertext can never be;
//  2. an AES-256-GCM ciphertext (token) under the shared internal/secret
//     primitive, with the enc:v1: envelope and AAD row binding, so the plaintext
//     is recoverable for display/rotation without ever living in the database as
//     text.
//
// The blind index is keyed (HMAC under an HKDF-derived key, not a bare SHA-256)
// so that a database dump alone gives an attacker nothing to brute-force against
// candidate tokens: without the root key the hash column is an opaque MAC.
//
// Compare with the recoverable-token alternative it deliberately does NOT take:
// a keyed-hash-ONLY design (store no recoverable token at all; shown once at
// creation) is strictly more secure — even a database dump plus the root key
// cannot recover a token — but it removes the ability to ever display or rotate
// a credential after creation. That is a product-behaviour decision for the
// owner (see the PR body); this change keeps the recoverable ciphertext so the
// existing "returned once, revocable, recoverable for display" contract holds.

// lookupInfo domain-separates the blind-index key from the data-encryption key
// (and every other use of the root key). It is a versioned label: a future
// rotation ships a new label, not a silent change of an existing one.
const lookupInfo = "tkiller/integration-token-lookup/v1"

// tokenColumn is the integration_tokens column the ciphertext lives in. It is
// part of the AAD, so the ciphertext is bound to (table, column, row id).
const tokenColumn = "token"

// tokenSecrets bundles the two derived keys a token read/write needs, so a hot
// path derives them once rather than per row.
type tokenSecrets struct {
	cipher    *secret.Cipher
	lookupKey []byte
}

// loadTokenSecrets builds the token cipher and blind-index key from the process
// root key. A missing or too-short root key is an error (never a silent
// pass-through), which is what makes every caller — the create path, the
// backfill, the read path — refuse rather than write unreadable garbage.
func loadTokenSecrets() (*tokenSecrets, error) {
	root, err := secret.RootKeyFromEnv()
	if err != nil {
		return nil, err
	}
	c, err := secret.New(root)
	if err != nil {
		return nil, err
	}
	lookupKey, err := secret.DeriveKey(root, lookupInfo, 32)
	if err != nil {
		return nil, err
	}
	return &tokenSecrets{cipher: c, lookupKey: lookupKey}, nil
}

// hash returns the deterministic, keyed blind index of a raw token: lowercase
// hex HMAC-SHA256(token) under the derived lookup key. The same token always
// yields the same hash (so it can be indexed and matched), and two different
// tokens collide only with SHA-256 birthday probability.
func (t *tokenSecrets) hash(token string) string {
	mac := hmac.New(sha256.New, t.lookupKey)
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// tokenAAD binds a stored ciphertext to integration_tokens.token of one row, so
// a value cannot be moved to another row in SQL and still decrypt. The read and
// write sites must use the same row id.
func tokenAAD(id int64) string {
	return secret.RowAAD("integration_tokens", tokenColumn, strconv.FormatInt(id, 10))
}

// TokenLookupHash returns the blind index of a raw token using the process root
// key. It is exported so the backfill and tests derive the index exactly the way
// the lookup path does.
func TokenLookupHash(token string) (string, error) {
	ts, err := loadTokenSecrets()
	if err != nil {
		return "", err
	}
	return ts.hash(token), nil
}

// EncryptToken encrypts a raw token at rest, bound to its integration_tokens
// row id. The stored value stays recoverable so the credential can be shown and
// rotated.
func EncryptToken(id int64, token string) (string, error) {
	ts, err := loadTokenSecrets()
	if err != nil {
		return "", err
	}
	return ts.cipher.Encrypt(token, tokenAAD(id))
}

// DecryptToken reverses EncryptToken. An empty stored value returns "" (an
// absent credential, which the caller treats as a missing token); any other
// value must be an enc:v1: envelope, and any failure to decrypt it — wrong key,
// tampering, a row-level AAD swap, or a value that is not an envelope at all —
// is an error. It never hands back the ciphertext as if it were the credential.
func DecryptToken(id int64, stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	ts, err := loadTokenSecrets()
	if err != nil {
		return "", err
	}
	return ts.cipher.Decrypt(stored, tokenAAD(id))
}

// ErrIntegrationTokenNotFound is returned when no active token matches.
var ErrIntegrationTokenNotFound = errors.New("integration token not found or inactive")

// LookupToken authenticates an inbound request from the raw token in the path.
//
// It computes the blind index and matches integration_tokens.token_hash under
// the unique index; the matched row's ciphertext is decrypted so callers receive
// the real credential. An AAD-swapped or tampered row fails here rather than
// being used.
func LookupToken(db *gorm.DB, tokenStr string) (*database.IntegrationToken, error) {
	if tokenStr == "" {
		return nil, ErrIntegrationTokenNotFound
	}
	if db == nil {
		return nil, errors.New("integrations: no database handle")
	}
	hash, err := TokenLookupHash(tokenStr)
	if err != nil {
		return nil, err
	}

	var tok database.IntegrationToken
	err = db.Where("token_hash = ? AND active = ?", hash, true).First(&tok).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrIntegrationTokenNotFound
		}
		return nil, err
	}

	plaintext, err := DecryptToken(tok.ID, tok.Token)
	if err != nil {
		return nil, fmt.Errorf("integrations: token %d failed to decrypt: %w", tok.ID, err)
	}
	if plaintext == "" {
		return nil, fmt.Errorf("integrations: token %d has no stored credential", tok.ID)
	}
	tok.Token = plaintext
	return &tok, nil
}

// RotateToken issues a fresh credential for an existing row owned by userID,
// re-encrypting it in place. The old token stops matching immediately because its
// blind index is replaced. It returns the new raw token (shown once).
func RotateToken(db *gorm.DB, id int64, userID string) (string, error) {
	if db == nil {
		return "", errors.New("integrations: no database handle")
	}
	var tok database.IntegrationToken
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&tok).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrIntegrationTokenNotFound
		}
		return "", err
	}
	newToken, err := generateToken()
	if err != nil {
		return "", err
	}
	enc, err := EncryptToken(tok.ID, newToken)
	if err != nil {
		return "", err
	}
	hash, err := TokenLookupHash(newToken)
	if err != nil {
		return "", err
	}
	if err := db.Model(&database.IntegrationToken{}).Where("id = ?", tok.ID).
		Updates(map[string]any{"token": enc, "token_hash": hash}).Error; err != nil {
		return "", err
	}
	return newToken, nil
}
