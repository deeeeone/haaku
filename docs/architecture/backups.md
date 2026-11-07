# Backups and restore

`internal/backup` takes, lists, rotates, deletes and restores whole-database
backups. A backup is one folder holding a `pg_dump` archive and a manifest that
is written last; the manifest's presence is the commit point that makes the
folder a valid recovery point. This document is the durable description; the
package comment states only the rules the code must keep.

A backup is a recovery point for **this server's own database**. It is not
point-in-time recovery — a deployment that needs that archives WAL at the
PostgreSQL layer. It does not carry the `SECRET_KEY`, `.env`, or Compose files,
and it does not carry media files (haaku keeps media in a separate store).
Those belong to the operator's own off-server copy.

## Layout

Every backup is one folder at the destination:

```
backups/<server-id>/<UTC time>-<kind>/
  db.dump        pg_dump custom format, zstd level 3
  manifest.json  written last; its presence is the commit point
```

- `server-id` is `HAAKU_SERVER_ID` (default `default`). Every server writes and
  rotates only its own prefix.
- The folder name is the backup id: the start of the run in UTC, to the
  millisecond, and the kind — `20260102T030405.123Z-nightly`. The kinds are
  `nightly`, `manual`, `before-restore`, and `before-upgrade`. A name that does
  not parse is never treated as a backup.
- **A folder is never reused.** A run reserves its id under the subsystem lock
  from the folders already at the destination, complete or not and of any kind; a
  time a folder already has (two runs in one millisecond, or a clock that went
  back) moves on a millisecond until it is free. Every folder's time is therefore
  distinct, which keeps rotation's newest-first order total. Two runs started in
  the same millisecond still land in different folders.

### Commit point

The manifest is the **last object written**. Its presence means every object it
lists was written in full, at the size and checksum it records. Therefore:

- A folder without `manifest.json` is **incomplete**. It is never restored from,
  never counted toward the retention count, and never rotated in place of a
  complete one. A run that fails before the manifest leaves an incomplete folder
  and deletes nothing.
- A folder whose manifest lists an object the folder does not hold, or holds at a
  different size, is **incomplete** too. It keeps its manifest, so the sweep
  leaves it for the operator.
- A folder whose manifest is present but cannot be read or verified is
  **unreadable**. It is listed with that status, kept, never rotated and never
  swept.

### Manifest

