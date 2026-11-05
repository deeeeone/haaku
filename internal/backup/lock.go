package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Unlocker releases a held lock.
type Unlocker interface{ Unlock() }

// Locker serialises the backup subsystem: one backup, rotation, deletion or
// restore at a time. The whole run is under the lock, from the listing that
// reserves the folder id to the manifest that commits it, so two runs can never
// choose the same id.
type Locker interface {
	Lock(ctx context.Context) (Unlocker, error)
}

// MemoryLocker serialises within one process. It is the default, for tests and
// for a single-process server; a deployment that runs several processes
// against one destination wants LocalLocker, which also excludes other
// processes.
type MemoryLocker struct{ mu sync.Mutex }

// NewMemoryLocker returns a process-local locker.
func NewMemoryLocker() Locker { return &MemoryLocker{} }

func (l *MemoryLocker) Lock(ctx context.Context) (Unlocker, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	return memoryUnlocker{&l.mu}, nil
}

type memoryUnlocker struct{ mu *sync.Mutex }

func (u memoryUnlocker) Unlock() { u.mu.Unlock() }

// LocalLocker takes an exclusive flock on a lock file inside the destination
// directory, so it excludes other processes sharing the same destination as
// well as goroutines in this one. A blocker that holds the lock forever is the
// caller's to bound with ctx.
type LocalLocker struct {
	path string
	mu   sync.Mutex
}

// NewLocalLocker returns a locker whose lock file lives in dir. dir is created
// if it is not there.
func NewLocalLocker(dir string) (Locker, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("backup: creating the lock directory: %w", err)
	}
	return &LocalLocker{path: filepath.Join(dir, ".backup.lock")}, nil
}

func (l *LocalLocker) Lock(ctx context.Context) (Unlocker, error) {
	l.mu.Lock()
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		l.mu.Unlock()
		return nil, fmt.Errorf("backup: opening the lock file: %w", err)
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &localUnlocker{f: f, mu: &l.mu}, nil
		}
		if err != syscall.EWOULDBLOCK {
			_ = f.Close()
			l.mu.Unlock()
			return nil, fmt.Errorf("backup: taking the lock: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			l.mu.Unlock()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

type localUnlocker struct {
	f  *os.File
	mu *sync.Mutex
}

func (u *localUnlocker) Unlock() {
	_ = syscall.Flock(int(u.f.Fd()), syscall.LOCK_UN)
	_ = u.f.Close()
	u.mu.Unlock()
}
