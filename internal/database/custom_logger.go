package database

import (
	"context"
	"log"
	"time"

	"gorm.io/gorm/logger"
)

// CustomLogger implements gorm logger.Interface with a configurable slow-query
// threshold. Pattern: theintrodb-api database/custom_logger.go.
type CustomLogger struct {
	SlowThreshold time.Duration
	logLevel      logger.LogLevel
}

// NewCustomLogger creates a logger for the given slow-query threshold (ms).
func NewCustomLogger(slowQueryThresholdMs int) *CustomLogger {
	return &CustomLogger{
		SlowThreshold: time.Duration(slowQueryThresholdMs) * time.Millisecond,
		logLevel:      logger.Warn,
	}
}

func (l *CustomLogger) LogMode(level logger.LogLevel) logger.Interface {
	n := *l
	n.logLevel = level
	return &n
}

func (l *CustomLogger) Info(_ context.Context, msg string, data ...interface{}) {
	if l.logLevel >= logger.Info {
		log.Printf("[gorm:info] "+msg, data...)
	}
}

func (l *CustomLogger) Warn(_ context.Context, msg string, data ...interface{}) {
	if l.logLevel >= logger.Warn {
		log.Printf("[gorm:warn] "+msg, data...)
	}
}

func (l *CustomLogger) Error(_ context.Context, msg string, data ...interface{}) {
	if l.logLevel >= logger.Error {
		log.Printf("[gorm:error] "+msg, data...)
	}
}

// Trace logs SQL execution. Slow queries (>= threshold) log at Warn.
func (l *CustomLogger) Trace(_ context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.logLevel <= logger.Silent {
		return
	}
	elapsed := time.Since(begin)
	sql, rows := fc()

	switch {
	case err != nil && l.logLevel >= logger.Error:
		log.Printf("[gorm] %s | rows=%d | %v | ERROR: %v", sql, rows, elapsed, err)
	case elapsed >= l.SlowThreshold && l.SlowThreshold != 0 && l.logLevel >= logger.Warn:
		log.Printf("[gorm:slow] %s | rows=%d | %v", sql, rows, elapsed)
	case l.logLevel >= logger.Info:
		log.Printf("[gorm] %s | rows=%d | %v", sql, rows, elapsed)
	}
}

// compile-time check
var _ logger.Interface = (*CustomLogger)(nil)
