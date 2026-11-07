// Package secret provides at-rest symmetric encryption for server-owned
// credentials (outbound-webhook signing secrets today; integration tokens,
// OAuth client secrets, S3 keys and similar in follow-ups). Callers store the
// resulting ciphertext in the column that previously held the plaintext and
// decrypt on read, so the secret never lives in the database as naked text.
//
// The one primitive everything reuses is AES-256-GCM with a random 12-byte
// nonce and a versioned envelope, enc:v1:<base64url(nonce‖sealed)>. The 32-byte
// data key is HKDF-SHA256 derived from a root key (the TKILLER_SECRET_KEY env
// value) under a versioned domain label, so the key that protects the data
// lives outside Postgres and encrypted secrets survive a full database dump.
// The root key is never stored in the database and must be backed up separately
// from database dumps — a dump full of ciphertext that travels with the key
// gives no confidentiality at all.
//
// Each ciphertext is GCM-bound to its logical row via additional-authenticated
// data (AAD) of the form <table>:<column>:<row key>, so a database-write
// attacker cannot transplant a credential blob into another row or column:
// decrypting with a different binding fails authentication.
//
// # Read path
//
// Read sites decrypt with Decrypt: a stored value must be an enc:v1: envelope,
// and any failure to open it — wrong key, tampering, truncation, AAD mismatch —
// is an error, never a plaintext fallback. An absent value is represented as an
// empty column, which the caller checks before calling Decrypt.
//
// # Equality-looked-up secrets
//
// Some secrets are looked up by their raw value (integration tokens matched
// with WHERE token = $1). AES-256-GCM uses a random nonce, so storing the
// ciphertext alone is NOT sufficient there — the lookup would never match.
// Integration tokens therefore carry a second, deterministic column: an
// HMAC-SHA256 blind index under an HKDF-derived lookup key
// ("tkiller/integration-token-lookup/v1"), matched under a partial unique index,
// alongside the randomized, AAD-bound ciphertext the create/rotate path writes.
// See internal/handlers/integrations/token_secret.go for the write/read path.
// Any future equality-looked-up secret should follow the same two-column
// pattern rather than be half-migrated.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	// envelopePrefix tags a value as v1 ciphertext. It is exactly 7 ASCII
	// characters, is not valid base64url, and does not collide with any real
	// credential in practice (hex secrets, UUIDs, JWTs, URLs).
	envelopePrefix = "enc:v1:"

	// dataEncryptionInfo domain-separates the derived data key from any other
	// use of the root key. It is a versioned label: a future rotation ships
	// enc:v2: together with "tkiller/data-encryption/v2".
	dataEncryptionInfo = "tkiller/data-encryption/v1"

	// MinMasterKeyLen is the minimum acceptable root-key length in bytes, and
	// the minimum number of characters the environment variable must hold. The
	// server refuses to start below this floor.
	MinMasterKeyLen = 32

	// dataKeyLen is the AES-256 key size.
	dataKeyLen = 32
)

// EnvVar is the root-key environment variable. It must hold at least
// MinMasterKeyLen characters.
const EnvVar = "TKILLER_SECRET_KEY"

// ErrUnknownVersion is returned by Decrypt when the envelope carries a version
// this build does not understand. It is the forward-compatibility hook for a
// future enc:v2: key rotation: an older binary surfaces an explicit error
// instead of silently mishandling a newer ciphertext.
var ErrUnknownVersion = errors.New("secret: unknown ciphertext version")

// Cipher encrypts and decrypts short credential strings with AES-256-GCM. The
// data key is derived once in New and the AEAD is built once and reused, so the
// hot path is a single Seal/Open. A Cipher is safe for concurrent use by
// multiple goroutines.
type Cipher struct {
	gcm cipher.AEAD
}

// RootKeyFromEnv returns the raw root-key bytes from EnvVar. It errors when the
// variable is unset, or when its value is shorter than MinMasterKeyLen
// characters, so an unusable encryption root is rejected before any cipher is
// built.
func RootKeyFromEnv() ([]byte, error) {
	v := os.Getenv(EnvVar)
	if v == "" {
		return nil, fmt.Errorf(
			"secret: no root key set — set %s to at least %d characters; generate one with `openssl rand -base64 48`",
			EnvVar, MinMasterKeyLen)
	}
	if len(v) < MinMasterKeyLen {
		return nil, fmt.Errorf("secret: %s must be at least %d characters, got %d", EnvVar, MinMasterKeyLen, len(v))
	}
	return []byte(v), nil
}

