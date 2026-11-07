# Design docs

One file per feature group. These are the **design** record — the *why* and the shape we agreed on. They are not the implementation log (that is the audit `*.md` beside each package) and not the inventory (`docs/parity/parity.yaml`). When a design changes, edit the file here; when code lands, link the PR from the relevant issue, not from here.

Written for the two people who will read them: whoever implements the feature next, and Pas.

## Index

| # | File | Covers | Status |
|---|---|---|---|
| 01 | [identity-and-network.md](01-identity-and-network.md) | Accounts (local + Clerk), the config toggles, and the hosted social network that self-hosted instances piggyback | **decided** |
| 02 | scrobble-and-sync.md | The scrobble loop: client → instance → history/progress, the sync session engine, `last_activities`, webhooks/outbox | planned |
| 03 | lists.md | Lists and list items: ownership, ordering, visibility, smart lists | planned |
| 04 | tracking.md | History, collection, watchlist, ratings, progress — the personal tracker's core | planned |
| 05 | stats.md | Per-instance aggregates (watchers/plays/collectors), `trending` vs `popular`, personal stats | planned |
| 06 | metadata-and-import.md | TMDB ingest/change-feed, search coverage, external IDs, the Trakt importer | planned |
| 07 | operations.md | Self-hosted lifecycle: migrations, backups/restore, upgrades, secret encryption, observability | planned |

## Rules for this folder

- **Decisions, not optimism.** A doc says what we decided and what we deliberately did not decide; unresolved questions get an explicit "Open questions" section, never a silent assumption.
- **Name the owner of each open question** (either "Pas decides" or the issue that tracks it).
- **One feature group per file.** If a doc starts covering two unrelated systems, split it.
- Cross-link: this folder links to issues and to `docs/architecture/` (which describes how things are wired), never the reverse — architecture docs stay implementation-facing.
