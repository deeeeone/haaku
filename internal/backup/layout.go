package backup

import (
	"errors"
	"strings"
	"time"
)

// Kind says why a backup was taken. Rotation treats each kind differently.
type Kind string

const (
	// KindNightly is the scheduled backup.
	KindNightly Kind = "nightly"
	// KindManual is a "back up now" run.
	KindManual Kind = "manual"
	// KindBeforeRestore is taken by a restore before it replaces the database.
	KindBeforeRestore Kind = "before-restore"
	// KindBeforeUpgrade is taken before pending migrations are applied.
	KindBeforeUpgrade Kind = "before-upgrade"
)

// Valid reports whether k is a kind this package writes.
func (k Kind) Valid() bool {
	switch k {
	case KindNightly, KindManual, KindBeforeRestore, KindBeforeUpgrade:
		return true
	}
	return false
}

const (
	// RootPrefix holds every server's backups at a destination.
	RootPrefix = "backups/"

	// ManifestName is the commit point. It is written last.
	ManifestName = "manifest.json"
	// DumpName is the pg_dump custom-format archive.
	DumpName = "db.dump"

	// DefaultServerID is used when no server id is configured. A destination
	// that one server writes to needs only the default.
	DefaultServerID = "default"

	// folderTimeLayout is the UTC time, to the millisecond, at the start of a
	// folder name. secondTimeLayout is the same to the second, which an earlier
	// format wrote and ParseFolderName still reads.
	folderTimeLayout = "20060102T150405.000Z"
	secondTimeLayout = "20060102T150405Z"
)

var (
	// ErrNotFound reports a backup id that names no backup of this server.
	ErrNotFound = errors.New("backup: not found")
	// ErrIncomplete reports a folder without a manifest: it is never a valid
	// backup.
	ErrIncomplete = errors.New("backup: incomplete (no manifest)")
	// ErrUnreadable reports a folder whose manifest is present but cannot be
	// read or verified.
	ErrUnreadable = errors.New("backup: unreadable")
)

// FolderName is a backup's id: its UTC start time, to the millisecond, and its
// kind. A new backup's id comes from reserveID, which never names a folder that
// exists.
func FolderName(at time.Time, kind Kind) string {
	return at.UTC().Truncate(time.Millisecond).Format(folderTimeLayout) + "-" + string(kind)
}

// ParseFolderName reads a backup id, with a time to the millisecond or, as an
// earlier format wrote them, to the second. A name that is not a backup does not
// parse, so neither listing nor rotation ever touches it.
func ParseFolderName(name string) (time.Time, Kind, bool) {
	stamp, kind, found := strings.Cut(name, "-")
	if !found || !Kind(kind).Valid() {
		return time.Time{}, "", false
	}
	layout := folderTimeLayout
	if len(stamp) == len(secondTimeLayout) {
		layout = secondTimeLayout
	}
	at, err := time.Parse(layout, stamp)
	if err != nil || at.Format(layout) != stamp {
		return time.Time{}, "", false
	}
	return at, Kind(kind), true
}

// reserveID returns the id of a new backup of kind started at: one whose time
// no folder at the destination already has, of any kind and complete or not. A
// time in use (two runs in one millisecond, or a clock that went back) moves on
// a millisecond until it is free, so a backup never writes into another's folder
// and every folder's time is distinct, which keeps rotation's newest-first order
// total. folders must be listed under the subsystem lock, so nothing else takes
// the id before this run writes.
func reserveID(folders []folder, at time.Time, kind Kind) string {
	taken := make(map[time.Time]bool, len(folders))
	for _, f := range folders {
		taken[f.at.UTC()] = true
	}
	at = at.UTC().Truncate(time.Millisecond)
	for taken[at] {
		at = at.Add(time.Millisecond)
	}
	return FolderName(at, kind)
}

// serverPrefix is one server's folder at the destination.
func serverPrefix(serverID string) string { return RootPrefix + serverID + "/" }

// folderPrefix is one backup's folder, with a trailing slash.
func folderPrefix(serverID, id string) string { return serverPrefix(serverID) + id + "/" }

// objectKey is one object inside a backup folder.
func objectKey(serverID, id, name string) string { return folderPrefix(serverID, id) + name }

// validServerID accepts one safe path segment: the default, a UUID, or any
// other run of path-safe characters.
func validServerID(id string) bool {
	if id == "" || len(id) > 128 || id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		if !safeIDRune(r) {
			return false
		}
	}
	return true
}

func safeIDRune(r rune) bool {
	return r == '-' || r == '_' || r == '.' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
