# Media metadata: TVDB primary, TMDB supplementary

haaku keeps a local catalogue of the movies and shows it has been asked about.
That catalogue is filled from two providers with a deliberate, fixed division of
labour — the same split theintrodb-api uses.

| | **TheTVDB (primary)** | **TMDB (supplementary)** |
|---|---|---|
| Identity | `tvdb_id` is the media identity — the id clients and media servers speak | `tmdb_id` + `imdb_id` are links carried alongside it |
| Text | title, overview, tagline (movies: from the translation record), genres, status, network, certification | fills a gap only when TVDB has none |
| Structure | seasons and episodes, in TVDB's `default` (official/aired) order | — |
| Artwork | poster, background, clear logo — **absolute URLs**, stored as `images.poster` / `images.backdrop` / `images.logo` | `poster_path` / `backdrop_path` kept raw, and joined into `images.poster` only when TVDB has no artwork |
| Numbers | `score` (kept in `metadata.tvdb`, not a rating) | `rating_avg` / `rating_count` (vote_average / vote_count), popularity |
| Extra | trailer URL | tagline fallback, adult flag |

A TVDB record with no TMDB twin ingests fine: `tmdb_id` stays `0` and every
TMDB-sourced number keeps its zero value. **`tmdb_id = 0` means "unknown", not
"movie 0"** — the unique indexes on `tmdb_id`/`tvdb_id` are *partial*
(`WHERE … > 0`) so any number of titles may carry an unknown id.

## The link runs both ways

TVDB's `remoteIds` normally name the TMDB record (`TheMovieDB.com`), and that is
the preferred link. When a record has none, `external/tmdb.Find` asks TMDB which
record carries the TVDB id (`/find/{id}?external_source=tvdb_id`). Both halves
are logged at debug level and neither is fatal.

## How the app uses it

1. `GET /v1/search?query=…` answers from the **local catalogue first**, then
   fills the rest of the page from TVDB's live name search. Live hits carry
   `source: "tvdb"`, their ids, and `in_catalog: false`.
2. `GET /v1/media/resolve?type=movie|show&id=<id>&id_type=tvdb|tmdb|imdb`
   ingests the title — TVDB record, its seasons and episodes, plus the TMDB
   supplement — and returns the canonical row in the same shape
   `/v1/movies/{id}` returns. It is **idempotent**: a title already in the
   catalogue is returned with no provider call. Authentication is required
   because it writes.
3. Everything else (watchlist, watching, ratings, comments) addresses media by
   its local row. Those endpoints resolve incoming ids through
   `database.ResolveIDRefs`, which accepts **tvdb, tmdb or imdb** — preferring
   tvdb — and reads both the media columns and the `external_ids` table.

That last point is load-bearing: rows ingested by the old TMDB-only path carry
their ids on the row and have **no `external_ids` entry**, so a resolution that
only read `external_ids` reported them not-found. A watchlist add for every
backfilled title used to fail this way.

## TMDB is addressed on v3

`external/tmdb` talks to `api.themoviedb.org/3` with the v4 bearer token. The
detail/search routes this package needs (`/movie/{id}`, `/tv/{id}`,
`/search/multi`, `/configuration`, `/tv/popular`) **404 on the /4 tree** for this
project's token — measured: `/4/movie/603`, `/4/tv/1396`, `/4/search/multi`,
`/4/configuration` all return `{"status_code":34}` while `/3` returns 200. The
client used to address /4, so the metadata worker's backfill and change-feed sync
imported nothing and the catalogue stayed empty. `external/tmdb`'s tests pin the
version and the paths so it cannot drift back.

## Not in this pass

- **Person and list ingestion.** TVDB's search returns people; there is nothing
  local to ingest them into yet, so those hits are dropped rather than shown as
  cards with no destination.
- **Episode-level search.** TVDB's name search covers movies and series only.
- **Non-English metadata.** Names/overviews are requested in `eng`; artwork
  selection prefers English, then the highest TVDB score.
- **Season artwork.** Season rows are derived from the episode list (TVDB's
  season list carries no episode counts), so season posters are not fetched.
- **TMDB-only installs.** `TVDB_API_KEY` is optional: without it the
  TVDB-backed live search and resolve paths report the provider unavailable and
  the API answers from the local catalogue (which the TMDB backfill populates).