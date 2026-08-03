package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"trakt-api/internal/config"
	"trakt-api/internal/httpapi"
)

func main() {
	// Load configuration (env vars only — no secret files, no pass-cli)
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// TODO(T0.4/T0.8): database init (pool tuning), redis init (circuit breaker,
	// single-flight), worker pools — patterns in docs/02-architecture/engineering-patterns.md
	// (planning DB at ../trakt-killer-plan).

	app := httpapi.NewServer(cfg)

	// Start server in a goroutine
	go func() {
		log.Printf("trakt-api listening on :%s (%s)", cfg.Port, cfg.Env)
		if err := app.Listen(":" + cfg.Port); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Graceful shutdown (10s drain — pattern: theintrodb-api main.go)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("Server stopped gracefully")
}
