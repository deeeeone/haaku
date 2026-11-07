package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"trakt-api/external/tmdb"
	"trakt-api/internal/backup"
	"trakt-api/internal/config"
	"trakt-api/internal/database"
	"trakt-api/internal/handlers/integrations"
	"trakt-api/internal/httpapi"
	"trakt-api/internal/logging"
	"trakt-api/internal/mode"
	"trakt-api/internal/obs"
	"trakt-api/internal/workers"
	"trakt-api/redis"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

func main() {
	os.Exit(run())
}

// run is main's body, returning the process exit code so that deferred cleanup
// (database and Redis handles) still runs. A bare os.Exit in main would skip
// those defers.
func run() int {
	migrateOnly := flag.Bool("migrate-only", false,
		"apply pending schema migrations and exit (equivalent to "+mode.EnvVar+"=migrate)")
	backupNow := flag.Bool("backup", false, "take a manual database backup and exit")
	backupList := flag.Bool("backup-list", false, "list database backups and exit")
	backupRestore := flag.String("backup-restore", "", "restore the backup with this id and exit")
	backupDir := flag.String("backup-dir", "", "backup destination directory (default $TKILLER_BACKUP_DIR or ./backups)")
	flag.Parse()

	serverMode, err := mode.FromEnv()
	if err != nil {
		slog.Error("invalid server mode", "error", err)
		return 2
	}
	if *migrateOnly {
		serverMode = mode.Migrate
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return 1
	}

	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)

	logger.Info("starting trakt-api", "env", cfg.Env, "port", cfg.Port, "mode", serverMode.String())

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
		if serverMode == mode.Migrate {
			logger.Error("migration requested without DATABASE_URL")
			return 1
		}
	}

	// Backup commands (issue #16): one-shot, then exit. They use the database and
	// the backup destination and never start a listener.
	if *backupNow || *backupList || *backupRestore != "" {
		return runBackupCommand(logger, cfg, backupCommand{
			take:    *backupNow,
			list:    *backupList,
			restore: *backupRestore,
			dir:     *backupDir,
		})
	}

	// Migrate-and-exit mode stops here: no Redis, no workers, no listener.
	//
	// Only this mode and the API mode may change the schema, and the API applies
	// migrations before it serves; a worker never does (see internal/mode).
	if serverMode == mode.Migrate {
		return migrate(logger)
	}

	// API mode owns the schema: apply every pending migration before any worker
	// or listener starts, so the tables are the ones the SQL migrations define
	// and never the ones GORM's AutoMigrate used to invent. The runner is the
	// same one `--migrate-only`/`TKILLER_MODE=migrate` uses, so there is a
	// single, tested path; it is a fast no-op once the schema is current. A
	// worker mode never gets here (wantsMigrations is false for it), which is
	// the whole point of internal/mode.
	if wantsMigrations(serverMode, cfg.DatabaseURL != "") {
		if code := migrate(logger); code != 0 {
			return code
		}
	}

	// Redis (T0.7 + T0.8: circuit breaker, single-flight, version counters)
	if cfg.RedisURL != "" {
		if err := redis.InitRedis(); err != nil {
			logger.Warn("redis unavailable — running without cache", "error", err)
		}
		defer func() { _ = redis.CloseRedis() }()
	}

	// Worker selection (T0.9). One binary runs every role, so the same worker
	// registry drives an API that hosts the workers in-process and a dedicated
	// worker process. TKILLER_WORKER picks which run:
	//
	//	unset / all  every worker (the classic single-container install)
	//	<n>,<n>      only those workers
	//	none         no background worker in this process
	//
	// "none" is how a separate worker deployment claims the workers without the
	// API running a second copy of the same loop: a worker runs in exactly the
	// process that selected it, and nowhere else. See
	// docs/architecture/workers.md.
	selection, err := workers.ParseSelection(os.Getenv(workers.EnvSelection))
	if err != nil {
		logger.Error("invalid worker selection", "error", err)
		return 2
	}

	startWorkers, fatalSelection := workerPlan(serverMode, selection)
	switch {
	case fatalSelection:
		// A worker-only process that selected nothing would start, do nothing
		// and look healthy. Refuse instead.
		logger.Error("worker mode selected no workers",
			"selection", selection.String(),
			"hint", "set "+workers.EnvSelection+" to a worker name, a list, or all")
		return 2
	case !startWorkers && serverMode.ServesHTTP():
		logger.Info("workers: none — API only (this process runs no background workers)")
	}

	var runtime *workers.Runtime
	if startWorkers {
		deps := workers.Deps{
			Logger: logger,
			DB:     currentDB(),
			Redis:  redis.GetClient(),
			TMDB:   tmdbForSelection(logger, selection),
		}
		runtime = workers.NewRuntime(deps, workers.Catalog(deps, selection), healthListenFor(serverMode))
		logger.Info("starting background workers",
			"mode", serverMode.String(), "workers", selection.Names())
	}

	// Observability listeners (issue #9). Off unless TKILLER_METRICS_LISTEN is set.
	if err := obs.Start(context.Background()); err != nil {
		logger.Warn("observability listeners unavailable", "error", err)
	}

	// One context for the whole process: the HTTP listener and the workers both
	// stop on the same SIGINT/SIGTERM, and the runtime drains before the
	// process exits.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if runtime != nil {
		if err := runtime.Start(ctx); err != nil {
			logger.Error("background workers failed to start", "error", err)
			// An API can still serve without its background workers; a
			// worker-only process has nothing left to do.
			if !serverMode.ServesHTTP() {
				return 1
			}
		}
	}

	if !serverMode.ServesHTTP() {
		logger.Info("worker mode — no HTTP listener")
		<-ctx.Done()
		shutdownWorkers(logger, runtime)
		return 0
	}

	app := httpapi.NewServer(cfg)

	// Cron jobs (T0.9 — TIDB pattern: UTC, panic recovery, startup refresh).
	// The background workers are NOT here: they run through the worker runtime
	// above, which the worker mode shares, so there is one implementation of
	// each loop rather than one per entrypoint.
	setupCronJobs(logger)

	go func() {
		logger.Info("trakt-api listening", "port", cfg.Port)
		if err := app.Listen(":" + cfg.Port); err != nil {
			logging.Fatal(logger, "server start failed", "error", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
	}
	shutdownWorkers(logger, runtime)
	logger.Info("server stopped gracefully")
	return 0
}

// workerPlan decides what a mode and a worker selection mean:
//
//   - no workers run outside api/worker modes;
//   - an empty selection is fatal only for a worker-only process, which would
//     otherwise start, do nothing and look healthy;
//   - otherwise the selected workers run.
//
// It is a pure function so the two properties the deployment depends on —
// "worker mode never migrates" and "an API-only process starts no worker" —
// can be asserted without a database, a Redis or a socket.
func workerPlan(m mode.Mode, sel workers.Selection) (start, fatal bool) {
	if !m.RunsWorkers() {
		return false, false
	}
	if sel.Empty() {
		return false, m == mode.Worker
	}
	return true, false
}

// wantsMigrations reports whether a mode may and should apply migrations.
func wantsMigrations(m mode.Mode, hasDatabase bool) bool {
	return hasDatabase && m.MayMigrate()
}

// healthListenFor returns the address the worker health listener should bind.
// The API already answers /healthz and /readyz on its own port, so it gets no
// second listener; a worker-only process needs one.
func healthListenFor(m mode.Mode) string {
	if m.ServesHTTP() {
		return ""
	}
	return workers.HealthListenAddr()
}

// currentDB returns the initialized database handle, or nil when the process is
// running without persistence. The workers treat a nil handle as "retry later".
func currentDB() *gorm.DB {
	db, err := database.GetDB()
	if err != nil {
		return nil
	}
	return db
}

// tmdbForSelection builds the TMDB client only when the metadata worker is
// selected, so a process that does not need it does not pay for it (and a
// missing token cannot fail an unrelated worker).
func tmdbForSelection(logger *slog.Logger, sel workers.Selection) *tmdb.Client {
	if !sel.Has(workers.NameMetadata) {
		return nil
	}
	client, err := tmdb.New()
	if err != nil {
		logger.Warn("metadata worker selected but the TMDB client is unavailable", "error", err)
		return nil
	}
	return client
}

// shutdownWorkers stops the worker runtime, bounded so a wedged pass cannot
// hold the process open. A nil runtime (API-only, or worker mode with workers
// off) is a no-op.
func shutdownWorkers(logger *slog.Logger, rt *workers.Runtime) {
	if rt == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := rt.Shutdown(ctx); err != nil {
		logger.Warn("background workers did not stop cleanly", "error", err)
	}
	logger.Info("background workers stopped")
}

// migrate applies pending migrations and reports the outcome as an exit code.
func migrate(logger *slog.Logger) int {
	ctx := context.Background()
	if err := database.Migrate(ctx); err != nil {
		if errors.Is(err, database.ErrMigrationsNotImplemented) {
			logger.Error("migrations are not available yet", "error", err)
		} else {
			logger.Error("migration failed", "error", err)
		}
		return 1
	}
	logger.Info("migrations applied")

	// The schema is current; sweep any credential columns still holding plaintext
	// (issue #16 §2 — EncryptIfPlaintext was tested but never wired). This runs
	// in the migrate and api startup paths only (a worker never calls migrate)
	// and refuses to run without a usable root key, so it can never write
	// unreadable values into the schema.
	db, err := database.GetDB()
	if err != nil {
		logger.Error("integration token backfill: database unavailable", "error", err)
		return 1
	}
	if _, err := integrations.BackfillPlaintextTokens(ctx, db, logger); err != nil {
		logger.Error("integration token backfill failed", "error", err)
		return 1
	}
	return 0
}

// setupCronJobs initializes cron with UTC timezone and panic recovery.
// Pattern: theintrodb-api main.go setupCronJobs().
//
// It is the API's own scheduler, for jobs that must run exactly where the API
// runs. The recurring background work — webhook delivery, activity fan-out,
// outbox publishing and the TMDB maintenance jobs — is NOT here: it goes
// through the worker runtime (internal/workers), so a dedicated worker process
// can own it instead.
func setupCronJobs(logger *slog.Logger) {
	c := cron.New(cron.WithLocation(time.UTC))

	// TODO(Phase 3): refresh trending rankings every 15m. The ranking recompute
	// is the stats worker's job list (internal/workers catalog); this seam is
	// for anything that must be API-local.
	// _, err := c.AddFunc("*/15 * * * *", refreshTrendingRankings)

	// Start the scheduler; jobs are added by domain agents as phases land.
	c.Start()
	logger.Info("cron scheduler started")
}

// backupCommand is the parsed one-shot backup flags.
type backupCommand struct {
	take    bool
	list    bool
	restore string
	dir     string
}

// runBackupCommand builds the backup service and runs one command: take a manual
// backup, list backups, or restore one in full. It reports the outcome as an
// exit code. Configuration is env-only, like the rest of the server.
func runBackupCommand(logger *slog.Logger, cfg *config.Config, cmd backupCommand) int {
	ctx := context.Background()
	if cfg.DatabaseURL == "" {
		logger.Error("backup commands need DATABASE_URL")
		return 1
	}
	dir := cmd.dir
	if dir == "" {
		dir = envDefault("TKILLER_BACKUP_DIR", "./backups")
	}
	destination, err := backup.NewLocalDestination(dir)
	if err != nil {
		logger.Error("backup destination unavailable", "error", err, "dir", dir)
		return 1
	}
	locker, err := backup.NewLocalLocker(dir)
	if err != nil {
		logger.Error("backup lock unavailable", "error", err)
		return 1
	}
	requireTables := splitCSV(envDefault("TKILLER_BACKUP_REQUIRE_TABLES", "users"))
	dumper, err := backup.NewPostgresDumper(ctx, cfg.DatabaseURL, requireTables)
	if err != nil {
		logger.Error("backup needs pg_dump and pg_restore on this host", "error", err)
		return 1
	}
	restorer, err := backup.NewPostgresRestorer(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("restore needs pg_restore on this host", "error", err)
		return 1
	}
	service, err := backup.New(backup.Options{
		Destination:   destination,
		Locker:        locker,
		Dumper:        dumper,
		Restorer:      restorer,
		ServerID:      envDefault("TKILLER_SERVER_ID", backup.DefaultServerID),
		AppVersion:    envDefault("TKILLER_VERSION", "dev"),
		Keep:          envInt("TKILLER_BACKUP_KEEP", backup.DefaultKeep),
		RequireTables: requireTables,
		Logger:        logger,
	})
	if err != nil {
		logger.Error("backup service unavailable", "error", err)
		return 1
	}

	switch {
	case cmd.list:
		listing, err := service.List(ctx)
		if err != nil {
			logger.Error("listing backups failed", "error", err)
			return 1
		}
		printListing(listing)
		return 0
	case cmd.restore != "":
		result, err := service.Restore(ctx, cmd.restore)
		if err != nil {
			logger.Error("restore failed", "error", err, "id", cmd.restore)
			return 1
		}
		logger.Info("restore complete", "id", result.ID, "bytes", result.Bytes, "verified", result.Verified)
		return 0
	default:
		summary, err := service.Backup(ctx, backup.KindManual)
		if err != nil {
			logger.Error("backup failed", "error", err)
			return 1
		}
		logger.Info("backup complete", "id", summary.ID, "objects", len(summary.Objects), "bytes", summary.SizeBytes, "location", summary.Location)
		return 0
	}
}

// printListing writes a listing as tab-separated lines, for a human at a shell.
func printListing(listing backup.Listing) {
	fmt.Printf("destination: %s\n", listing.Destination)
	fmt.Printf("server: %s  keep: %d  complete: %d  incomplete: %d  unreadable: %d\n",
		listing.ServerID, listing.Keep, listing.Complete, listing.Incomplete, listing.Unreadable)
	for _, b := range listing.Backups {
		completed := ""
		if b.CompletedAt != nil {
			completed = b.CompletedAt.UTC().Format(time.RFC3339)
		}
		fmt.Printf("%s	%s	%s	%d	%s\n", b.ID, b.Kind, b.Status, b.SizeBytes, completed)
	}
}

func envDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
