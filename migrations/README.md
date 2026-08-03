# Migrations

Append-only SQL migrations, numbered: `0001_<name>.sql` (up) + `0001_<name>.down.sql` (down).

- **Tooling:** golang-migrate or goose (decision T0.4 — pick one, wire into Makefile + CI).
- **Rules:** never edit a merged migration; a schema change = a NEW migration. Conflict on numbering = rebase renumbers.
- **Conventions:** every migration updates `../trakt-killer-plan/02-architecture/data-model.md` FIRST (schema rule).
- Dev: `make compose-up` provides Postgres at `postgres://trakt:***@localhost:5432/trakt`.
