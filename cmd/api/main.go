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

	// Redis (T0.4 init; T0.7 circuit breaker + single-flight pending)
	if cfg.RedisURL != "" {
		if _, err := database.InitRedis(); err != nil {
			logger.Warn("redis unavailable — running without cache", "error", err)
		}
		defer database.CloseRedis()
	}

	app := httpapi.NewServer(cfg)

	// Code-first OpenAPI generator (T0.6 — non-fatal, TIDB pattern)
	if err := openapi.GenerateYAML("openapi.yaml"); err != nil {
		logger.Warn("failed to generate openapi.yaml", "error", err)
	}

	// TODO(T0.9): Cron scaffold (robfig/cron) — startup-refresh for leaderboard,
	// trending rankings recompute, TMDB change-feed poll.
	// Pattern: theintrodb-api main.go setupCronJobs().

	// TODO(T0.9): Worker entrypoints — metadata-worker, webhook-worker, import-worker,
	// stats-worker, notify-worker. Separate main packages in cmd/workers/.

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
