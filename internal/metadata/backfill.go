package metadata

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/haaku-tv/haaku/external/tmdb"

	"gorm.io/gorm"
)

// This file holds the TMDB upkeep work that the retired cmd/workers/metadata
// binary used to run: a bulk backfill of popular/trending titles, and the
// change-feed sync in sync.go. Both are now driven by the one binary's worker
// mode (HAAKU_WORKER=metadata), so the schedule lives in one place and there
// is no second main to keep in step.

// SinceFile returns the change-feed cursor path: $METADATA_SINCE_FILE, or
// .hermes/metadata_sync_since.txt relative to the working directory (which is
// /config/haaku in the omnibus image, so the cursor survives an update with
// the rest of the config directory).
func SinceFile() string {
	if v := strings.TrimSpace(os.Getenv("METADATA_SINCE_FILE")); v != "" {
		return v
	}
	return ".hermes/" + SinceFileName
}

// BackfillPages returns how many pages of popular titles one backfill pass
// walks: $METADATA_BACKFILL_PAGES, or 5. Trending uses a fixed two pages.
func BackfillPages() int {
	if v := strings.TrimSpace(os.Getenv("METADATA_BACKFILL_PAGES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 5
}

// Backfill refreshes the local media cache from TMDB's popular and trending
// lists. It is idempotent (ingest upserts), so a re-run costs API calls but
// changes nothing it has already seen.
func Backfill(ctx context.Context, c *tmdb.Client, db *gorm.DB, maxPages int) error {
	if c == nil || db == nil {
		return nil
	}
	slog.Info("[backfill] starting", "pages", maxPages)

	// Each stage logs its own failures and continues: one bad page must not
	// abandon the rest of the backfill.
	if err := backfillPopularMovies(ctx, c, db, maxPages); err != nil {
		slog.Error("[backfill] movie backfill error", "error", err)
	}
	if err := backfillPopularTV(ctx, c, db, maxPages); err != nil {
		slog.Error("[backfill] TV backfill error", "error", err)
	}
	if err := backfillTrending(ctx, c, db, 2); err != nil {
		slog.Error("[backfill] trending backfill error", "error", err)
	}

	slog.Info("[backfill] complete")
	return nil
}

// backfillPopularMovies fetches and ingests popular movies.
func backfillPopularMovies(ctx context.Context, c *tmdb.Client, db *gorm.DB, maxPages int) error {
	slog.Info("[backfill] fetching popular movies", "pages", maxPages)

	total := 0
	skipped := 0
	for page := 1; page <= maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		result, err := c.MoviePopular(page)
		if err != nil {
			slog.Error("[backfill] movie popular page fetch failed", "page", page, "error", err)
			continue
		}

		for _, item := range result.Results {
			if err := ctx.Err(); err != nil {
				return err
			}

			// Rate limit: 250ms between detail API calls.
			time.Sleep(250 * time.Millisecond)

			// Fetch full movie detail.
			movie, err := c.GetMovie(item.ID)
			if err != nil {
				slog.Error("[backfill] movie detail fetch failed, skipping",
					"tmdb_id", item.ID, "error", err)
				skipped++
				continue
			}

			// Ingest the movie.
			if _, err := IngestMovie(db, movie, nil); err != nil {
				slog.Error("[backfill] movie ingest failed, skipping",
					"tmdb_id", item.ID, "title", movie.Title, "error", err)
				skipped++
				continue
			}
			total++
		}

		slog.Info("[backfill] movie page done",
			"page", page, "items", len(result.Results), "total_ingested", total)

		// Rate limit between pages.
		time.Sleep(250 * time.Millisecond)
	}

	slog.Info("[backfill] movies complete", "total", total, "skipped", skipped)
	return nil
}

// backfillPopularTV fetches and ingests popular TV shows with full seasons/episodes.
func backfillPopularTV(ctx context.Context, c *tmdb.Client, db *gorm.DB, maxPages int) error {
	slog.Info("[backfill] fetching popular TV shows", "pages", maxPages)

	total := 0
	skipped := 0
	for page := 1; page <= maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		result, err := c.TVPopular(page)
		if err != nil {
			slog.Error("[backfill] TV popular page fetch failed", "page", page, "error", err)
			continue
		}

		for _, item := range result.Results {
			if err := ctx.Err(); err != nil {
				return err
			}

			// Rate limit.
			time.Sleep(250 * time.Millisecond)

			// Fetch full show detail (includes season summaries).
			show, err := c.GetShow(item.ID)
			if err != nil {
				slog.Error("[backfill] show detail fetch failed, skipping",
					"tmdb_id", item.ID, "error", err)
				skipped++
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}

			// Fetch every season's episodes, including season 0: specials are
			// stored (Trakt-shaped clients expect a season 0) and excluded from
			// the show's totals by the ingest, which derives those from the
			// season rows it writes. A season whose fetch fails is skipped
			// rather than failing the show; its absence then shows up honestly
			// in a lower episode count.
			seasons := make([]tmdb.Season, 0, len(show.Seasons))
			for _, seasonSummary := range show.Seasons {
				if err := ctx.Err(); err != nil {
					return err
				}

				// Rate limit.
				time.Sleep(250 * time.Millisecond)

				seasonDetail, err := c.GetSeason(show.ID, seasonSummary.SeasonNumber)
				if err != nil {
					slog.Error("[backfill] season detail fetch failed, skipping",
						"tmdb_show_id", show.ID, "season", seasonSummary.SeasonNumber, "error", err)
					continue
				}
				seasons = append(seasons, *seasonDetail)
			}

			// Ingest the show, its seasons and its episodes in one call. The
			// seasons hang off the SHOW id, which is what makes
			// /shows/{id}/seasons/{n}/episodes and the season-level counts
			// work (an earlier version passed the show id as the season id,
			// putting every episode of every season under one bogus row).
			if _, err := IngestShow(db, show, seasons, nil); err != nil {
				slog.Error("[backfill] show ingest failed, skipping",
					"tmdb_id", item.ID, "title", show.Name, "error", err)
				skipped++
				continue
			}

			total++
		}

		slog.Info("[backfill] TV page done",
			"page", page, "items", len(result.Results), "total_ingested", total)

		// Rate limit between pages.
		time.Sleep(250 * time.Millisecond)
	}

	slog.Info("[backfill] TV shows complete", "total", total, "skipped", skipped)
	return nil
}

