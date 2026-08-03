package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var (
	db      *gorm.DB
	rdb     *redis.Client
	dbOnce  sync.Once
	redOnce sync.Once
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

// InitDB opens the Postgres connection, configures the pool, and runs
// AutoMigrate. Safe to call multiple times (sync.Once).
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

		if err = db.AutoMigrate(&User{}); err != nil {
			err = fmt.Errorf("automigrate failed: %w", err)
			return
		}

		log.Println("[db] connection established and schema migrated")
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

// --- Redis ---

// InitRedis opens the Redis connection. Safe to call multiple times.
func InitRedis() (*redis.Client, error) {
	var err error
	redOnce.Do(func() {
		opts, perr := redis.ParseURL(getEnv("REDIS_URL", "redis://localhost:6379"))
		if perr != nil {
			err = fmt.Errorf("invalid REDIS_URL: %w", perr)
			return
		}
		rdb = redis.NewClient(opts)
		if cerr := rdb.Ping(context.Background()).Err(); cerr != nil {
			err = fmt.Errorf("redis ping failed: %w", cerr)
			return
		}
		log.Println("[redis] connected")
	})
	return rdb, err
}

// GetRedis returns the Redis singleton. Call after InitRedis.
func GetRedis() (*redis.Client, error) {
	if rdb == nil {
		return nil, fmt.Errorf("redis not initialized — call InitRedis first")
	}
	return rdb, nil
}

// CloseRedis shuts down the Redis connection.
func CloseRedis() error {
	if rdb == nil {
		return nil
	}
	return rdb.Close()
}
