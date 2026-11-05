package backup

import "context"

// BeforeUpgrade takes a pre-migration safety copy: a database-only
// before-upgrade backup and its manifest. Rotation keeps the two newest
// before-upgrade copies and nothing else of that kind.
//
// This is the helper the migration runner calls before it applies pending
// migrations, and whose failure must abort the migration rather than proceed
// unprotected. This package does not wire it into the runner: cmd/ and
// internal/database belong to issue #14 and another change.
func (s *Service) BeforeUpgrade(ctx context.Context) (*Summary, error) {
	return s.Backup(ctx, KindBeforeUpgrade)
}

// PreMigrationCopy builds a Service from opts and takes one pre-migration safety
// copy. It is the one-call helper for a migration runner that has no Service of
// its own.
func PreMigrationCopy(ctx context.Context, opts Options) (*Summary, error) {
	service, err := New(opts)
	if err != nil {
		return nil, err
	}
	return service.BeforeUpgrade(ctx)
}
