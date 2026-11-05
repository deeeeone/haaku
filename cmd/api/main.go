package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"trakt-api/internal/backup"
	"trakt-api/internal/config"
	"trakt-api/internal/database"
	"trakt-api/internal/httpapi"
	"trakt-api/internal/logging"
	"trakt-api/internal/mode"
	"trakt-api/internal/obs"
	"trakt-api/internal/openapi"
	"trakt-api/internal/outbox"
	"trakt-api/internal/workers"
	"trakt-api/redis"

	"github.com/robfig/cron/v3"
)

func main() {
	os.Exit(run())
}

// run is main's body, returning the process exit code so that deferred cleanup
// (database and Redis handles) still runs. A bare os.Exit in main would skip
// those defers.
func run() int {
	migrateOnly := flag.Bool("migrate-only", false,
		"apply pending schema migrations and exit (equivalent to "+mode.EnvVar+"=migrate)")
	backupNow := flag.Bool("backup", false, "take a manual database backup and exit")
	backupList := flag.Bool("backup-list", false, "list database backups and exit")
	backupRestore := flag.String("backup-restore", "", "restore the backup with this id and exit")
	backupDir := flag.String("backup-dir", "", "backup destination directory (default $TKILLER_BACKUP_DIR or ./backups)")
	flag.Parse()

	serverMode, err := mode.FromEnv()
	if err != nil {
		slog.Error("invalid server mode", "error", err)
		return 2
	}
	if *migrateOnly {
		serverMode = mode.Migrate
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return 1
	}

	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)

	logger.Info("starting trakt-api", "env", cfg.Env, "port", cfg.Port, "mode", serverMode.String())

	// Database (T0.4)
	if cfg.DatabaseURL != "" {
		if _, err := database.InitDB(); err != nil {
			logging.Fatal(logger, "database init failed", "error", err)
		}
		defer func() {
			if err := database.CloseDB(); err != nil {
				logger.Error("database close error", "error", err)
			}
		}()
	} else {
		logger.Warn("DATABASE_URL not set — running without persistence")
		if serverMode == mode.Migrate {
			logger.Error("migration requested without DATABASE_URL")
			return 1
		}
	}

	// Backup commands (issue #16): one-shot, then exit. They use the database and
	// the backup destination and never start a listener.
	if *backupNow || *backupList || *backupRestore != "" {
		return runBackupCommand(logger, cfg, backupCommand{
			take:    *backupNow,
			list:    *backupList,
			restore: *backupRestore,
			dir:     *backupDir,
		})
	}

	// Migrate-and-exit mode stops here: no Redis, no workers, no listener.
	//
	// Only this mode and the API mode may change the schema, and the API applies
	// migrations before it serves; a worker never does (see internal/mode).
	if serverMode == mode.Migrate {
		return migrate(logger)
	}

	// Redis (T0.7 + T0.8: circuit breaker, single-flight, version counters)
	if cfg.RedisURL != "" {
		if err := redis.InitRedis(); err != nil {
			logger.Warn("redis unavailable — running without cache", "error", err)
		}
		defer redis.CloseRedis()
	}

	// Observability listeners (issue #9). Off unless TKILLER_METRICS_LISTEN is set.
	if err := obs.Start(context.Background()); err != nil {
		logger.Warn("observability listeners unavailable", "error", err)
	}

	// Background workers (T0.9)
	// - Webhooks: reads events_outbox rows via cron and delivers webhooks
	// - Outbox publisher: polls events_outbox → Redis Streams "events"
	// - Activity worker: consumes "events" → activity_feed fan-out
	//
	// A worker mode runs exactly these and nothing else, so one image can serve
	// every role.
	if serverMode.RunsWorkers() {
		workers.StartWebhookWorker()
		outbox.StartPublisher(redis.GetClient())
		workers.StartActivityWorker()
	}

	if !serverMode.ServesHTTP() {
		logger.Info("worker mode — no HTTP listener")
		waitForShutdown(logger)
		return 0
	}

	app := httpapi.NewServer(cfg)

	// Code-first OpenAPI generator (T0.6 — non-fatal, TIDB pattern)
	if err := openapi.GenerateYAML("openapi.yaml"); err != nil {
		logger.Warn("failed to generate openapi.yaml", "error", err)
	}

	// Cron jobs (T0.9 — TIDB pattern: UTC, panic recovery, startup refresh)
	setupCronJobs(logger)

	// TODO(T0.9): Worker entrypoints in cmd/workers/ — run as separate containers:
	// metadata-worker, webhooks-worker, stats-worker, import-worker, notify-worker

	go func() {
		logger.Info("trakt-api listening", "port", cfg.Port)
		if err := app.Listen(":" + cfg.Port); err != nil {
			logging.Fatal(logger, "server start failed", "error", err)
		}
	}()

	waitForShutdown(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(ctx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
	}
	logger.Info("server stopped gracefully")
	return 0
}

