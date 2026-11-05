package backup

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// TestLocalDestinationRoundTrip exercises the local-path destination the real
// binary uses: put, open, list a page, and delete a folder.
func TestLocalDestinationRoundTrip(t *testing.T) {
	d, err := NewLocalDestination(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalDestination: %v", err)
	}
	ctx := context.Background()
	keys := []string{
		"backups/default/20260101T000000.000Z-manual/db.dump",
		"backups/default/20260101T000000.000Z-manual/manifest.json",
		"backups/default/20260101T000001.000Z-nightly/db.dump",
	}
	for i, key := range keys {
		n, err := d.Put(ctx, key, strings.NewReader(strings.Repeat("x", i+1)))
		if err != nil {
			t.Fatalf("Put(%s): %v", key, err)
		}
		if int(n) != i+1 {
			t.Fatalf("Put(%s) wrote %d bytes, want %d", key, n, i+1)
		}
	}

	rc, size, err := d.Open(ctx, keys[0])
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rc.Close() }()
	data, _ := io.ReadAll(rc)
	if size != 1 || string(data) != "x" {
		t.Fatalf("Open returned size %d, data %q", size, data)
	}
	if _, _, err := d.Open(ctx, "backups/default/missing/db.dump"); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("Open of a missing object = %v, want %v", err, ErrObjectNotFound)
	}

	if err := d.DeletePrefix(ctx, "backups/default/20260101T000000.000Z-manual/"); err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}
	if _, _, err := d.Open(ctx, keys[1]); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("object survived DeletePrefix: %v", err)
	}
	if _, _, err := d.Open(ctx, keys[2]); err != nil {
		t.Fatalf("DeletePrefix removed an unrelated folder: %v", err)
	}
}

// TestLocalDestinationPagesAndOrders: a listing is in key order and pages with a
// continuation token.
func TestLocalDestinationPagesAndOrders(t *testing.T) {
	d, err := NewLocalDestination(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalDestination: %v", err)
	}
	ctx := context.Background()
	for _, name := range []string{"c", "a", "b"} {
		if _, err := d.Put(ctx, "backups/default/f/"+name, strings.NewReader(name)); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	var got []string
	after := ""
	for {
		items, next, err := d.ListPage(ctx, "backups/default/f/", after, 1)
		if err != nil {
			t.Fatalf("ListPage: %v", err)
		}
		for _, item := range items {
			got = append(got, strings.TrimPrefix(item.Key, "backups/default/f/"))
		}
		if next == "" {
			break
		}
		after = next
	}
	if strings.Join(got, "") != "abc" {
		t.Fatalf("listing order = %v, want a b c", got)
	}
}

// TestLocalDestinationRefusesUnsafeDeletes: a localization bug must never let a
// delete escape the backups folder.
func TestLocalDestinationRefusesUnsafeDeletes(t *testing.T) {
	d, err := NewLocalDestination(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalDestination: %v", err)
	}
	ctx := context.Background()
	if err := d.DeletePrefix(ctx, "etc/"); err == nil {
		t.Fatalf("DeletePrefix accepted a prefix outside the backups folder")
	}
	if err := d.Delete(ctx, "not-backups/x"); err == nil {
		t.Fatalf("Delete accepted a key outside the backups folder")
	}
}

// TestLocalDestinationIsPrivate: a local backup holds the same data as the
// database, so objects are 0600.
func TestLocalDestinationIsPrivate(t *testing.T) {
	root := t.TempDir()
	d, err := NewLocalDestination(root)
	if err != nil {
		t.Fatalf("NewLocalDestination: %v", err)
	}
	key := "backups/default/f/db.dump"
	if _, err := d.Put(context.Background(), key, strings.NewReader("secret")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := os.Stat(d.Location(key))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("object mode = %v, want 0600", info.Mode().Perm())
	}
}

// TestS3DestinationIsDeclaredNotImplemented: the seam exists and refuses, so a
// caller cannot mistake the stub for a working destination.
func TestS3DestinationIsDeclaredNotImplemented(t *testing.T) {
	if _, err := NewS3Destination(S3Options{Bucket: "b"}); !errors.Is(err, ErrS3NotImplemented) {
		t.Fatalf("NewS3Destination error = %v, want %v", err, ErrS3NotImplemented)
	}
}
