package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"trakt-api/internal/config"
	"trakt-api/internal/logging"
)

// stats-worker recomputes user stats, trending/popular/anticipated rankings,
// and activity feed fan-out. Phase 3+ — stub for now.
func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("[stats-worker] failed to load config", "error", err)
		os.Exit(1)
	}
	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)

	logger.Info("[stats-worker] starting", "env", cfg.Env)

	// TODO(Phase 3): ranking recompute via stats_cache + trending_rankings tables
	logger.Info("[stats-worker] not yet implemented — Phase 3")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("[stats-worker] shutting down")
	_ = context.Background()
}
