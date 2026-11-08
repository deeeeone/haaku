# Migrations

The schema is defined by Goose-annotated SQL in `sql/`, **embedded in the binary**
(`migrations/embed.go`) and applied by the server at startup. A fresh deployment
never serves against an empty database, and no human has to remember to run a
tool. The runner lives in `internal/database/migrate*.go`.

## How it works

- **Tool:** [Goose](https://github.com/pressly/goose) v3, pinned in the Makefile
  (`go run github.com/pressly/goose/v3/cmd/goose@v3.27.1`) and in `go.mod`.
- **Applied at startup:** `cmd/api` calls `database.Migrate` before it listens.
  `HAAKU_MODE=api` and `HAAKU_MODE=migrate` (or `--migrate-only`) apply
  migrations; `HAAKU_MODE=worker` never touches the schema.
- **Under a lock:** the whole sequence — ledger setup, pending check, backup,
  apply — is serialised by a PostgreSQL advisory lock, so two servers starting at
  once do not race. The second waits up to `HAAKU_MIGRATION_WAIT` (default
  `60m`) for the first.
- **With a backup:** before changing a database that already has a schema, the
  runner takes a `pg_dump` (custom format) and **refuses to migrate if it fails**.
  The two newest copies are kept under `HAAKU_MIGRATION_BACKUP_DIR` (default
  `./pre-migrate-backups`). Pass `HAAKU_SKIP_BACKUP_BEFORE_MIGRATE=true` only on
  a dev box without a matching `pg_dump`.
- **Ledger:** Goose records applied versions in `goose_db_version`; the runner
  ensures that table exists before anything is read or applied.

## Rules

- **Leave existing migration files alone.** The `0001`–`0032` files keep their
  original numbers; renumbering them would make a migration that has already run
  look new.
- **New migrations use timestamped names** (`20261105120000_add_x.sql`, via
  `make migrate-create NAME=add_x`) so parallel PRs cannot collide on a number.
- **Never run `goose fix`.** It renames files to sequential numbers, rewriting
  history that the ledger already records.
- **Never edit a merged migration.** The ledger records that a version was
  applied; editing its SQL afterwards is silently skipped, which is worse than a
  loud failure. A change is always a new migration.
- **`-- +goose StatementBegin`/`StatementEnd` around anything that is not a single
  statement** (a `DO` block, a function body): Goose splits on semicolons, and a
  body full of `;` would be cut apart.

## Authoring

```sh
make migrate-create NAME=add_thing   # writes sql/<timestamp>_add_thing.sql
make migrate-validate                # parses/validates every file offline
```

## Applying, inspecting (upgrading runbook)

The server applies migrations on its own; these are for an operator.

```sh
# 1. Back up (the runner does this too — this is belt and braces).
pg_dump --format=custom "$DATABASE_URL" > backup.dump

# 2. Fetch the new image / checkout, then apply and exit.
#    (config.Load requires TMDB_API_KEY even for a migration.)
make migrate-up                       # = go run ./cmd/api --migrate-only

# 3. Start the server and verify: /readyz reports the schema as ready, and the
#    log line "database schema is up to date".
```

`make migrate-status` reads the Goose ledger directly.

### Treating a failure

A migration that fails leaves the schema **not** on the new version and the
server refusing to serve (the process exits non-zero). Do not "fix forward" by
editing the failed file.

### Down-migration policy: forward-only

There is no in-place downgrade. Recovery is: **stop the server, restore the
verified pre-migration `pg_dump`, redeploy the previous image.** This is why the
runner refuses to start without a backup.

Down sections exist only where a rollback is genuinely safe (dropping the tables
or indexes a migration added). A migration that destroys or rewrites data has no
safe down and does not pretend to: the restore above is the rollback. (The old
`.down.sql` files this README once promised never existed; the down section now
lives in the same Goose-annotated file.)

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `HAAKU_MIGRATE_TIMEOUT` | `20m` | How long applying migrations may take. `0` disables the deadline. |
| `HAAKU_MIGRATION_WAIT` | `60m` | How long a server waits for another server's migration sequence. `0` waits forever. |
| `HAAKU_MIGRATION_BACKUP_DIR` | `./pre-migrate-backups` | Where pre-migration dumps are written. |
| `HAAKU_SKIP_BACKUP_BEFORE_MIGRATE` | unset | `true` to migrate without a backup (dev boxes only). |
| `HAAKU_PG_DUMP` | `pg_dump` | Override the `pg_dump` binary path. |

A deployment that raises `HAAKU_MIGRATE_TIMEOUT` must raise the container's
`HEALTHCHECK --start-period` to match, so a migrating server is not restarted
mid-migration.

## Tests

The migration tests in `internal/database/migrate_test.go` run against a **real
PostgreSQL** and skip with a reason when `DATABASE_URL` is unset. They cover the
fresh schema, idempotency, two concurrent runs, and a failed migration leaving
its backup in place:

```sh
make pg-up
export DATABASE_URL="$(./scripts/dev-pg.sh start -q)"
go test ./internal/database/ -run TestMigrate -v
```