// LoadFromEnv builds the process Cipher from the environment. It is the entry
// point every call site uses; the root key is validated on each call so a
// missing/short key fails immediately rather than producing a weak cipher.
func LoadFromEnv() (*Cipher, error) {
	key, err := RootKeyFromEnv()
	if err != nil {
		return nil, err
	}
	return New(key)
}

// DeriveKey derives length bytes of purpose-separated key material from the
// root key using HKDF-SHA256 (RFC 5869). Distinct purpose strings yield
// independent keys, so one root key can serve data encryption, backup
// encryption and session signing without cross-use. Purposes must be versioned
// labels (e.g. "tkiller/data-encryption/v1") so a future rotation changes the
// label without silently changing the meaning of an existing call site.
func DeriveKey(masterKey []byte, purpose string, length int) ([]byte, error) {
	if len(masterKey) < MinMasterKeyLen {
		return nil, fmt.Errorf("secret: master key must be at least %d bytes, got %d", MinMasterKeyLen, len(masterKey))
	}
	if purpose == "" {
		return nil, errors.New("secret: purpose label must not be empty")
	}
	key, err := hkdf.Key(sha256.New, masterKey, nil /* salt */, purpose, length)
	if err != nil {
		return nil, fmt.Errorf("secret: derive key for %q: %w", purpose, err)
	}
	return key, nil
}

// New derives a Cipher from the root key (the raw TKILLER_SECRET_KEY value).
// The key must be at least MinMasterKeyLen bytes; New errors otherwise so a
// short or empty key can never silently produce a weak cipher.
func New(masterKey []byte) (*Cipher, error) {
	dataKey, err := DeriveKey(masterKey, dataEncryptionInfo, dataKeyLen)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, fmt.Errorf("secret: new aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret: new gcm: %w", err)
	}
	return &Cipher{gcm: gcm}, nil
}

// Encrypt seals plaintext under the data key, binding the ciphertext to aad (the
// row-identity string the matching Decrypt must supply). An empty plaintext
// returns ("", nil): an absent credential stays an empty column value and is
// never wrapped in an envelope. The result is enc:v1:<base64url(nonce‖sealed)>.
func (c *Cipher) Encrypt(plaintext, aad string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secret: read nonce: %w", err)
	}
	// Seal appends the ciphertext to nonce, so the returned slice is nonce‖sealed.
	sealed := c.gcm.Seal(nonce, nonce, []byte(plaintext), []byte(aad))
	return envelopePrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Decrypt opens an enc:v1: envelope produced by Encrypt with the same aad. Any
// failure — unknown version, malformed base64, truncation, tampering, the wrong
// key, an aad mismatch, or a value that is not an envelope at all — returns an
// error. Decrypt never falls back to returning the input ciphertext or an empty
// string; an absent credential is an empty column the caller checks before
// calling Decrypt.
func (c *Cipher) Decrypt(ciphertext, aad string) (string, error) {
	// Parse "enc:<version>:<body>" and dispatch on the version so a future
	// enc:v2: can be added without touching existing call sites.
	parts := strings.SplitN(ciphertext, ":", 3)
	if len(parts) != 3 || parts[0] != "enc" {
		return "", errors.New("secret: not an enc envelope")
	}
	switch parts[1] {
	case "v1":
		return c.openV1(parts[2], aad)
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownVersion, parts[1])
	}
}

// openV1 decodes and authenticates a v1 body (the part after "enc:v1:").
func (c *Cipher) openV1(encoded, aad string) (string, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("secret: decode ciphertext: %w", err)
	}
	nonceSize := c.gcm.NonceSize()
	if len(sealed) < nonceSize {
		return "", errors.New("secret: ciphertext too short")
	}
	nonce, body := sealed[:nonceSize], sealed[nonceSize:]
	plaintext, err := c.gcm.Open(nil, nonce, body, []byte(aad))
	if err != nil {
		return "", fmt.Errorf("secret: authenticate ciphertext: %w", err)
	}
	return string(plaintext), nil
}

// RowAAD returns the additional-authenticated-data string binding a ciphertext
// to one logical row: table:column:<row key>. Binding to row identity stops a
// database-write attacker from transplanting a credential blob into another row
// or column — decrypting with a different (table, column, row key) fails
// authentication. Read and write sites must both use this helper so the AAD
// cannot drift.
func RowAAD(table, column, rowKey string) string {
	return table + ":" + column + ":" + rowKey
}
