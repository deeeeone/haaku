# Media metadata: TMDB primary, TVDB supplementary

haaku keeps a local catalogue of the movies and shows it has been asked about.
That catalogue is filled from two providers with a deliberate, fixed division of
labour.

| | **TMDB (primary)** | **TheTVDB (supplementary)** |
|---|---|---|
| Identity | `tmdb_id` is the media identity — the id the ingest is keyed on and the id a live search hit carries | `tvdb_id` is a link carried alongside it, for the TVDB-keyed clients and media servers |
| Text | title, original title, overview, tagline, genres, status, network, certification, runtime | fills a gap only when TMDB has none |
| Structure | seasons and episodes | — |
| Artwork | poster, backdrop — TMDB's path-only CDN joined into absolute URLs, stored as `images.poster` / `images.backdrop` | kept as `tvdb_poster` / `tvdb_backdrop`, and promoted into `images.poster` only when TMDB has none |
| Numbers | `rating_avg` / `rating_count` (vote_average / vote_count), popularity, adult | `score` is kept in `metadata.tvdb` — it is not a 0-10 rating and never overwrites `rating_avg` |
| Extra | trailer (videos), certification (release dates / content ratings), IMDb id | trailer and certification fallbacks, the slug, and the clear logo |

**TMDB is required, TVDB is optional.** Without `TVDB_API_KEY` every resolve still
succeeds on TMDB alone; the tvdb id link and the supplement's fallbacks are simply
absent, and `tvdb_id` stays `0`. **`tvdb_id = 0` means "unknown", not "title 0"** —
the unique indexes on `tmdb_id`/`tvdb_id` are *partial* (`WHERE … > 0`) so any
number of titles may carry an unknown link id.

## Why TMDB is primary

It is the provider with the deeper catalogue. Its detail response carries cast and
crew, watch providers, per-country release dates and certifications, and videos —
the fields a Trakt-shaped client expects — and `append_to_response` fetches the
external ids, the release dates and the videos in the SAME call as the title. TVDB
has the smaller record for the same title.

## The link runs both ways, and is not symmetric

That asymmetry is a measured property of the two APIs, not a choice:

| direction | how | cost |
|---|---|---|
| TVDB shows are found from TMDB | `GET /tv/{id}/external_ids` names `tvdb_id` outright | free (appended to the detail call) |
| TVDB movies are found from TMDB | TVDB's own remote-id search: `GET /search/remoteid/{imdb_id}` | one TVDB call |

TMDB's **movie** external-ids payload carries no `tvdb_id` — measured against the
live API: `GET /movie/603/external_ids` returns `imdb_id` and the social handles
only, while `GET /tv/1396/external_ids` returns `tvdb_id 81189`. So a movie's
supplementary link is recovered through TVDB's remote-id search using the IMDb id.
Every one of these calls is a debug line on failure and never fatal: the
supplement is optional by definition.

## How the app uses it

1. `GET /v1/search?query=…` answers from the **local catalogue first**, then fills
   the rest of the page from **TMDB's live search**. Live hits carry
   `source: "tmdb"`, `ids.tmdb`, and `in_catalog: false`. A single-type search
   (`type=movie` or `type=show`) uses `/search/movie` or `/search/tv`; a combined
   one uses `/search/multi`, because `/search/multi` takes no type filter and
   filtering a short page client-side would tell the caller it was full.
2. `GET /v1/media/resolve?type=movie|show&id=<id>&id_type=tmdb|tvdb|imdb`
   ingests the title — the TMDB record, its seasons and episodes, plus the TVDB
   supplement — and returns the canonical row in the same shape
   `/v1/movies/{id}` returns. It is **idempotent**: a title already in the
   catalogue is returned with no provider call. Authentication is required
   because it writes. `id_type` defaults to **tmdb**; a `tvdb` or `imdb` id is
   mapped to its TMDB record through `/find` first, so a TVDB-keyed client keeps
   working.
3. Everything else (watchlist, watching, ratings, comments) addresses media by
   its local row. Those endpoints resolve incoming ids through
   `database.ResolveIDRefs`, which accepts **tmdb, tvdb or imdb** — preferring
   tmdb — and reads both the media columns and the `external_ids` table.

That last point is load-bearing: rows ingested by the older TMDB-only path carry
their ids on the row and have **no `external_ids` entry**, so a resolution that
only read `external_ids` reported them not-found. A watchlist add for every
backfilled title used to fail this way.

## TVDB is not searched

A live search hit has to be ingestable, and with `tmdb_id` as the identity a
TVDB-only record has no destination: there is no row to key it on. A card that
cannot be opened is worse than no card, so `/v1/search` queries TMDB alone and
drops the `person` hits `/search/multi` returns until issue #68 lands person
ingestion.

## Slugs

TMDB names no slug, so a row's `slug` (the value `/movies/{slug}` and
`/shows/{slug}` are addressed by) comes from, in order: the TVDB supplement's
slug, the slug the row already carries, or one derived from the title. Derivation
is collision-checked against `external_ids`, because that table's
`UNIQUE (media_type, source, external_id)` key is upserted on every ingest — two
titles deriving the same slug would make the second one take the slug away from
the first and break its URL.

## What this costs

- **A season walk, not one call.** TMDB has no "all episodes" endpoint, so
  resolving a show costs one request per season (`/tv/{id}/season/{n}`, season 0
  included). TVDB served a whole series in two calls. A skipped season is logged,
  and the show's counts are then derived from the seasons actually stored, so the
  totals cannot claim episodes the rows do not hold.
- **No absolute episode numbers.** `episodes.absolute_number` was a TVDB concept;
  TMDB does not carry it, so the column stays NULL.
- **TVDB-only titles are no longer ingestable.** With `tmdb_id` as the identity
  there is no row to key one on. That is the deliberate cost of the flip.

## Addressed on v3, not v4

`external/tmdb` talks to `api.themoviedb.org/3` with the v4 bearer token. The
detail/search routes this package needs (`/movie/{id}`, `/tv/{id}`,
`/search/multi`, `/configuration`, `/tv/popular`) **404 on the /4 tree** for this
project's token — measured: `/4/movie/603`, `/4/tv/1396`, `/4/search/multi`,
`/4/configuration` all return `{"status_code":34}` while `/3` returns 200. The
client used to address /4, so the metadata worker's backfill and change-feed sync
imported nothing and the catalogue stayed empty. `external/tmdb`'s tests pin the
version and the paths so it cannot drift back.

`external/tvdb` talks to `api4.thetvdb.com/v4`, caching its login token for 12 h
and refreshing it once on a 401. Its search `type` parameter is single-valued — a
`type=movie,series` request matches nothing and answers an empty page.

## Not in this pass

- **Person and credits ingestion.** TMDB supplies it, the schema (`people`,
  `credits`) and the writers (`IngestPerson`, `IngestCredits`) exist, but nothing
  calls them from the resolve path yet — issue #68 owns it. Person search hits are
  therefore still dropped.
- **Episode-level search.** Search covers movies and shows.
- **Non-English metadata.** Names and overviews are requested in the provider's
  default locale (English); artwork selection prefers English where a provider
  labels it.
- **Season-level credits, and artwork beyond the season poster.**
