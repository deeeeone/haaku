package webhooks

import (
	"strconv"

	"github.com/haaku-tv/haaku/internal/secret"
)

// Webhook endpoint secrets are encrypted at rest with the shared
// haaku/internal/secret primitive: AES-256-GCM with a random 12-byte nonce
// and the enc:v1:<base64url(nonce‖sealed)> envelope, under a data key that is
// HKDF-SHA256 derived from the root key (HAAKU_SECRET_KEY). This file owns the
// two things that are webhook-specific: the AAD row binding, and the read/write
// points that pin it to the endpoint row.

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

// DecryptSecret reverses EncryptSecret. The read path has two cases:
//
//  1. an empty stored value returns "" (nothing to recover), so an endpoint
//     whose secret was never written is reported as absent rather than failing
//     to decrypt;
//  2. a stored value is decrypted under the endpoint's AAD, and any failure
//     (wrong key, tampering, truncation, moved row, or a value that is not an
//     enc:v1: envelope) is an error — the raw bytes are never handed back as if
//     they were the signing secret.
func DecryptSecret(endpointID int64, stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	c, err := secret.LoadFromEnv()
	if err != nil {
		return "", err
	}
	return c.Decrypt(stored, webhookSecretAAD(endpointID))
}
