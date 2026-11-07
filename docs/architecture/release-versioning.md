# Release, upgrade and rollback policy

haaku is pre-release: there is no release process and no release tag yet.
This document states the policy the first release must follow, because the pivot
to a bundled-database image (issue #13) makes us the ones shipping a database —
and a self-hosted product that cannot be safely upgraded cannot be shipped.

## Version format

Release tags follow [Semantic Versioning](https://semver.org/) with a leading
`v`:

- Stable release: `vMAJOR.MINOR.PATCH`, for example `v1.2.0`
- Prerelease: `vMAJOR.MINOR.PATCH-SUFFIX`, for example `v0.1.0-alpha.1`
- Build metadata: append `+IDENTIFIER`, for example `v1.2.0+build.7`

For releases below `v1.0.0`, a minor release may include breaking changes. Every
release note must call out upgrade, configuration and compatibility impact. An
API-breaking change is still reviewed rather than silent: `make contract-diff`
requires each one to be written down in `contracts/api/v1/breaking-changes.yaml`
before it can land (see [api-versioning.md](api-versioning.md)).

The Git tag and its matching GitHub Release are the **authoritative product
version**. Other version-like values in the repository describe dependencies,
protocols or compatibility targets, and are not product versions.

## Container tags are build identity, not versions

Container tags identify a build; they carry **no compatibility or support
promise**:

| Tag | Meaning |
| --- | --- |
| `build-N` | Ordered build identifier. A larger number is a newer published build; gaps from unsuccessful or non-publishing runs are expected. |
| `latest` | Mutable pointer updated by successful default-branch publications. |
| `<sha>` | Exact source identity for the build. |

Build numbers are not release versions. Pin a `build-N`, a commit-SHA tag, or an
image digest when a deployment must not move with `latest`.

## Backups and rollback

Administrators take a backup before every release upgrade and **verify that it can
be restored** ([backups.md](backups.md#verifying-a-restore)). This applies to
every upgrade, not only to major versions.

The server also takes its own database safety copy before it applies migrations
at startup ([before upgrades](backups.md#pre-migration-safety-copy)). That copy
holds the database only, keeps the two newest, and sits at the backup
destination. It does **not** replace the administrator's verified backup, which
should also include the files and configuration, and be kept off the server.

### There is no in-place downgrade

**A newer release may write or migrate data that an older release cannot read, and
there is no supported in-place database downgrade.** Recovery from a failed
upgrade is:

1. **Stop** the new version.
2. **Restore** the verified pre-upgrade backup — *and its matching
   configuration*. A restore with the wrong configuration is not a recovery.
3. **Redeploy the previous image.**

State this plainly: **that restore discards everything since the backup was
taken.** Every change made after the backup — new watch history, new users, new
lists — is gone. There is no partial rollback and no way to merge the two.

A configuration file is part of the recovery point, not an afterthought: if a
release adds required configuration, the previous image is only recoverable with
the configuration it ran with.

### Breaking version bumps

A load-bearing version bump is tagged as breaking in the release notes.
**`PG_MAJOR` in particular** (the bundled PostgreSQL major, issue #13): a
PostgreSQL major upgrade cannot be undone by swapping the image back, because the
data directory is rewritten for the new major. Treat it as a one-way door and
name it in the release notes.

## Release notes

Release notes are authoritative for version-specific compatibility. They must
state, when applicable:

- the supported **upgrade path** and any required intermediate release;
- **configuration** the release adds, changes or removes;
- the **compatibility** impact (schema, `PG_MAJOR`, API surface).

Treat a combination the release notes do not certify as unsupported. Release
notes are not a changelog; a changelog alone is not release notes.
