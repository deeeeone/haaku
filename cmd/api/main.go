package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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