`manifest.json` has `format: "haaku-backup"` and `version: 1`. A reader accepts
every version up to its own and refuses a newer one. It records the id, kind,
server id, start and completion times, the app version, the PostgreSQL and
`pg_dump` versions, the database name, each object's name, size and SHA-256, and
the core table inventory (each table's presence and row count). A manifest is at
most 16 MiB, which bounds what a reader reads.

A manifest that names a folder, kind or server other than the one it was read
from is refused, not trusted in part.

## Bounded listings

Anyone who can write the destination could otherwise hold enough objects, or page
without end, to exhaust the server's memory on every scheduled backup. Every
listing pages through `Destination.ListPage` (at most 1,000 objects a request)
and stops at a cap: one server's backup folders are read in key order, at most
2,000 folders and 1,000 requests, and a folder holding more than 32 objects (which
no backup writes) has unknown contents and is `unreadable`. Past a cap the
listing fails, so **nothing is rotated or swept**. A listing that does not advance
is refused.

## Taking a backup

1. Take the subsystem lock, so one backup, rotation, deletion or restore runs at
   a time.
2. List the folders at the destination and reserve an id (above).
3. Run `pg_dump --format=custom --compress=zstd:3` and stream its stdout straight
   into the destination, hashing the stored bytes. Nothing is spooled to disk.
4. Read the core table inventory and check the required tables. A backup must
   never claim to be complete without user data.
5. Write the manifest — the commit point.
6. Rotate and sweep.

A failure before step 5 leaves an incomplete folder and deletes nothing.

## Rotation and the sweep

Rotation runs after a successful backup and on its own. It keeps the newest
`backup.keep` complete backups of the nightly, manual and before-restore kinds,
and, separately, the two newest `before-upgrade` copies. Everything older is
deleted, oldest first.

- An **incomplete** folder is not a valid backup: it is never counted toward the
  retention count and is never deleted in place of a complete one. It is swept
  only once its newest object is older than 24 hours (`IncompleteGrace`), because
  a run that is still writing holds the lock — so an old incomplete folder is
  abandoned and a fresh one may not be.
- An **unreadable** folder is kept, never rotated and never swept.
- A delete removes a folder's manifest first, then the rest: a destination that
  refuses deletes then leaves the backup whole, and a delete that stops partway
  leaves an incomplete folder the sweep finishes, never a manifest that lists
  objects that are gone.

## Restore

`api --backup-restore <id>` replaces the current database with the named
backup. It:

1. Refuses a folder that is not complete (incomplete, or unreadable).
2. Reads `db.dump` back and checks its size and SHA-256 against the manifest
   **before** `pg_restore` sees it, so a truncated or tampered archive never
   reaches the database.
3. Runs `pg_restore --clean --if-exists --no-owner --no-privileges` against the
   configured database, reading the archive from stdin.
4. Confirms the restored database matches the manifest's inventory: each table the
   manifest recorded as present must be present with the same row count. A
   mismatch fails the restore (`ErrRestoreVerificationFailed`).

A full restore needs a `pg_restore` at least as new as the `pg_dump` that wrote
the archive.

### Verifying a restore

Taking a backup proves nothing about whether it can be restored. The drill is to
restore it into a scratch database — not the live one — and check the data is
there. The migration runner's safety copy and the operator's verified backup are
both only as good as the last time someone restored one.

## Pre-migration safety copy

`Service.BeforeUpgrade` (and the one-call `PreMigrationCopy`) takes a
`before-upgrade` backup of the database before pending migrations are applied.
Rotation keeps the two newest. A failure to take it must abort the migration
rather than proceed unprotected.

The helper is **exposed but not wired in**: the migration runner
(`internal/database`, issue #14) belongs to another change; it calls this helper
from its startup path.

## Destinations

`backup.Destination` is the seam for every destination: put a stream, open, list
a page, delete a folder, probe, report a human-readable location.

- **Local path** (`NewLocalDestination`, `LocalLocker`): the default. Objects are
  0700 directories and 0600 files: a local backup holds the same data as the
  database, password hashes included. The subsystem lock is an `flock` on a lock
  file in the destination directory, so it excludes other processes sharing the
  destination.
- **S3-compatible** (`NewS3Destination`): **declared but not implemented.** It
  always returns `ErrS3NotImplemented` so a caller cannot mistake the stub for a
  working destination. It lands with credentials and a live bucket, which this
  change does not have; tests must never reach the network.

A destination is trusted not to escape its root: deletes refuse a prefix or key
outside `backups/`.

## Encryption (not implemented here)

Objects are written **in the clear**. The destination abstraction carries a
`KeyProvider` seam — a narrow interface that derives a purpose-scoped key
(`KeyPurposeEncryption`) — so at-rest encryption of destination objects can be
added without changing the layout. It is section 2 of issue #16 (`SECRET_KEY`,
HKDF-SHA256, AES-256-GCM) and is wired by a later change; `internal/backup` takes
no dependency on `internal/secret` and defaults to no encryption.

When encryption lands: an S3 bucket's objects are encrypted under a key derived
from `SECRET_KEY`; the manifest records the key's check value; and encrypted
backups start only after an admin confirms keeping a copy of `SECRET_KEY` off the
server. The key must be backed up **separately from the database dumps** — like a
CA private key — or the ciphertext and the key travel together by accident.
**Key loss is unrecoverable**: encrypted backups become unreadable and the
affected credentials must be re-entered.

## Tools

`pg_dump` and `pg_restore` run on the host. `backup.FindTools` prefers the newest
`/usr/lib/postgresql/<major>/bin` and falls back to `PATH`; `psql` beside them
reads the inventory. A `pg_dump` older than the database server is refused.

## Commands

```
api --backup                       take a manual backup and exit   # make backup
api --backup-list                  list backups and exit           # make backup-list
api --backup-restore <id>          restore the backup in full and exit
```

They need `DATABASE_URL`, `pg_dump`/`pg_restore` on the host, and
`HAAKU_BACKUP_DIR` (default `./backups`). `HAAKU_BACKUP_KEEP` sets the
retention count; `HAAKU_BACKUP_REQUIRE_TABLES` (default `users`) sets the tables
whose absence fails a backup. `make backup` / `make backup-list` wrap the first
two.

## Not done yet

- The admin API and Settings UI page (list, "back up now", restore, retention
  config, encryption status) — a separate change, with `internal/httpapi` and
  `web/`.
- S3-compatible storage (declared; `ErrS3NotImplemented`).
- Encryption of destination objects (`KeyProvider` seam; section 2 of issue #16).
- Selective/partial restore (this change is full restore only).
- Wiring the pre-migration safety copy into the migration runner (issue #14).
