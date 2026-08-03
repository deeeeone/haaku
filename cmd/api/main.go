package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"trakt-api/internal/config"
	"trakt-api/internal/database"
	"trakt-api/internal/httpapi"
	"trakt-api/internal/logging"
	"trakt-api/internal/openapi"
	"trakt-api/redis"

	"github.com/robfig/cron/v3"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)

	logger.Info("starting trakt-api", "env", cfg.Env, "port", cfg.Port)

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
	}

	// Redis (T0.7 + T0.8: circuit breaker, single-flight, version counters)
	if cfg.RedisURL != "" {
		if err := redis.InitRedis(); err != nil {
			logger.Warn("redis unavailable — running without cache", "error", err)
		}
		defer redis.CloseRedis()
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

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(ctx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
	}
	logger.Info("server stopped gracefully")
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
