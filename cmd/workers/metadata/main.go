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

// metadata-worker polls TMDB change feeds and refreshes our media cache.
// Phase 1+ — stub for now.
func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("[metadata-worker] failed to load config", "error", err)
		os.Exit(1)
	}
	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)

	logger.Info("[metadata-worker] starting", "env", cfg.Env)

	// TODO(Phase 1): TMDB client + polling loop + image backfill
	logger.Info("[metadata-worker] not yet implemented — Phase 1")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("[metadata-worker] shutting down")
	_ = context.Background()
}
