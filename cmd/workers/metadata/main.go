package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"trakt-api/external/tmdb"
	"trakt-api/internal/config"
	"trakt-api/internal/database"
	"trakt-api/internal/logging"
	"trakt-api/internal/metadata"

	"gorm.io/gorm"
)

func main() {
	// Parse CLI flags.
	mode := flag.String("mode", "backfill", "Worker mode: backfill or sync")
	pages := flag.Int("pages", 5, "Number of pages to fetch in backfill mode")
	flag.Parse()

	// Load configuration.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("[metadata-worker] failed to load config", "error", err)
		os.Exit(1)
	}
	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)

	logger.Info("[metadata-worker] starting", "env", cfg.Env, "mode", *mode)

	// Initialize database.
	db, err := database.InitDB()
	if err != nil {
		logging.Fatal(logger, "[metadata-worker] failed to init DB", "error", err)
	}
	sqlDB, _ := db.DB()
	if sqlDB != nil {
		defer func() { _ = sqlDB.Close() }()
	}

	// Initialize TMDB client.
	tmdbClient, err := tmdb.New()
	if err != nil {
		logging.Fatal(logger, "[metadata-worker] failed to create TMDB client", "error", err)
	}

	// Run in a cancellable context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		logger.Info("[metadata-worker] received shutdown signal")
		cancel()
	}()

	// Execute chosen mode.
	switch *mode {
	case "backfill":
		if err := runBackfill(ctx, tmdbClient, db, *pages); err != nil {
			logger.Error("[metadata-worker] backfill failed", "error", err)
			os.Exit(1)
		}
	case "sync":
		sinceFile := os.Getenv("METADATA_SINCE_FILE")
		if sinceFile == "" {
			sinceFile = ".hermes/metadata_sync_since.txt"
		}
		if err := runSync(ctx, tmdbClient, db, sinceFile); err != nil {
			logger.Error("[metadata-worker] sync failed", "error", err)
			os.Exit(1)
		}
	default:
		logger.Error("[metadata-worker] unknown mode", "mode", *mode)
		os.Exit(1)
	}

	logger.Info("[metadata-worker] finished successfully")
}

// runBackfill fetches popular movies, TV shows, and trending items from TMDB
// and ingests them into the local media cache.
func runBackfill(ctx context.Context, c *tmdb.Client, db *gorm.DB, maxPages int) error {
	slog.Info("[backfill] starting", "pages", maxPages)

	// 1. Backfill popular movies.
	if err := backfillPopularMovies(ctx, c, db, maxPages); err != nil {
		slog.Error("[backfill] movie backfill error", "error", err)
	}

	// 2. Backfill popular TV shows.
	if err := backfillPopularTV(ctx, c, db, maxPages); err != nil {
		slog.Error("[backfill] TV backfill error", "error", err)
	}

	// 3. Backfill trending (all types).
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
			if _, err := metadata.IngestMovie(db, movie); err != nil {
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

			// Ingest the show.
			dbShow, err := metadata.IngestShow(db, show)
			if err != nil {
				slog.Error("[backfill] show ingest failed, skipping",
					"tmdb_id", item.ID, "title", show.Name, "error", err)
				skipped++
				continue
			}

			// Fetch full season details (including episodes) for each season.
			for _, seasonSummary := range show.Seasons {
				if seasonSummary.SeasonNumber == 0 {
					continue // Skip specials season for backfill efficiency.
				}
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

				// Ingest the season record.
				_, err = metadata.IngestSeason(db, dbShow.ID, show.ID, seasonSummary)
				if err != nil {
					slog.Error("[backfill] season ingest failed",
						"show_id", dbShow.ID, "season", seasonSummary.SeasonNumber, "error", err)
					continue
				}

				// Batch ingest episodes.
				if len(seasonDetail.Episodes) > 0 {
					if err := metadata.IngestEpisodesBatch(db, dbShow.ID, dbShow.ID,
						seasonDetail.Episodes); err != nil {
						slog.Error("[backfill] episode batch ingest failed",
							"show_id", dbShow.ID, "season", seasonSummary.SeasonNumber, "error", err)
					}
				}
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
					if _, err := metadata.IngestMovie(db, movie); err != nil {
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
					if _, err := metadata.IngestShow(db, show); err != nil {
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

// runSync runs the change-feed sync mode.
func runSync(ctx context.Context, c *tmdb.Client, db *gorm.DB, sinceFile string) error {
	slog.Info("[sync] starting change feed sync", "since_file", sinceFile)
	return metadata.RunSync(c, db, sinceFile)
}
