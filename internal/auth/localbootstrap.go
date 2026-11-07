package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"gorm.io/gorm"
)

// Owner bootstrap from the environment (issue #53 follow-up).
//
// The owner account is now created from HAAKU_ADMIN_EMAIL / HAAKU_ADMIN_PASSWORD
// at startup instead of being handed to whoever signs up first. That removes the
// race a fresh, reachable box had (the first stranger to POST /signup owned the
// instance) and it means the operator holds the owner credentials before the
// API ever answers a request.
//
// The contract, in one place:
//
//   - EMPTY instance + both variables set  -> the owner is created, active and
//     admin, with a "local_" id and an argon2id hash from the existing path.
//   - EMPTY instance + variables unset     -> the caller refuses to start. An
//     instance nobody can administer is worse than a loud exit.
//   - NON-EMPTY instance                   -> never touched, whatever the
//     variables say. Seeding is idempotent and never mutates an instance that
//     already has accounts.
//   - CLERK mode                           -> both variables are ignored
//     entirely; Clerk owns identity and this function does nothing.
//
// The refusal deliberately does NOT apply to the migrate-only path
// (HAAKU_MODE=migrate / --migrate-only), which never calls this function — so
// CI's migrate-only jobs and the omnibus first start are unaffected.

// ErrNoOwnerConfigured is returned by BootstrapOwner when an empty local-mode
// instance has no owner configured. The message is what an operator sees, so it
// names the variables and says what to do about it. It never contains a value
// (the password must never be logged).
var ErrNoOwnerConfigured = errors.New(
	"this is a fresh local-mode instance with no accounts, and no owner is configured: " +
		"set HAAKU_ADMIN_EMAIL and HAAKU_ADMIN_PASSWORD so the first start can create the owner account, " +
		"or set HAAKU_AUTH_MODE=clerk to delegate identity to Clerk")

// BootstrapOwner creates the owner account from the two admin environment
// variables on an empty local-mode instance. It is a no-op in clerk mode and on
// an instance that already has any account, so it is safe to call on every
// start. It returns an error (naming the offending variable) when the instance
// is empty and the owner cannot be created, which is the caller's cue to refuse
// to start.
func BootstrapOwner(db *gorm.DB, adminEmail, adminPassword string) error {
	if db == nil {
		return errors.New("auth: cannot bootstrap the owner without a database")
	}
	// Clerk mode ignores the variables entirely: Clerk is the only
	// authenticator, and writing a local owner row would create exactly the
	// "local user shadows a Clerk user" situation the mode switch forbids.
	if !LocalAuthEnabled() {
		return nil
	}

	// Never touch an instance that already has accounts. A soft-deleted user
	// does not count (GORM excludes it), matching "live accounts" everywhere
	// else in the module.
	var existing int64
	if err := db.Model(&database.User{}).Count(&existing).Error; err != nil {
		return fmt.Errorf("auth: count users: %w", err)
	}
	if existing > 0 {
		return nil
	}

	email := strings.TrimSpace(adminEmail)
	switch {
	case email == "" && adminPassword == "":
		return ErrNoOwnerConfigured
	case email == "":
		return errors.New("HAAKU_ADMIN_EMAIL is not set while HAAKU_ADMIN_PASSWORD is: set both, or neither")
	case adminPassword == "":
		return errors.New("HAAKU_ADMIN_PASSWORD is not set while HAAKU_ADMIN_EMAIL is: set both, or neither")
	}

	normalized, err := normalizeEmail(email)
	if err != nil {
		return fmt.Errorf("HAAKU_ADMIN_EMAIL: %w", err)
	}
	// The same minimum length the signup route enforces, so the seeded owner
	// cannot be weaker than an account created through the API. The error
	// names the variable; the password itself is never included.
	if err := ValidatePassword(adminPassword); err != nil {
		return fmt.Errorf("HAAKU_ADMIN_PASSWORD: %w", err)
	}
	hash, err := HashPassword(adminPassword)
	if err != nil {
		// The underlying error describes the KDF, not the password.
		return fmt.Errorf("auth: hash HAAKU_ADMIN_PASSWORD: %w", err)
	}

	return createOwnerRow(db, normalized, hash)
}

// createOwnerRow writes the owner row and its credential in one transaction. The
// user count is re-checked inside the transaction so two processes racing on a
// first start cannot both insert (the second sees the first's row and returns
// cleanly). The id keeps the local_ prefix — the namespace that stops a local
// id from ever colliding with a Clerk id.
func createOwnerRow(db *gorm.DB, email, hash string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&database.User{}).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		id, err := localUserID()
		if err != nil {
			return err
		}
		username, err := allocateUsername(tx, "", email)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		user := database.User{
			ID:        id,
			Username:  username,
			Email:     email,
			Status:    database.UserStatusActive,
			IsAdmin:   true,
			Plan:      "free",
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := tx.Create(&user).Error; err != nil {
			if isUniqueViolation(err) {
				return errEmailTaken
			}
			return err
		}
		return tx.Create(&database.LocalCredential{
			UserID:            id,
			PasswordHash:      hash,
			PasswordUpdatedAt: now,
		}).Error
	})
}
