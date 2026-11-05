package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var (
	db     *gorm.DB
	dbOnce sync.Once
)

// --- env helpers ---

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if iv, err := strconv.Atoi(v); err == nil {
			return iv
		}
	}
	return fallback
}

// --- defaults (match theintrodb-api conventions) ---

const (
	defaultMaxIdleConns         = 5
	defaultMaxOpenConns         = 20
	defaultConnMaxLifetimeMin   = 30
	defaultSlowQueryThresholdMs = 10
)

// --- PostgreSQL ---

// InitDB opens the Postgres connection and configures the pool. Safe to call
// multiple times (sync.Once).
//
// It deliberately does NOT create or alter the schema. The schema is owned by
// the embedded SQL migrations (see Migrate); letting GORM's AutoMigrate create
// tables first produced GORM-flavoured column types (search_vector as text,
// users.username as text) that then broke the SQL migrations built on top of
// them, and left SQL and the models free to drift. Apply Migrate after InitDB.
func InitDB() (*gorm.DB, error) {
	var err error
	dbOnce.Do(func() {
		dsn := os.Getenv("DATABASE_URL")
		if dsn == "" {
			err = fmt.Errorf("DATABASE_URL is required")
			return
		}

		threshold := getEnvInt("DB_SLOW_QUERY_THRESHOLD_MS", defaultSlowQueryThresholdMs)
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
			Logger: NewCustomLogger(threshold),
		})
		if err != nil {
			err = fmt.Errorf("failed to connect to database: %w", err)
			return
		}

		var sqlDB *sql.DB
		sqlDB, err = db.DB()
		if err != nil {
			err = fmt.Errorf("failed to get underlying sql.DB: %w", err)
			return
		}

		maxIdle := getEnvInt("DB_MAX_IDLE_CONNS", defaultMaxIdleConns)
		maxOpen := getEnvInt("DB_MAX_OPEN_CONNS", defaultMaxOpenConns)
		maxLifetimeMin := getEnvInt("DB_CONN_MAX_LIFETIME_MINUTES", defaultConnMaxLifetimeMin)

		sqlDB.SetMaxIdleConns(maxIdle)
		sqlDB.SetMaxOpenConns(maxOpen)
		sqlDB.SetConnMaxLifetime(time.Duration(maxLifetimeMin) * time.Minute)

		log.Printf("[db] pool configured: idle=%d open=%d lifetime=%dm slow_query_threshold=%dms",
			maxIdle, maxOpen, maxLifetimeMin, threshold)

		log.Println("[db] connection established")
	})
	return db, err
}

// GetDB returns the singleton. Call after InitDB.
func GetDB() (*gorm.DB, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized — call InitDB first")
	}
	return db, nil
}

// SetDBForTest is a TEST-ONLY seam that points the package-level DB singleton
// at an arbitrary *gorm.DB (e.g. an in-memory sqlite DB created by a test) so
// DB-bound handlers can be exercised without a live Postgres. It re-arms
// dbOnce so a later InitDB (from another test or the real app) still runs and
// takes over, and registers a cleanup that resets the singleton to nil.
//
// Do NOT call this from production code paths. Production always uses InitDB
// (Postgres); this helper exists solely so the underscore-excluded _tests_
// integration suite and per-module handler tests can inject their own handle.
// It is not safe to call concurrently with InitDB/CloseDB.
func SetDBForTest(tb testing.TB, testDB *gorm.DB) {
	tb.Helper()
	db = testDB
	dbOnce = sync.Once{}
	tb.Cleanup(func() {
		db = nil
		dbOnce = sync.Once{}
	})
}

// WithTransaction executes fn inside a DB transaction. Automatically rolls
// back on error or panic, commits on success. Pattern: theintrodb-api.
func WithTransaction(fn func(tx *gorm.DB) error) error {
	database, err := GetDB()
	if err != nil {
		return fmt.Errorf("cannot start transaction: %w", err)
	}
	tx := database.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit().Error
}

// WithTransactionResult is WithTransaction that returns a value.
func WithTransactionResult(fn func(tx *gorm.DB) (interface{}, error)) (interface{}, error) {
	database, err := GetDB()
	if err != nil {
		return nil, fmt.Errorf("cannot start transaction: %w", err)
	}
	tx := database.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()
	result, err := fn(tx)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	return result, nil
}

// CloseDB closes the database connection pool.
func CloseDB() error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// --- Redis stubs (T0.7 full implementation) ---

// InitRedis initializes the Redis connection. Returns nil,* if not configured.
// Full implementation arrives in T0.7 (circuit breaker + single-flight).
func InitRedis() (interface{}, error) {
	return nil, nil
}

// GetRedis returns the Redis client singleton or nil if not initialized.
func GetRedis() (interface{}, error) {
	return nil, nil
}

// CloseRedis closes the Redis connection if open.
func CloseRedis() {
}
