package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New creates a slog.Logger — JSON in production, text in development.
// Pattern: theintrodb-api main.go (stdlib log) → upgraded to slog for
// structured JSON (per system-overview.md observability section).
func New(appEnv string) *slog.Logger {
	var handler slog.Handler
	switch strings.ToLower(appEnv) {
	case "production", "prod":
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	default:
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
	}
	return slog.New(handler)
}

// Fatal logs at Fatal level and exits. Only for startup failures (InitDB, etc.).
// Normal handler errors use log-and-return, not os.Exit.
func Fatal(logger *slog.Logger, msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}
