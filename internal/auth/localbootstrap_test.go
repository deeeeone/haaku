package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/haaku-tv/haaku/internal/database"
)

// Owner bootstrap from the environment (issue #53 follow-up). These tests drive
// auth.BootstrapOwner directly against the same in-memory sqlite handle the rest
// of the module tests use. cmd/api wires this function into startup after
// migrations; the real binary path is exercised against PostgreSQL in the PR.

const bootstrapOwnerPassword = "owner-password-1234" // 20 chars, over MinPasswordLen

func TestBootstrapOwnerSeedsActiveAdminOwner(t *testing.T) {
	db, _ := setupLocalAuth(t)

	if err := BootstrapOwner(db, "owner@example.com", bootstrapOwnerPassword); err != nil {
		t.Fatalf("BootstrapOwner on an empty instance: %v", err)
	}

	var user database.User
	if err := db.First(&user, "email = ?", "owner@example.com").Error; err != nil {
		t.Fatalf("owner row: %v", err)
	}
	if user.Status != database.UserStatusActive {
		t.Errorf("seeded owner status = %q, want active", user.Status)
	}
	if !user.IsAdmin {
		t.Error("the seeded owner is not an admin")
	}
	if !strings.HasPrefix(user.ID, LocalIDPrefix) {
		t.Errorf("seeded owner id %q is not namespaced with %q", user.ID, LocalIDPrefix)
	}

	// The credential is a real argon2id hash from the existing path, and the
	// configured password verifies against it.
	var cred database.LocalCredential
	if err := db.First(&cred, "user_id = ?", user.ID).Error; err != nil {
		t.Fatalf("owner credential row: %v", err)
	}
	if !strings.HasPrefix(cred.PasswordHash, "$argon2id$") {
		t.Errorf("stored hash is not argon2id: %q", cred.PasswordHash)
	}
	if err := VerifyPassword(cred.PasswordHash, bootstrapOwnerPassword); err != nil {
		t.Errorf("the configured password does not verify: %v", err)
	}
	if err := VerifyPassword(cred.PasswordHash, "not-the-password-xx"); err == nil {
		t.Error("a wrong password verified against the seeded hash")
	}
}

func TestBootstrapOwnerRefusesEmptyInstanceWithNoOwnerConfigured(t *testing.T) {
	db, _ := setupLocalAuth(t)

	err := BootstrapOwner(db, "", "")
	if !errors.Is(err, ErrNoOwnerConfigured) {
		t.Fatalf("empty instance with no vars: err = %v, want ErrNoOwnerConfigured", err)
	}
	// The message must tell the operator what to do — and name both variables.
	for _, want := range []string{"HAAKU_ADMIN_EMAIL", "HAAKU_ADMIN_PASSWORD"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal message does not name %s: %q", want, err.Error())
		}
	}
	// Nothing was created by a refusal.
	var n int64
	if err := db.Model(&database.User{}).Count(&n).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 0 {
		t.Fatalf("a refusal created %d users, want 0", n)
	}
}

func TestBootstrapOwnerRefusesHalfConfiguredInstance(t *testing.T) {
	cases := []struct {
		name          string
		email, passwd string
		wantNamed     string
	}{
		{"email only", "owner@example.com", "", "HAAKU_ADMIN_PASSWORD"},
		{"password only", "", bootstrapOwnerPassword, "HAAKU_ADMIN_EMAIL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := setupLocalAuth(t)
			err := BootstrapOwner(db, tc.email, tc.passwd)
			if err == nil {
				t.Fatal("half-configured instance did not refuse to start")
			}
			if !strings.Contains(err.Error(), tc.wantNamed) {
				t.Errorf("error %q does not name the missing variable %s", err.Error(), tc.wantNamed)
			}
			var n int64
			_ = db.Model(&database.User{}).Count(&n).Error
			if n != 0 {
				t.Fatalf("a refusal created %d users, want 0", n)
			}
		})
	}
}

