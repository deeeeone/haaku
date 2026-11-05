package webhooks

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"

	"trakt-api/internal/secret"
)

// Webhook endpoint secrets are encrypted at rest with the shared
// trakt-api/internal/secret primitive: AES-256-GCM with a random 12-byte nonce
// and the enc:v1:<base64url(nonce‖sealed)> envelope, under a data key that is
// HKDF-SHA256 derived from the root key (TKILLER_SECRET_KEY, or the legacy
// WEBHOOK_SECRET_KEY). This file owns the two things that are webhook-specific:
// the AAD row binding, and the compatibility path for values written by the
// earlier format.

// webhookSecretAAD binds a stored ciphertext to webhook_endpoints.secret_hash
// of one endpoint, so a value cannot be moved to another row in SQL and still
// decrypt. The read site (the delivery worker) and both write sites (create and
// rotate) must use the same endpoint id.
func webhookSecretAAD(endpointID int64) string {
	return secret.RowAAD("webhook_endpoints", "secret_hash", strconv.FormatInt(endpointID, 10))
}

// EncryptSecret encrypts a webhook signing secret at rest, bound to its
// endpoint row. The stored value must stay recoverable: the delivery worker
// decrypts it to sign requests with the plaintext the consumer was shown once.
func EncryptSecret(endpointID int64, plaintext string) (string, error) {
	c, err := secret.LoadFromEnv()
	if err != nil {
		return "", err
	}
	return c.Encrypt(plaintext, webhookSecretAAD(endpointID))
}

// DecryptSecret reverses EncryptSecret.
//
// The read path has three cases:
//
//  1. an empty stored value returns "" (nothing to recover);
//  2. an enc:v1: value is decrypted under the endpoint's AAD, and any failure
//     (wrong key, tampering, truncation, moved row) is an error — it is never
//     silently used as a credential;
//  3. a non-enveloped value is a blob written by the pre-enc:v1 format, and is
//     decrypted by decryptLegacyWebhookSecret. If that also fails the value is
//     unrecoverable (e.g. an HMAC "hashSecret" value from an even earlier
//     release, which cannot be reversed) and an error is returned — the raw
//     bytes are never handed back as if they were the signing secret.
func DecryptSecret(endpointID int64, stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	c, err := secret.LoadFromEnv()
	if err != nil {
		return "", err
	}
	if secret.IsEncrypted(stored) {
		return c.Decrypt(stored, webhookSecretAAD(endpointID))
	}
	plaintext, err := decryptLegacyWebhookSecret(stored)
	if err != nil {
		return "", fmt.Errorf("webhook secret is not a recognisable encrypted value (root key changed, or a pre-encryption HMAC value): %w", err)
	}
	return plaintext, nil
}

// decryptLegacyWebhookSecret decrypts the format used before this package
// existed: base64.StdEncoding(nonce‖sealed) with AES-256-GCM, no AAD, and a key
// taken directly from the hex-encoded WEBHOOK_SECRET_KEY (32 bytes / 64 hex
// chars). It exists only so already-stored values keep working across the
// migration; new values are always written with EncryptSecret.
//
// The key is read from WEBHOOK_SECRET_KEY (the variable that produced legacy
// blobs) when set, otherwise from TKILLER_SECRET_KEY — so an operator who
// renames the variable to the same hex value keeps reading old rows.
func decryptLegacyWebhookSecret(stored string) (string, error) {
	raw := os.Getenv(secret.LegacyEnvVar)
	if raw == "" {
		raw = os.Getenv(secret.EnvVar)
	}
	if raw == "" {
		return "", fmt.Errorf("neither %s nor %s is set", secret.LegacyEnvVar, secret.EnvVar)
	}
	key, err := hex.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("legacy root key is not hex-encoded: %w", err)
	}
	if len(key) != 32 {
		return "", fmt.Errorf("legacy root key must decode to 32 bytes (64 hex chars), got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("legacy aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("legacy gcm: %w", err)
	}
	blob, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", fmt.Errorf("legacy blob is not valid base64: %w", err)
	}
	if len(blob) < aead.NonceSize() {
		return "", errors.New("legacy blob is too short")
	}
	nonce, sealed := blob[:aead.NonceSize()], blob[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("legacy decryption failed: %w", err)
	}
	return string(plaintext), nil
}
