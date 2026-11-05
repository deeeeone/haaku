package database

import (
	"context"
	"errors"
)

// ErrMigrationsNotImplemented is returned by Migrate until the migration runner
// lands (issue #14).
//
// It is an error and not a no-op on purpose: a server that cannot create its own
// schema must say so at startup rather than serve requests against an empty
// database.
var ErrMigrationsNotImplemented = errors.New("schema migrations are not implemented yet (see issue #14)")

// Migrate applies every pending schema migration to the configured database.
//
// The intended contract, once issue #14 lands:
//   - migrations are embedded in the binary (migrations/embed.go) and applied
//     from there, so the binary carries the schema it expects;
//   - the run is serialised by a PostgreSQL advisory lock, so two servers
//     starting at once do not race;
//   - a pg_dump safety copy is taken first, and a failure to take it aborts the
//     migration rather than proceeding unprotected;
//   - the version ledger is seeded from the legacy numbered migrations so an
//     existing install is not replayed.
func Migrate(_ context.Context) error {
	return ErrMigrationsNotImplemented
}