// backfillTrending fetches and ingests trending movies and TV shows.
func backfillTrending(ctx context.Context, c *tmdb.Client, db *gorm.DB, maxPages int) error {
	slog.Info("[backfill] fetching trending", "pages", maxPages)

	for _, window := range []string{"day", "week"} {
		for page := 1; page <= maxPages; page++ {
			if err := ctx.Err(); err != nil {
				return err
			}

			result, err := c.Trending("all", window, page)
			if err != nil {
				slog.Error("[backfill] trending fetch failed",
					"window", window, "page", page, "error", err)
				continue
			}

			for _, item := range result.Results {
				if err := ctx.Err(); err != nil {
					return err
				}

				time.Sleep(250 * time.Millisecond)

				switch item.MediaType {
				case "movie":
					movie, err := c.GetMovie(item.ID)
					if err != nil {
						slog.Error("[backfill] trending movie fetch failed",
							"tmdb_id", item.ID, "error", err)
						continue
					}
					if _, err := IngestMovie(db, movie, nil); err != nil {
						slog.Error("[backfill] trending movie ingest failed",
							"tmdb_id", item.ID, "error", err)
					}
				case "tv":
					show, err := c.GetShow(item.ID)
					if err != nil {
						slog.Error("[backfill] trending TV fetch failed",
							"tmdb_id", item.ID, "error", err)
						continue
					}
					// Trending walks the show's own record only; the season
					// walk is the popular-TV stage's job, and doubling it here
					// would re-fetch the same seasons twice per run.
					if _, err := IngestShow(db, show, nil, nil); err != nil {
						slog.Error("[backfill] trending TV ingest failed",
							"tmdb_id", item.ID, "error", err)
					}
				}
			}

			slog.Info("[backfill] trending page done",
				"window", window, "page", page, "items", len(result.Results))
			time.Sleep(250 * time.Millisecond)
		}
	}

	slog.Info("[backfill] trending complete")
	return nil
}
