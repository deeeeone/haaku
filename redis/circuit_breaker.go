package redis

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Circuit breaker states.
const (
	CircuitClosed   = 0 // Normal operation
	CircuitOpen     = 1 // Failing — requests fail fast
	CircuitHalfOpen = 2 // Testing if service recovered
)

// Default circuit breaker configuration.
//
// NOTE: there is no "stay open" timeout knob. The open -> half-open transition
// is governed by defaultTestInterval below plus lastTestTime; REDIS_CB_TIMEOUT_SEC
// and a defaultCircuitTimeout constant used to be declared here but nothing read
// them, so they were removed with the CI gates. Adding a stay-open phase is a
// behaviour change and needs its own test.
const (
	defaultFailureThreshold = 250              // Open after 250 consecutive failures
	defaultSuccessThreshold = 10               // Need 10 successes to fully close
	defaultTestInterval     = 10 * time.Second // Test interval in open state
)

// env-configured overrides (read once at startup but reloaded on each access).
func getFailureThreshold() int32 {
	if v := os.Getenv("REDIS_CB_FAILURE_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return int32(n)
		}
	}
	return defaultFailureThreshold
}

func getSuccessThreshold() int32 {
	if v := os.Getenv("REDIS_CB_SUCCESS_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return int32(n)
		}
	}
	return defaultSuccessThreshold
}

// CircuitBreaker implements the circuit breaker pattern for Redis operations.
// Concurrency-safe via atomic counters + RWMutex for timestamp/state transitions.
type CircuitBreaker struct {
	state           int32
	failures        int32
	successes       int32
	lastFailureTime time.Time
	lastTestTime    time.Time
	mu              sync.RWMutex
}

// Global circuit breaker instance.
var globalCircuitBreaker = &CircuitBreaker{}

// Call executes fn with circuit breaker protection.
// Returns an error immediately if the circuit is open.
func (cb *CircuitBreaker) Call(fn func() error) error {
	state := atomic.LoadInt32(&cb.state)

	if state == CircuitOpen {
		// Check if enough time has passed to test recovery.
		cb.mu.RLock()
		shouldTest := time.Since(cb.lastTestTime) > defaultTestInterval
		cb.mu.RUnlock()

		if shouldTest {
			cb.mu.Lock()
			if atomic.LoadInt32(&cb.state) == CircuitOpen && time.Since(cb.lastTestTime) > defaultTestInterval {
				atomic.StoreInt32(&cb.state, CircuitHalfOpen)
				log.Printf("[redis] circuit breaker → half-open")
				cb.lastTestTime = time.Now()
				atomic.StoreInt32(&cb.successes, 0)
			}
			cb.mu.Unlock()
			state = CircuitHalfOpen
		} else {
			return fmt.Errorf("circuit breaker is open")
		}
	}

	// Execute the function.
	err := fn()

	if err != nil {
		failures := atomic.AddInt32(&cb.failures, 1)
		cb.mu.Lock()
		cb.lastFailureTime = time.Now()
		cb.mu.Unlock()

		if state == CircuitClosed && failures >= getFailureThreshold() {
			atomic.StoreInt32(&cb.state, CircuitOpen)
			log.Printf("[redis] circuit breaker opened — %d consecutive failures", failures)
		} else if state == CircuitHalfOpen {
			atomic.StoreInt32(&cb.state, CircuitOpen)
			log.Printf("[redis] circuit breaker re-opened from half-open")
		}
		return err
	}

	// Success — potentially close the circuit.
	switch state {
	case CircuitHalfOpen:
		successes := atomic.AddInt32(&cb.successes, 1)
		if successes >= getSuccessThreshold() {
			cb.mu.Lock()
			atomic.StoreInt32(&cb.state, CircuitClosed)
			log.Printf("[redis] circuit breaker closed — %d consecutive successes", successes)
			atomic.StoreInt32(&cb.failures, 0)
			atomic.StoreInt32(&cb.successes, 0)
			cb.mu.Unlock()
		}
	case CircuitClosed:
		// Reset failure counter on success in closed state.
		atomic.StoreInt32(&cb.failures, 0)
	}

	return nil
}

// IsHealthy returns true if the circuit is not fully open.
func (cb *CircuitBreaker) IsHealthy() bool {
	state := atomic.LoadInt32(&cb.state)
	return state == CircuitClosed || state == CircuitHalfOpen
}

// GetState returns a human-readable circuit state.
func (cb *CircuitBreaker) GetState() string {
	switch atomic.LoadInt32(&cb.state) {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// GetCircuitBreakerState returns the global circuit breaker state.
func GetCircuitBreakerState() string {
	return globalCircuitBreaker.GetState()
}

// IsCircuitBreakerHealthy returns whether the global circuit breaker is healthy.
func IsCircuitBreakerHealthy() bool {
	return globalCircuitBreaker.IsHealthy()
}

// RetryConfig holds retry parameters.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// Default retry configuration for Redis operations.
var defaultRetryConfig = RetryConfig{
	MaxRetries: 3,
	BaseDelay:  100 * time.Millisecond,
	MaxDelay:   2 * time.Second,
}

// executeWithRetry runs fn through the circuit breaker with exponential backoff.
func executeWithRetry(fn func() error, config RetryConfig) error {
	var err error
	delay := config.BaseDelay

	for i := 0; i <= config.MaxRetries; i++ {
		err = globalCircuitBreaker.Call(fn)
		if err == nil {
			return nil
		}
		if i < config.MaxRetries {
			time.Sleep(delay)
			delay *= 2
			if delay > config.MaxDelay {
				delay = config.MaxDelay
			}
		}
	}
	return fmt.Errorf("operation failed after %d retries: %w", config.MaxRetries, err)
}
