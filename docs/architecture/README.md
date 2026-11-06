# Design docs

Architecture and design decisions that are too long for a comment and too
specific for the README. One file per subject, named for the subject rather than
the date, so a doc is updated in place as the design changes.

| Doc | Subject |
|---|---|
| [monorepo.md](monorepo.md) | Repository layout: one repo, one binary, one image |
| [api-versioning.md](api-versioning.md) | API versioning, the generated spec, and its gates |
| [omnibus-image.md](omnibus-image.md) | The single Docker image that bundles PostgreSQL and Redis |
| [backups.md](backups.md) | Backups, restore, rotation and the pre-migration safety copy |
| [release-versioning.md](release-versioning.md) | Release, upgrade and rollback policy |

## Writing one

- State the decision, then the reasoning, then what it costs. A doc that only
  argues for the change is a proposal, not a design doc.
- Record the alternatives that were rejected and why, so the next person does
  not re-litigate it.
- Update the doc in the same PR that changes the behaviour. A design doc that
  describes the previous design is worse than no doc.