// migrate applies pending migrations and reports the outcome as an exit code.
func migrate(logger *slog.Logger) int {
	if err := database.Migrate(context.Background()); err != nil {
		if errors.Is(err, database.ErrMigrationsNotImplemented) {
			logger.Error("migrations are not available yet", "error", err)
		} else {
			logger.Error("migration failed", "error", err)
		}
		return 1
	}
	logger.Info("migrations applied")
	return 0
}

// waitForShutdown blocks until SIGINT or SIGTERM.
func waitForShutdown(logger *slog.Logger) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutting down", "signal", sig.String())
}

// setupCronJobs initializes cron with UTC timezone and panic recovery.
// Pattern: theintrodb-api main.go setupCronJobs().
func setupCronJobs(logger *slog.Logger) {
	c := cron.New(cron.WithLocation(time.UTC))

	// TODO(Phase 3): refresh trending rankings every 15m
	// _, err := c.AddFunc("*/15 * * * *", refreshTrendingRankings)

	// TODO(Phase 1): daily TMDB change-feed poll
	// _, err := c.AddFunc("0 3 * * *", refreshMetadataCache)

	// Start the scheduler; jobs are added by domain agents as phases land.
	c.Start()
	logger.Info("cron scheduler started")
}

// recoverFromPanic logs and recovers from goroutine panics.
func recoverFromPanic(logger *slog.Logger, context string) {
	if r := recover(); r != nil {
		logger.Error("panic recovered", "context", context, "panic", r)
	}
}

// backupCommand is the parsed one-shot backup flags.
type backupCommand struct {
	take    bool
	list    bool
	restore string
	dir     string
}

// runBackupCommand builds the backup service and runs one command: take a manual
// backup, list backups, or restore one in full. It reports the outcome as an
// exit code. Configuration is env-only, like the rest of the server.
func runBackupCommand(logger *slog.Logger, cfg *config.Config, cmd backupCommand) int {
	ctx := context.Background()
	if cfg.DatabaseURL == "" {
		logger.Error("backup commands need DATABASE_URL")
		return 1
	}
	dir := cmd.dir
	if dir == "" {
		dir = envDefault("TKILLER_BACKUP_DIR", "./backups")
	}
	destination, err := backup.NewLocalDestination(dir)
	if err != nil {
		logger.Error("backup destination unavailable", "error", err, "dir", dir)
		return 1
	}
	locker, err := backup.NewLocalLocker(dir)
	if err != nil {
		logger.Error("backup lock unavailable", "error", err)
		return 1
	}
	requireTables := splitCSV(envDefault("TKILLER_BACKUP_REQUIRE_TABLES", "users"))
	dumper, err := backup.NewPostgresDumper(ctx, cfg.DatabaseURL, requireTables)
	if err != nil {
		logger.Error("backup needs pg_dump and pg_restore on this host", "error", err)
		return 1
	}
	restorer, err := backup.NewPostgresRestorer(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("restore needs pg_restore on this host", "error", err)
		return 1
	}
	service, err := backup.New(backup.Options{
		Destination:   destination,
		Locker:        locker,
		Dumper:        dumper,
		Restorer:      restorer,
		ServerID:      envDefault("TKILLER_SERVER_ID", backup.DefaultServerID),
		AppVersion:    envDefault("TKILLER_VERSION", "dev"),
		Keep:          envInt("TKILLER_BACKUP_KEEP", backup.DefaultKeep),
		RequireTables: requireTables,
		Logger:        logger,
	})
	if err != nil {
		logger.Error("backup service unavailable", "error", err)
		return 1
	}

	switch {
	case cmd.list:
		listing, err := service.List(ctx)
		if err != nil {
			logger.Error("listing backups failed", "error", err)
			return 1
		}
		printListing(listing)
		return 0
	case cmd.restore != "":
		result, err := service.Restore(ctx, cmd.restore)
		if err != nil {
			logger.Error("restore failed", "error", err, "id", cmd.restore)
			return 1
		}
		logger.Info("restore complete", "id", result.ID, "bytes", result.Bytes, "verified", result.Verified)
		return 0
	default:
		summary, err := service.Backup(ctx, backup.KindManual)
		if err != nil {
			logger.Error("backup failed", "error", err)
			return 1
		}
		logger.Info("backup complete", "id", summary.ID, "objects", len(summary.Objects), "bytes", summary.SizeBytes, "location", summary.Location)
		return 0
	}
}

// printListing writes a listing as tab-separated lines, for a human at a shell.
func printListing(listing backup.Listing) {
	fmt.Printf("destination: %s\n", listing.Destination)
	fmt.Printf("server: %s  keep: %d  complete: %d  incomplete: %d  unreadable: %d\n",
		listing.ServerID, listing.Keep, listing.Complete, listing.Incomplete, listing.Unreadable)
	for _, b := range listing.Backups {
		completed := ""
		if b.CompletedAt != nil {
			completed = b.CompletedAt.UTC().Format(time.RFC3339)
		}
		fmt.Printf("%s	%s	%s	%d	%s\n", b.ID, b.Kind, b.Status, b.SizeBytes, completed)
	}
}

func envDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
