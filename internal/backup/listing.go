package backup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Every listing of a destination is bounded: anyone who can write the
// destination could otherwise hold enough keys, or page without end, to exhaust
// the server's memory on every scheduled backup. A reader pages through
// Destination.ListPage and stops at a cap; past it the listing fails, so
// nothing is rotated or swept.
const (
	// listPageSize is the most objects one listing request asks for.
	listPageSize = 1000
	// maxBackupFolders bounds the backup folders of one server a listing reads.
	maxBackupFolders = 2000
	// maxFolderObjects bounds the objects of one backup folder. A backup writes
	// two; a folder with more than this was not written by a backup.
	maxFolderObjects = 32
	// maxListRequests bounds the requests one folder listing makes.
	maxListRequests = 1000
)

// ErrListingTooLarge refuses a listing past its cap.
var ErrListingTooLarge = errors.New("backup: the destination holds more objects than a backup write or read will read")

// folderObject is one object inside a backup folder.
type folderObject struct {
	name    string
	size    int64
	modTime time.Time
}

// folder is one backup folder as a listing read it.
type folder struct {
	id      string
	at      time.Time
	kind    Kind
	objects []folderObject
	// overflow is set when the folder holds more objects than maxFolderObjects,
	// which were not all read: its contents are unknown.
	overflow bool
}

func (f folder) object(name string) (folderObject, bool) {
	for _, o := range f.objects {
		if o.name == name {
			return o, true
		}
	}
	return folderObject{}, false
}

// newestModTime is the modification time of the folder's newest object, or the
// zero time when it holds none.
func (f folder) newestModTime() time.Time {
	var newest time.Time
	for _, o := range f.objects {
		if o.modTime.After(newest) {
			newest = o.modTime
		}
	}
	return newest
}

// listFolders lists this server's backup folders, bounded.
func (s *Service) listFolders(ctx context.Context) ([]folder, error) {
	return listBackupFolders(ctx, s.opts.Destination, serverPrefix(s.opts.ServerID))
}

// listBackupFolders lists the backup folders under prefix in key order, each
// with its objects. A child that is not a backup folder is skipped. It refuses
// more than maxBackupFolders folders or maxListRequests requests.
func listBackupFolders(ctx context.Context, d Destination, prefix string) ([]folder, error) {
	var out []folder
	after := ""
	for range maxListRequests {
		page, next, err := d.ListPage(ctx, prefix, after, listPageSize)
		if err != nil {
			return nil, err
		}
		for _, obj := range page {
			rest := strings.TrimPrefix(obj.Key, prefix)
			name, base, inFolder := strings.Cut(rest, "/")
			if !inFolder {
				continue
			}
			at, kind, ok := ParseFolderName(name)
			if !ok {
				// Not a backup folder (for example a full restore's
				// overwritten bytes): never touch it.
				continue
			}
			if n := len(out); n == 0 || out[n-1].id != name {
				if n == maxBackupFolders {
					return nil, fmt.Errorf("%w: more than %d backup folders under %s", ErrListingTooLarge, maxBackupFolders, prefix)
				}
				out = append(out, folder{id: name, at: at, kind: kind})
			}
			f := &out[len(out)-1]
			if len(f.objects) == maxFolderObjects {
				f.overflow = true
				continue
			}
			f.objects = append(f.objects, folderObject{name: base, size: obj.Size, modTime: obj.ModTime})
		}
		switch {
		case next == "":
			return out, nil
		case next <= after:
			return nil, errors.New("backup: the destination's listing did not advance")
		default:
			after = next
		}
	}
	return nil, fmt.Errorf("%w: listing %s took more than %d requests", ErrListingTooLarge, prefix, maxListRequests)
}
