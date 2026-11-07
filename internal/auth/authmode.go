package auth

import (
	"fmt"
	"sync"

	"github.com/haaku-tv/haaku/internal/config"
)

// Identity mode (issue #53).
//
// The mode decides whether the local account module exists at all. Every
// decision it drives — registering the routes, registering their OpenAPI
// definitions, accepting a local session token in the middleware — goes through
// LocalAuthEnabled, so the router and the spec cannot disagree about it and a
// clerk instance cannot authenticate a local session even if one were somehow
// in the database.
//
// The zero value is the self-hosted default (local/approval): cmd/openapi and
// cmd/parity register routes through this package without ever calling
// InitAuthMode, and the committed spec documents the local default. The API
// server calls InitAuthMode with the validated values from config.Load.

var (
	authModeMu sync.RWMutex
	authMode   = config.AuthModeLocal
	signupMode = config.SignupApproval
)

// InitAuthMode records the identity mode and local sign-up policy selected at
// startup. Empty values fall back to the documented defaults (local /
// approval), so a caller that has no configuration still gets the self-hosted
// shape rather than an instance with no authenticator. Safe to call again —
// the last call wins, which is what tests rely on to switch modes.
func InitAuthMode(mode, signup string) error {
	if mode == "" {
		mode = config.AuthModeLocal
	}
	switch mode {
	case config.AuthModeLocal, config.AuthModeClerk:
	default:
		return fmt.Errorf("auth: unknown auth mode %q (want %q or %q)",
			mode, config.AuthModeLocal, config.AuthModeClerk)
	}

	if signup == "" {
		signup = config.SignupApproval
	}
	switch signup {
	case config.SignupApproval, config.SignupOpen, config.SignupClosed:
	default:
		return fmt.Errorf("auth: unknown signup policy %q (want %q, %q or %q)",
			signup, config.SignupApproval, config.SignupOpen, config.SignupClosed)
	}

	authModeMu.Lock()
	authMode, signupMode = mode, signup
	authModeMu.Unlock()
	return nil
}

// AuthMode returns the active identity mode ("local" or "clerk").
func AuthMode() string {
	authModeMu.RLock()
	defer authModeMu.RUnlock()
	return authMode
}

// SignupPolicy returns the local sign-up policy (approval | open | closed).
func SignupPolicy() string {
	authModeMu.RLock()
	defer authModeMu.RUnlock()
	return signupMode
}

// LocalAuthEnabled reports whether this instance issues identity itself.
// False means clerk mode: the local routes are not registered and a local
// session token is not an accepted credential.
func LocalAuthEnabled() bool { return AuthMode() == config.AuthModeLocal }
