// Package backup takes, lists, rotates, deletes and restores whole-database
// backups for trakt-killer: a pg_dump of the database in custom format plus a
// manifest that is written last.
//
// docs/architecture/backups.md is the durable description. This comment states
// only the rules the code must keep.
//
// # Layout
//
// Every backup is one folder at the destination:
//
//	backups/<server-id>/<UTC time>-<kind>/
//	  db.dump       pg_dump custom format, zstd
//	  manifest.json written last; its presence is the commit point
//
// # Commit point
//
// A folder without manifest.json is incomplete and is never a valid backup: it
// is never restored from, never counted toward the retention count, and never
// rotated over a complete one. The manifest is the last object written, so its
// presence means every object it lists was written in full.
//
// # Folder ids
//
// A folder is never reused. A run reserves its id under the subsystem lock from
// the folders already at the destination, complete or not and of any kind; a
// time a folder already has (two runs in one millisecond, or a clock that went
// back) moves on a millisecond until it is free. Every folder's time is
// therefore distinct, which keeps rotation's newest-first order total.
//
// # Bounded listings
//
// Anyone who can write the destination could otherwise hold enough objects to
// exhaust the server's memory on every scheduled backup. Every listing pages
// through Destination.ListPage and stops at a cap; past it the listing fails and
// nothing is rotated or swept.
//
// # Encryption (not implemented here)
//
// Objects are written in the clear. The destination abstraction carries a
// KeyProvider seam so that at-rest encryption of destination objects can be
// added without changing the layout; it is section 2 of issue #16 and is wired
// by a later change.
package backup
