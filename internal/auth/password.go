package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// Password hashing for local accounts (issue #53 §2).
//
// Argon2id, not bcrypt: it is the current OWASP-recommended password KDF, it is
// memory-hard (the parameter below costs 19 MiB per hash, which removes the
// GPU/ASIC advantage that makes bcrypt cheap to attack offline), and bcrypt
// additionally truncates input at 72 bytes and silently ignores the rest. The
// parameters are the OWASP "low memory" profile (m=19 MiB, t=2, p=1) — a
// self-hosted box is often a small ARM board, and a 64 MiB hash would make a
// handful of concurrent logins on one core an easy denial of service.
//
// Hashes are stored as PHC strings ($argon2id$v=19$m=...,t=...,p=...$salt$hash),
// so the parameters travel with the hash and can be raised later without
// invalidating existing passwords.
const (
	argonMemoryKiB   = 19 * 1024 // 19 MiB
	argonIterations  = 2
	argonParallelism = 1
	argonSaltLen     = 16
	argonKeyLen      = 32

	// MinPasswordLen is the shortest accepted signup/reset password. The max is
	// a DoS bound on the KDF input, not a security claim.
	MinPasswordLen = 10
	MaxPasswordLen = 200
)

// ErrInvalidCredentials is the single error a local login returns for a wrong
// password and for an email with no account. Never distinguish the two to a
// caller: only the server logs do.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// ErrInvalidHash is returned by VerifyPassword for a malformed stored hash.
// It is not an authentication failure — callers must treat a malformed row as
// a server error, never as "wrong password".
var ErrInvalidHash = errors.New("auth: malformed password hash")

// ValidatePassword enforces the length bounds. Returns an error for the caller
// to render as a 400 envelope.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLen {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}
	if len(password) > MaxPasswordLen {
		return fmt.Errorf("password must be at most %d characters", MaxPasswordLen)
	}
	return nil
}

// HashPassword derives an Argon2id PHC string from password with a fresh
// random salt.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemoryKiB, argonParallelism, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the stored PHC string. It
// returns ErrInvalidHash when the stored value is not a parseable argon2id
// hash, so a corrupt row cannot be mistaken for a wrong password.
func VerifyPassword(encoded, password string) error {
	memory, iterations, parallelism, salt, want, err := parseHash(encoded)
	if err != nil {
		return err
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

// parseHash splits an argon2id PHC string into its parameters.
func parseHash(encoded string) (memory, iterations uint32, parallelism uint8, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, key]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}
	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}
	if key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}
	if len(salt) == 0 || len(key) == 0 {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}
	return memory, iterations, parallelism, salt, key, nil
}

// dummyHash is verified against when a login names an email with no account, so
// an unknown email costs the same Argon2id work as a known one. Without it the
// response time itself answers "does this email have an account here?".
var (
	dummyHashOnce sync.Once
	dummyHash     string
)

// dummyPasswordHash returns a hash of random material, computed once and
// verifiably unguessable.
func dummyPasswordHash() string {
	dummyHashOnce.Do(func() {
		junk := make([]byte, 32)
		if _, err := rand.Read(junk); err != nil {
			// A failure here only weakens timing equalisation; the random
			// material is still unguessable enough for that purpose.
			junk = []byte(fmt.Sprintf("haaku-dummy-%p", &dummyHash))
		}
		h, err := HashPassword(string(junk))
		if err != nil {
			h = ""
		}
		dummyHash = h
	})
	return dummyHash
}