func TestBootstrapOwnerRefusesShortPasswordNamingTheVariable(t *testing.T) {
	db, _ := setupLocalAuth(t)

	err := BootstrapOwner(db, "owner@example.com", "short")
	if err == nil {
		t.Fatal("BootstrapOwner accepted a password shorter than MinPasswordLen")
	}
	if !strings.Contains(err.Error(), "HAAKU_ADMIN_PASSWORD") {
		t.Errorf("error %q does not name HAAKU_ADMIN_PASSWORD", err.Error())
	}
	// The message must never contain the password value itself.
	if strings.Contains(err.Error(), "short") {
		t.Errorf("the refusal error leaks the password value: %q", err.Error())
	}
	var n int64
	_ = db.Model(&database.User{}).Count(&n).Error
	if n != 0 {
		t.Fatalf("a refused short password created %d users, want 0", n)
	}
}

func TestBootstrapOwnerRejectsInvalidEmail(t *testing.T) {
	db, _ := setupLocalAuth(t)
	err := BootstrapOwner(db, "not-an-email", bootstrapOwnerPassword)
	if err == nil {
		t.Fatal("BootstrapOwner accepted an invalid email")
	}
	if !strings.Contains(err.Error(), "HAAKU_ADMIN_EMAIL") {
		t.Errorf("error %q does not name HAAKU_ADMIN_EMAIL", err.Error())
	}
}

func TestBootstrapOwnerIsIdempotentAndNeverTouchesAnInstanceWithUsers(t *testing.T) {
	db, _ := setupLocalAuth(t)

	if err := BootstrapOwner(db, "owner@example.com", bootstrapOwnerPassword); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	var first database.User
	if err := db.First(&first, "email = ?", "owner@example.com").Error; err != nil {
		t.Fatalf("owner row: %v", err)
	}

	// A second start with different variables must not add or modify anything.
	if err := BootstrapOwner(db, "other@example.com", "another-password-99"); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	var count int64
	if err := db.Model(&database.User{}).Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("user count = %d after a second start, want 1 (seeding must be idempotent)", count)
	}
	var got database.User
	if err := db.First(&got).Error; err != nil {
		t.Fatalf("reload owner: %v", err)
	}
	if got.ID != first.ID || got.Email != "owner@example.com" {
		t.Fatalf("the existing owner was modified: %+v, want id %q email owner@example.com", got, first.ID)
	}
	// The original password still works — nothing was re-hashed under it.
	var cred database.LocalCredential
	if err := db.First(&cred, "user_id = ?", first.ID).Error; err != nil {
		t.Fatalf("credential row: %v", err)
	}
	if err := VerifyPassword(cred.PasswordHash, bootstrapOwnerPassword); err != nil {
		t.Errorf("the original owner password stopped verifying after a restart: %v", err)
	}
}

func TestBootstrapOwnerNeverTouchesAnInstanceWithAnyUsers(t *testing.T) {
	// An instance that already has a non-owner account must not gain an owner:
	// "already has users" is the whole guard, not "already has an admin".
	db, app := setupLocalAuth(t)
	signup(t, app, "member@example.com", "correct-horse-battery")

	if err := BootstrapOwner(db, "late-owner@example.com", bootstrapOwnerPassword); err != nil {
		t.Fatalf("bootstrap on a non-empty instance: %v", err)
	}
	var n int64
	if err := db.Model(&database.User{}).Count(&n).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Fatalf("user count = %d, want 1 (an instance with users must never be seeded)", n)
	}
	var owner int64
	if err := db.Model(&database.User{}).Where("is_admin = ?", true).Count(&owner).Error; err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if owner != 0 {
		t.Fatalf("a late bootstrap minted %d admins on a non-empty instance", owner)
	}
}

func TestBootstrapOwnerIgnoresVariablesInClerkMode(t *testing.T) {
	db, _ := setupLocalAuth(t)
	if err := InitAuthMode("clerk", "approval"); err != nil {
		t.Fatalf("switch to clerk: %v", err)
	}
	t.Cleanup(func() { _ = InitAuthMode("", "") })

	// Both variables set, but clerk mode ignores them entirely — no local owner
	// row, and no refusal (the refusal is a local-mode-only condition).
	if err := BootstrapOwner(db, "owner@example.com", bootstrapOwnerPassword); err != nil {
		t.Fatalf("clerk mode must ignore the admin variables, got: %v", err)
	}
	var n int64
	if err := db.Model(&database.User{}).Count(&n).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 0 {
		t.Fatalf("clerk mode created %d local users, want 0", n)
	}

	// And unset variables in clerk mode are not an error either (no refusal).
	if err := BootstrapOwner(db, "", ""); err != nil {
		t.Fatalf("clerk mode with no vars returned: %v, want nil", err)
	}
}
