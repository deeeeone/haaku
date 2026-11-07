# The public API contract

Machine-checkable records of what clients (the Jellyfin plugin, the web UI,
third parties) are allowed to rely on.

| File | What it is | Who writes it |
|---|---|---|
| `openapi-baseline.yaml` | A frozen copy of `openapi.yaml` — the contract as it stood at the last accepted release. | A maintainer, deliberately (see below). |
| `breaking-changes.yaml` | The acknowledgement list: every breaking change to the baseline, with a date and a one-line reason. | Whoever makes the break, in the same pull request. |

## The gate

`make contract-diff` (and the `openapi` CI job) compares
`openapi-baseline.yaml` against the working tree's generated `openapi.yaml` and
classifies every difference:

- **breaking** — removed or renamed path; removed or renamed response field;
  newly-required request field; narrowed enum; changed type; removed enum value;
  removed status code. Any of these that is *not* listed in
  `breaking-changes.yaml` fails the build.
- **additive** — new path, new optional field, new enum value, new status code.
  Always allowed; reported but never fatal.

The gate does not regenerate anything. `openapi.yaml` stays a generated artifact
(`make api-spec`); this tool only reads it.

## When to update the baseline

Update `openapi-baseline.yaml` **when you accept a set of contract changes into a
release** — i.e. when the release notes will say "this is what `/v1` is now". It
is a deliberate, reviewable act, not a step of the build:

```bash
# after the release's contract changes have been reviewed and are settled
cp openapi.yaml contracts/api/v1/openapi-baseline.yaml
```

Concretely:

- **Additive changes** may be folded into the baseline at any release boundary;
  no acknowledgement is needed because the gate never objects to them.
- **Breaking changes** must first be acknowledged in `breaking-changes.yaml`
  (so the gate is green) *and* reviewed as a break. A breaking change inside
  `/v1` is against policy — the versioning rule is that a break means a new
  prefix, `/v2` (see `docs/architecture/api-versioning.md`). If a break lands in
  `/v1` anyway, the acknowledgement is where a reviewer sees it and pushes back.
- Once a break is part of the baseline it no longer appears in the diff, so its
  acknowledgement entry may be deleted at the same time. The file records
  pending breaks; the git history of `openapi-baseline.yaml` records accepted
  ones.

Do not update the baseline to make the gate pass. If the gate is red, either the
change is a bug to revert, or it is a break that belongs in
`breaking-changes.yaml`.
