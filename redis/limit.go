package redis

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrClientUnavailable reports that Redis was never initialised (or failed to
// connect). Callers decide what an unavailable counter means — the rate limiter
// fails OPEN on it, so it is returned rather than panicking inside a middleware.
var ErrClientUnavailable = errors.New("redis: client unavailable")

// limitSeq makes each window member unique. Two requests in the same nanosecond
// would otherwise ZADD the same member twice and the second would overwrite the
// first, undercounting the window.
var limitSeq atomic.Uint64

// RateLimitKey is the sliding-window counter key for one identity in one group.
// Format: rate_limit:<group>:<identifier>
//
// The group is the SECOND element on purpose (the pre-2026-11 key was
// ratelimit:<identifier>:<group>): an IPv6 identifier contains colons, so
// anything parsing the key has to split from the left on a known-width prefix.
func RateLimitKey(group, identifier string) string {
	return RedisRateLimitPrefix + group + ":" + identifier
}

// UsageKey is the daily budget counter key for one identity in one group.
// Format: usage:<group>:<identifier>:<YYYY-MM-DD>
//
// The date suffix rotates the counter daily, so the key space cannot grow
// without bound and no sweeper is needed.
func UsageKey(group, identifier string, day time.Time) string {
	return RedisUsagePrefix + group + ":" + identifier + ":" + day.Format("2006-01-02")
}

// WindowResult is the verdict for one sliding window.
type WindowResult struct {
	// Count is the number of requests in the window, this one included.
	Count int64
	// Remaining is max-count, floored at 0.
	Remaining int64
	// Reset is how long until the window frees a slot, derived from the OLDEST
	// request still in the window (not from the window length), so a client that
	// backs off for exactly this long is admitted next time.
	Reset time.Duration
}

// SlidingWindow counts this request against key and reports the verdict.
//
// The four commands are one pipeline, so the whole decision is a single round
// trip: ZADD the member, trim what left the window, read the oldest survivor
// (for Reset), then the cardinality. Pipelined commands execute in order on the
// server, so ZRangeByScore sees the trimmed set.
//
// A missing client is ErrClientUnavailable; every other failure is returned
// as-is. The caller owns the response — see internal/ratelimit, which fails open
// and logs.
func SlidingWindow(key string, window time.Duration, max int64, now time.Time) (WindowResult, error) {
	client := GetClient()
	if client == nil {
		return WindowResult{}, ErrClientUnavailable
	}

	nowMs := float64(now.UnixMilli())
	windowStartMs := float64(now.Add(-window).UnixMilli())
	member := strconv.FormatInt(now.UnixNano(), 10) + ":" + strconv.FormatUint(limitSeq.Add(1)%100000, 10)

	ctx, cancel := context.WithTimeout(context.Background(), RedisDefaultTimeout)
	defer cancel()

	pipe := client.Pipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: nowMs, Member: member})
	pipe.ZRemRangeByScore(ctx, key, "0", strconv.FormatFloat(windowStartMs, 'f', 0, 64))
	oldestCmd := pipe.ZRangeWithScores(ctx, key, 0, 0)
	countCmd := pipe.ZCard(ctx, key)
	// TTL so a key no longer receiving traffic expires instead of lingering.
	pipe.Expire(ctx, key, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return WindowResult{}, err
	}

	count, err := countCmd.Result()
	if err != nil {
		return WindowResult{}, err
	}

	reset := window
	if oldest, err := oldestCmd.Result(); err == nil && len(oldest) == 1 {
		if freeAt := time.UnixMilli(int64(oldest[0].Score)).Add(window); freeAt.After(now) {
			reset = freeAt.Sub(now)
		} else {
			reset = time.Second
		}
	}
	if reset < time.Second {
		reset = time.Second
	}

	remaining := max - count
	if remaining < 0 {
		remaining = 0
	}
	return WindowResult{Count: count, Remaining: remaining, Reset: reset}, nil
}

// UsageCheckRequest is one daily counter to increment and test.
type UsageCheckRequest struct {
	// Key is the full counter key for today — build it with UsageKey so the
	// window and the reset instant agree.
	Key string
	// Limit is this counter's ceiling for the day. 0 means "no budget": the
	// counter is not touched and the request is allowed.
	Limit int64
}

// UsageCheckResult is one counter's outcome, in request order.
type UsageCheckResult struct {
	Allowed   bool
	Count     int64
	Remaining int64
	// Reset is the time until the counter rolls over (the instance's next local
	// midnight, matching the date suffix the key carries).
	Reset time.Duration
}

// UsageCheckMany increments and tests several daily counters in ONE round trip.
//
// Every counter is incremented before any is judged: a request that one ceiling
// rejects still happened, so charging it to the other(s) is the accurate
// account. The 48h TTL is longer than the day the key names, so a counter read
// just after midnight still exists (and any clock skew across replicas is
// harmless).
//
// A request whose Limit is 0 is skipped entirely and reported allowed: that is
// how a surface says "this tier has no daily budget" (see internal/ratelimit's
// groupTiers). Counting it and refusing everything past zero would turn "no
// budget here" into "no requests", which reads as an outage rather than a
// design choice.
func UsageCheckMany(reqs []UsageCheckRequest, now time.Time) ([]UsageCheckResult, error) {
	client := GetClient()
	if client == nil {
		return nil, ErrClientUnavailable
	}
	if len(reqs) == 0 {
		return nil, nil
	}

	reset := nextLocalMidnight(now).Sub(now)
	results := make([]UsageCheckResult, len(reqs))

	counters := make([]int, 0, len(reqs))
	for i, req := range reqs {
		if req.Limit <= 0 {
			results[i] = UsageCheckResult{Allowed: true, Reset: reset}
			continue
		}
		counters = append(counters, i)
	}
	if len(counters) == 0 {
		return results, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), RedisDefaultTimeout)
	defer cancel()

	pipe := client.Pipeline()
	incrs := make([]*redis.IntCmd, len(counters))
	for n, i := range counters {
		incrs[n] = pipe.Incr(ctx, reqs[i].Key)
		pipe.Expire(ctx, reqs[i].Key, 48*time.Hour)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}

	for n, i := range counters {
		count, err := incrs[n].Result()
		if err != nil {
			return nil, err
		}
		remaining := reqs[i].Limit - count
		if remaining < 0 {
			remaining = 0
		}
		results[i] = UsageCheckResult{
			Allowed:   count <= reqs[i].Limit,
			Count:     count,
			Remaining: remaining,
			Reset:     reset,
		}
	}
	return results, nil
}

// nextLocalMidnight is the instant the daily counters roll over. It is the
// instance's LOCAL midnight (the same clock the date suffix uses), so an
// operator reading the logs sees budgets reset when their day does.
func nextLocalMidnight(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1)
}
