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

// webhooks-worker consumes the events outbox and delivers outbound webhooks.
// Phase 8+ — stub for now.
func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("[webhooks-worker] failed to load config", "error", err)
		os.Exit(1)
	}
	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)

	logger.Info("[webhooks-worker] starting", "env", cfg.Env)

	// TODO(Phase 8): Redis Streams consumer → outbox poll → HMAC sign → POST with retries
	logger.Info("[webhooks-worker] not yet implemented — Phase 8")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("[webhooks-worker] shutting down")
	_ = context.Background()
}
