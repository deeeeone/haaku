package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ObjectInfo describes one object a listing found.
type ObjectInfo struct {
	Key     string
	Size    int64
	ModTime time.Time
}

// Destination is the seam for every place a backup is written: put a stream,
// open, list a page, delete a folder. A local path implements it today; an
// S3-compatible bucket is declared but not implemented (see NewS3Destination).
//
// A Destination is trusted not to escape its root: DeletePrefix and Delete
// refuse a key outside RootPrefix, and ListPage never follows a symlink out of
// a local root.
type Destination interface {
	// Put writes r to key and returns the number of bytes written. A failure
	// leaves no partial object at key.
	Put(ctx context.Context, key string, r io.Reader) (int64, error)
	// Open returns a reader for key and its size. A key that is not there
	// returns ErrNotFound.
	Open(ctx context.Context, key string) (io.ReadCloser, int64, error)
	// ListPage returns up to limit objects whose key is greater than after, in
	// key order, and a continuation token: the last key of the page when more
	// objects follow, else "". A caller pages by passing the token back as
	// after.
	ListPage(ctx context.Context, prefix, after string, limit int) (items []ObjectInfo, next string, err error)
	// Delete removes the named objects. A key that is not there is not an
	// error.
	Delete(ctx context.Context, keys ...string) error
	// DeletePrefix removes every object under a folder prefix (with a trailing
	// slash).
	DeletePrefix(ctx context.Context, prefix string) error
	// Probe reports whether the destination is reachable and writable.
	Probe(ctx context.Context) error
	// Location is a human-readable address for key, for the listing.
	Location(key string) string
}

// maxListPageSize is the most objects one ListPage request takes.
const maxListPageSize = 1000

// ErrObjectNotFound reports a destination key that does not exist.
var ErrObjectNotFound = errors.New("backup: destination object not found")

// LocalDestination is a destination rooted at a directory.
type LocalDestination struct {
	Root string
}

// NewLocalDestination returns a destination writing under root. It creates root
// if it is not there.
func NewLocalDestination(root string) (*LocalDestination, error) {
	if root == "" {
		return nil, errors.New("backup: a local destination needs a root directory")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("backup: creating the backup destination: %w", err)
	}
	return &LocalDestination{Root: root}, nil
}

func (d *LocalDestination) path(key string) string {
	return filepath.Join(d.Root, filepath.FromSlash(key))
}

// Put writes r to key via a temporary file in the same directory, renamed into
// place once the write is complete, so a reader never sees a half-written
// object. Directories are 0700 and objects 0600: a local backup holds the same
// data as the database, password hashes included.
func (d *LocalDestination) Put(_ context.Context, key string, r io.Reader) (int64, error) {
	file := d.path(key)
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, fmt.Errorf("backup: creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return 0, fmt.Errorf("backup: creating a temporary object: %w", err)
	}
	tmpName := tmp.Name()
	written, err := io.Copy(tmp, r)
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(tmpName, 0o600)
	}
	if err == nil {
		err = os.Rename(tmpName, file)
	}
	if err != nil {
		_ = os.Remove(tmpName)
		return 0, fmt.Errorf("backup: writing %s: %w", key, err)
	}
	return written, nil
}

func (d *LocalDestination) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	f, err := os.Open(d.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, ErrObjectNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("backup: opening %s: %w", key, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, fmt.Errorf("backup: stating %s: %w", key, err)
	}
	return f, info.Size(), nil
}

// ListPage walks prefix and returns one page in key order. The whole prefix is
// read on each call; a local destination is this server's own disk, and the
// caller's bounded listing is what stops a tree that is too large to read.
func (d *LocalDestination) ListPage(ctx context.Context, prefix, after string, limit int) ([]ObjectInfo, string, error) {
	if limit < 1 || limit > maxListPageSize {
		return nil, "", fmt.Errorf("backup: a listing page holds 1 to %d objects, not %d", maxListPageSize, limit)
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	base := d.path(prefix)
	var items []ObjectInfo
	err := filepath.WalkDir(base, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(d.Root, p)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if !strings.HasPrefix(key, prefix) || key <= after {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		items = append(items, ObjectInfo{Key: key, Size: info.Size(), ModTime: info.ModTime()})
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("backup: listing %s: %w", prefix, err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	if len(items) <= limit {
		return items, "", nil
	}
	page := items[:limit]
	return page, page[len(page)-1].Key, nil
}

func (d *LocalDestination) Delete(_ context.Context, keys ...string) error {
	if err := checkDeleteKeys(keys); err != nil {
		return err
	}
	for _, key := range keys {
		if err := os.Remove(d.path(key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("backup: deleting %s: %w", key, err)
		}
	}
	return nil
}

func (d *LocalDestination) DeletePrefix(_ context.Context, prefix string) error {
	if err := checkDeletePrefix(prefix); err != nil {
		return err
	}
	if err := os.RemoveAll(d.path(prefix)); err != nil {
		return fmt.Errorf("backup: deleting %s: %w", prefix, err)
	}
	return nil
}

func (d *LocalDestination) Probe(_ context.Context) error {
	info, err := os.Stat(d.Root)
	if err != nil {
		return fmt.Errorf("backup: the destination is not there: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("backup: the destination %s is not a directory", d.Root)
	}
	return nil
}

func (d *LocalDestination) Location(key string) string {
	return d.path(key)
}

// checkDeletePrefix refuses a prefix that is not a folder under RootPrefix.
func checkDeletePrefix(prefix string) error {
	if !strings.HasPrefix(prefix, RootPrefix) || !strings.HasSuffix(prefix, "/") {
		return fmt.Errorf("backup: refusing to delete %q outside a backup folder", prefix)
	}
	return nil
}

// checkDeleteKeys refuses any key outside RootPrefix.
func checkDeleteKeys(keys []string) error {
	for _, key := range keys {
		if !strings.HasPrefix(key, RootPrefix) {
			return fmt.Errorf("backup: refusing to delete %q outside the backups folder", key)
		}
	}
	return nil
}

// S3Options configures an S3-compatible destination. Credentials come from the
// environment and are never written to the repository.
type S3Options struct {
	Endpoint string
	Bucket   string
	Region   string
	Prefix   string
}

// ErrS3NotImplemented reports that the S3 destination is declared but not
// implemented.
var ErrS3NotImplemented = errors.New("backup: the S3 destination is not implemented yet")

// NewS3Destination is the declared S3-compatible destination. It is not
// implemented: S3 support lands with credentials and a live bucket, which this
// change does not have, and a test must never reach the network. It always
// returns ErrS3NotImplemented, so a caller cannot mistake the stub for a working
// destination. The Destination interface above is what an implementation
// satisfies.
func NewS3Destination(S3Options) (Destination, error) {
	return nil, ErrS3NotImplemented
}
