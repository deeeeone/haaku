package workers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"trakt-api/external/tmdb"
	"trakt-api/internal/metadata"
	"trakt-api/internal/outbox"

	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Deps is what the background workers need from the process that hosts them.
// A nil field is allowed: a worker that needs it reports the failure on its
// next pass rather than crashing the process at startup.
type Deps struct {
	DB     *gorm.DB
	Redis  *goredis.Client
	TMDB   *tmdb.Client
	Logger *slog.Logger
}

// Kind distinguishes a worker that runs a step in a loop from one that runs
// jobs on a clock.
type Kind int

const (
	// Loop runs Step, waits Interval, and repeats until shutdown.
	Loop Kind = iota
	// Schedule runs each Job at its cron time (UTC).
	Schedule
)

// Job is one cron-scheduled unit of work inside a Schedule worker.
type Job struct {
	// Schedule is a standard five-field cron spec, evaluated in UTC.
	Schedule string
	// Describe is a one-line, human-readable summary for logs and docs.
	Describe string
	// Run performs the work. It must honour ctx so a shutdown stops it.
	Run func(ctx context.Context, deps Deps) error
}

// Worker is one named background unit. Name must be one of the names
// ParseSelection accepts, so metrics and logs line up with the selector.
type Worker struct {
	Name     string
	Kind     Kind
	Interval time.Duration                              // Loop: pause between Step calls.
	Step     func(ctx context.Context, deps Deps) error // Loop.
	Jobs     []Job                                      // Schedule.
}

// Catalog builds the workers a process with this selection runs. Only the
// selected names are constructed, so a worker the operator did not ask for
// cannot start by accident — that is the property that lets an API process and
// a worker process share one database without running the same loop twice.
func Catalog(deps Deps, sel Selection) []Worker {
	_ = deps // the workers read their dependencies when they run, not here.

	out := make([]Worker, 0, len(sel.names))
	for _, name := range sel.Names() {
		switch name {
		case NameWebhooks:
			out = append(out, Worker{
				Name:     name,
				Kind:     Loop,
				Interval: 500 * time.Millisecond,
				Step: func(ctx context.Context, d Deps) error {
					return DeliverWebhooks(ctx, d.DB, d.Redis)
				},
			})
		case NameActivity:
			out = append(out, Worker{
				Name:     name,
				Kind:     Loop,
				Interval: 500 * time.Millisecond,
				Step: func(ctx context.Context, d Deps) error {
					return FanOutActivity(ctx, d.DB, d.Redis)
				},
			})
		case NameOutbox:
			out = append(out, Worker{
				Name:     name,
				Kind:     Loop,
				Interval: time.Second,
				Step: func(ctx context.Context, d Deps) error {
					return outbox.PublishBatch(ctx, d.DB, d.Redis)
				},
			})
		case NameMetadata:
			out = append(out, metadataWorker())
		case NameStats:
			out = append(out, statsWorker())
		}
	}
	return out
}

// metadataWorker owns the TMDB upkeep jobs. It replaces the standalone
// cmd/workers/metadata binary: the same code now runs inside the one binary's
// worker mode, so the schedule lives in exactly one place. Both jobs are
// idempotent, so a missed or doubled run costs API calls, not correctness.
func metadataWorker() Worker {
	return Worker{
		Name: NameMetadata,
		Kind: Schedule,
		Jobs: []Job{
			{
				// 03:00 UTC daily — TMDB's change feed lags a day, and a quiet
				// hour keeps the poll away from the evening peak.
				Schedule: "0 3 * * *",
				Describe: "TMDB change-feed poll: refresh movies, shows and people changed since the last run",
				Run: func(ctx context.Context, d Deps) error {
					if d.TMDB == nil || d.DB == nil {
						return errors.New("metadata: a TMDB client and a database are required")
					}
					return metadata.RunSync(d.TMDB, d.DB, metadata.SinceFile())
				},
			},
			{
				// 04:00 UTC on Sundays — a bulk refresh of the local media
				// cache that would be wasteful daily.
				Schedule: "0 4 * * 0",
				Describe: "TMDB popular/trending backfill: refill the local media cache",
				Run: func(ctx context.Context, d Deps) error {
					if d.TMDB == nil || d.DB == nil {
						return errors.New("metadata: a TMDB client and a database are required")
					}
					return metadata.Backfill(ctx, d.TMDB, d.DB, metadata.BackfillPages())
				},
			},
		},
	}
}

// statsWorker is the seam for the stats and ranking recompute engine (issue
// #5). Its job list is deliberately EMPTY in this PR: the recompute is a
// sibling lane, and a placeholder job here would publish numbers nobody
// computed. Selecting this worker starts it, heartbeats it once at startup, and
// logs that it has nothing scheduled — the recompute lane fills in Jobs and
// nothing else changes. If you are here to make stats recompute and the Jobs
// slice is still empty, that is this seam, not a missing file.
func statsWorker() Worker {
	return Worker{Name: NameStats, Kind: Schedule, Jobs: nil}
}
