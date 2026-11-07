package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"
)

const (
	// DefaultKeep is how many complete backups rotation keeps when none is
	// configured.
	DefaultKeep = 7
	// beforeUpgradeKeep is how many pre-migration safety copies are kept. The
	// migration runner only ever needs the newest, and the one before it as a
	// fallback.
	beforeUpgradeKeep = 2
	// DefaultIncompleteGrace is how long an incomplete folder is kept before
	// the sweep removes it. A run that is still writing holds the lock, so a
	// folder older than this was abandoned.
	DefaultIncompleteGrace = 24 * time.Hour
)

var (
	// ErrUserDataMissing reports that a required core table is absent: a backup
	// must never claim to be complete without user data.
	ErrUserDataMissing = errors.New("backup: the database has no user data to back up")
	// ErrRestoreVerificationFailed reports that a restored database does not
	// match the manifest's inventory.
	ErrRestoreVerificationFailed = errors.New("backup: the restored database does not match the manifest")
)

// KeyProvider derives a purpose-scoped key. internal/secret implements it. The
// backup package accepts it as a dependency and defaults to nil, which means no
// encryption: at-rest encryption of destination objects is section 2 of issue
// #16 and is wired by a later change. Nothing here reads KeyProvider yet; it is
// the seam so the layout does not change when encryption lands.
type KeyProvider interface {
	DeriveKey(purpose string) ([]byte, error)
}

// KeyPurposeEncryption is the derivation purpose backup objects will use.
const KeyPurposeEncryption = "haaku/backup-encryption/v1"

// Options configure a Service.
type Options struct {
	// Destination is where backups are written. Required.
	Destination Destination
	// Dumper writes the database dump. Required.
	Dumper Dumper
	// Restorer replaces the database on restore. Required to call Restore.
	Restorer Restorer
	// Locker serialises the subsystem. Defaults to a process-local locker.
	Locker Locker
	// KeyProvider is the encryption seam. nil means no encryption.
	KeyProvider KeyProvider
	// ServerID names this server's folder. Defaults to DefaultServerID.
	ServerID string
	// AppVersion is recorded in the manifest.
	AppVersion string
	// Now is the clock, for tests. Defaults to time.Now.
	Now func() time.Time
	// Keep is how many complete backups rotation keeps (before-upgrade copies
	// are kept separately, two). Defaults to DefaultKeep.
	Keep int
	// RequireTables are core tables whose absence fails a backup. Defaults to
	// ["users"]: a backup must never claim to be complete without user data.
	RequireTables []string
	// IncompleteGrace is how long an abandoned incomplete folder is kept.
	// Defaults to DefaultIncompleteGrace.
	IncompleteGrace time.Duration
	// Logger receives operational messages. Defaults to slog.Default.
	Logger *slog.Logger
}

// Service takes, lists, rotates, deletes and restores backups.
type Service struct {
	opts Options
}

// New validates opts and returns a Service.
func New(opts Options) (*Service, error) {
	if opts.Destination == nil {
		return nil, errors.New("backup: a destination is required")
	}
	if opts.Dumper == nil {
		return nil, errors.New("backup: a dumper is required")
	}
	if opts.ServerID == "" {
		opts.ServerID = DefaultServerID
	}
	if !validServerID(opts.ServerID) {
		return nil, fmt.Errorf("backup: the server id %q is not one safe path segment", opts.ServerID)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Keep <= 0 {
		opts.Keep = DefaultKeep
	}
	if opts.RequireTables == nil {
		opts.RequireTables = []string{"users"}
	}
	if opts.IncompleteGrace <= 0 {
		opts.IncompleteGrace = DefaultIncompleteGrace
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Locker == nil {
		opts.Locker = NewMemoryLocker()
	}
	return &Service{opts: opts}, nil
}

// Summary describes one backup folder.
type Summary struct {
	ID          string        `json:"id"`
	Kind        Kind          `json:"kind"`
	Status      Status        `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
	SizeBytes   int64         `json:"size_bytes"`
	AppVersion  string        `json:"app_version,omitempty"`
	Location    string        `json:"location"`
	Objects     []ObjectEntry `json:"objects"`
	Tables      []TableCount  `json:"tables,omitempty"`
	Encrypted   bool          `json:"encrypted"`
}

// Listing is the whole state of the backup destination for this server.
type Listing struct {
	ServerID    string    `json:"server_id"`
	Destination string    `json:"destination"`
	Keep        int       `json:"keep"`
	Complete    int       `json:"complete"`
	Incomplete  int       `json:"incomplete"`
	Unreadable  int       `json:"unreadable"`
	Backups     []Summary `json:"backups"`
}

// Backup takes one backup of kind and returns its summary. The folder id is
// reserved under the lock; the manifest is written last, and its presence is the
// commit point. A failure before the manifest leaves an incomplete folder and
// deletes nothing. Rotation runs after a successful backup.
func (s *Service) Backup(ctx context.Context, kind Kind) (*Summary, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("backup: %q is not a backup kind", kind)
	}
	unlock, err := s.opts.Locker.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock.Unlock()

	startedAt := s.opts.Now().UTC()
	folders, err := s.listFolders(ctx)
	if err != nil {
		return nil, err
	}
	id := reserveID(folders, startedAt, kind)
	// The folder time may have moved on a millisecond; the manifest must record
	// the folder's own time, or a reader refuses it.
	reservedAt, _, _ := ParseFolderName(id)
	prefix := folderPrefix(s.opts.ServerID, id)
	s.opts.Logger.InfoContext(ctx, "taking backup", "component", "backup", "id", id, "kind", kind, "destination", prefix)

	entry, info, err := s.writeDump(ctx, prefix)
	if err != nil {
		// The folder, if any, has no manifest: it is incomplete and inert.
		s.opts.Logger.ErrorContext(ctx, "backup dump failed; leaving an incomplete folder", "component", "backup", "id", id, "error", err)
		return nil, err
	}

	tables, err := s.opts.Dumper.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	if err := checkRequiredTables(tables, s.opts.RequireTables); err != nil {
		return nil, err
	}

	manifest := &Manifest{
		Format:          manifestFormat,
		Version:         manifestVersion,
		ID:              id,
		Kind:            kind,
		ServerID:        s.opts.ServerID,
		StartedAt:       reservedAt,
		CompletedAt:     s.opts.Now().UTC(),
		AppVersion:      s.opts.AppVersion,
		PostgresVersion: info.PostgresVersion,
		PgDumpVersion:   info.PgDumpVersion,
		DatabaseName:    info.DatabaseName,
		Objects:         []ObjectEntry{entry},
		Tables:          tables,
	}
	if err := s.writeManifest(ctx, prefix, manifest); err != nil {
		return nil, err
	}
	s.opts.Logger.InfoContext(ctx, "backup complete", "component", "backup", "id", id, "bytes", entry.Size, "sha256", entry.SHA256)

	if deleted, err := s.rotateLocked(ctx); err != nil {
		// The backup succeeded; a rotation failure is reported but does not
		// undo it.
		s.opts.Logger.WarnContext(ctx, "backup rotation failed", "component", "backup", "error", err)
	} else if len(deleted) > 0 {
		s.opts.Logger.InfoContext(ctx, "rotated old backups", "component", "backup", "deleted", deleted)
	}
	return s.summaryFor(prefix, id, kind, reservedAt, manifest), nil
}

// writeDump streams the dump into the destination through a pipe, hashing the
// stored bytes, so nothing is spooled to disk and the manifest can record the
// dump's size and checksum.
func (s *Service) writeDump(ctx context.Context, prefix string) (ObjectEntry, DumpInfo, error) {
	pr, pw := io.Pipe()
	var info DumpInfo
	errc := make(chan error, 1)
	go func() {
		var dumpErr error
		info, dumpErr = s.opts.Dumper.Dump(ctx, pw)
		_ = pw.CloseWithError(dumpErr)
		errc <- dumpErr
	}()

	hasher := sha256.New()
	size, putErr := s.opts.Destination.Put(ctx, prefix+DumpName, io.TeeReader(pr, hasher))
	if putErr != nil {
		// Unblock the dumping goroutine so it does not leak.
		_ = pr.CloseWithError(putErr)
	}
	dumpErr := <-errc
	if dumpErr != nil {
		return ObjectEntry{}, DumpInfo{}, fmt.Errorf("dumping the database: %w", dumpErr)
	}
	if putErr != nil {
		return ObjectEntry{}, DumpInfo{}, fmt.Errorf("writing %s: %w", DumpName, putErr)
	}
	entry := ObjectEntry{Name: DumpName, Size: size, SHA256: hex.EncodeToString(hasher.Sum(nil))}
	return entry, info, nil
}

// writeManifest writes the manifest last. Its presence commits the backup.
func (s *Service) writeManifest(ctx context.Context, prefix string, m *Manifest) error {
	data, err := m.encode()
	if err != nil {
		return err
	}
	if _, err := s.opts.Destination.Put(ctx, prefix+ManifestName, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("writing the manifest (the backup stays incomplete): %w", err)
	}
	return nil
}

func (s *Service) summaryFor(prefix, id string, kind Kind, startedAt time.Time, m *Manifest) *Summary {
	completed := m.CompletedAt
	return &Summary{
		ID:          id,
		Kind:        kind,
		Status:      StatusComplete,
		CreatedAt:   startedAt,
		CompletedAt: &completed,
		SizeBytes:   m.Size(),
		AppVersion:  m.AppVersion,
		Location:    s.opts.Destination.Location(prefix),
		Objects:     m.Objects,
		Tables:      m.Tables,
		Encrypted:   m.Encrypted,
	}
}

// checkRequiredTables fails when a required core table is absent, so a backup
// never claims to be complete without user data.
func checkRequiredTables(tables []TableCount, required []string) error {
	if len(required) == 0 {
		return nil
	}
	present := make(map[string]bool, len(tables))
	for _, t := range tables {
		present[t.Name] = t.Present
	}
	for _, want := range required {
		if !present[want] {
			return fmt.Errorf("%w: the table %q is absent", ErrUserDataMissing, want)
		}
	}
	return nil
}

// inspect reads a folder's manifest, if any, and reports its status.
//
//   - no manifest object: incomplete;
//   - a manifest that cannot be read, or is too large, or names no object this
//     folder holds: unreadable (kept, never rotated, never swept);
//   - a manifest that lists an object the folder does not hold, or holds at a
//     different size: incomplete;
//   - otherwise: complete.
func (s *Service) inspect(ctx context.Context, f folder) (Status, *Manifest) {
	if f.overflow {
		return StatusUnreadable, nil
	}
	info, ok := f.object(ManifestName)
	if !ok {
		return StatusIncomplete, nil
	}
	if info.size > maxManifestBytes {
		return StatusUnreadable, nil
	}
	rc, _, err := s.opts.Destination.Open(ctx, objectKey(s.opts.ServerID, f.id, ManifestName))
	if err != nil {
		return StatusUnreadable, nil
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, maxManifestBytes+1))
	if err != nil || len(data) > maxManifestBytes {
		return StatusUnreadable, nil
	}
	m, err := parseManifest(data, f.id, s.opts.ServerID)
	if err != nil {
		return StatusUnreadable, nil
	}
	for _, o := range m.Objects {
		got, ok := f.object(o.Name)
		if !ok || got.size != o.Size {
			// A manifest that names an object that is not there keeps its
			// manifest; it is incomplete, not unreadable.
			return StatusIncomplete, m
		}
	}
	return StatusComplete, m
}

// List reports this server's backups, newest first, bounded by the listing
// caps. It does not take the subsystem lock: it writes nothing, and every object
// is written atomically and the manifest last, so a listing never sees a
// half-written folder. A backup in progress is simply not yet listed as
// complete.
func (s *Service) List(ctx context.Context) (Listing, error) {
	listing := Listing{
		ServerID:    s.opts.ServerID,
		Destination: s.opts.Destination.Location(serverPrefix(s.opts.ServerID)),
		Keep:        s.opts.Keep,
		Backups:     []Summary{},
	}
	folders, err := s.listFolders(ctx)
	if err != nil {
		return listing, err
	}
	sortNewestFirst(folders)
	for _, f := range folders {
		status, m := s.inspect(ctx, f)
		summary := Summary{
			ID:        f.id,
			Kind:      f.kind,
			Status:    status,
			CreatedAt: f.at,
			Location:  s.opts.Destination.Location(folderPrefix(s.opts.ServerID, f.id)),
			Objects:   []ObjectEntry{},
		}
		if m != nil {
			summary.CompletedAt = &m.CompletedAt
			summary.SizeBytes = m.Size()
			summary.AppVersion = m.AppVersion
			summary.Objects = m.Objects
			summary.Tables = m.Tables
			summary.Encrypted = m.Encrypted
		}
		switch status {
		case StatusComplete:
			listing.Complete++
		case StatusUnreadable:
			listing.Unreadable++
		default:
			listing.Incomplete++
		}
		listing.Backups = append(listing.Backups, summary)
	}
	return listing, nil
}

// Delete removes one backup of this server.
func (s *Service) Delete(ctx context.Context, id string) error {
	if _, _, ok := ParseFolderName(id); !ok {
		return ErrNotFound
	}
	unlock, err := s.opts.Locker.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock.Unlock()

	folders, err := s.listFolders(ctx)
	if err != nil {
		return err
	}
	for _, f := range folders {
		if f.id == id {
			return s.deleteFolder(ctx, f)
		}
	}
	return ErrNotFound
}

// deleteFolder removes every object in a folder, its manifest first: a
// destination that refuses deletes then leaves the backup whole, and a delete
// that stops partway leaves an incomplete folder the sweep finishes, never a
// manifest that lists objects that are gone.
func (s *Service) deleteFolder(ctx context.Context, f folder) error {
	prefix := folderPrefix(s.opts.ServerID, f.id)
	if err := s.opts.Destination.Delete(ctx, prefix+ManifestName); err != nil {
		return err
	}
	return s.opts.Destination.DeletePrefix(ctx, prefix)
}

// RestoreResult reports what a restore did.
type RestoreResult struct {
	ID               string `json:"id"`
	Objects          int    `json:"objects"`
	Bytes            int64  `json:"bytes"`
	Verified         bool   `json:"verified"`
	InventoryChecked bool   `json:"inventory_checked"`
}

// Restore replaces the current database with the named backup. It refuses an
// incomplete or unreadable folder, verifies the dump's size and checksum before
// running pg_restore, and, when the manifest records an inventory, confirms the
// restored database matches it.
func (s *Service) Restore(ctx context.Context, id string) (*RestoreResult, error) {
	if _, _, ok := ParseFolderName(id); !ok {
		return nil, ErrNotFound
	}
	if s.opts.Restorer == nil {
		return nil, errors.New("backup: no restorer is configured")
	}
	unlock, err := s.opts.Locker.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock.Unlock()

	folders, err := s.listFolders(ctx)
	if err != nil {
		return nil, err
	}
	var target *folder
	for i := range folders {
		if folders[i].id == id {
			target = &folders[i]
			break
		}
	}
	if target == nil {
		return nil, ErrNotFound
	}
	status, m := s.inspect(ctx, *target)
	switch status {
	case StatusComplete:
	case StatusIncomplete:
		return nil, fmt.Errorf("%w: %s", ErrIncomplete, id)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnreadable, id)
	}
	dump, ok := m.object(DumpName)
	if !ok {
		return nil, fmt.Errorf("%w: the manifest names no %s", ErrIncomplete, DumpName)
	}

	// Verify the stored dump against the manifest before loading it: a
	// truncated or replaced archive must not reach pg_restore.
	if err := s.verifyObject(ctx, objectKey(s.opts.ServerID, id, DumpName), dump); err != nil {
		return nil, err
	}
	rc, _, err := s.opts.Destination.Open(ctx, objectKey(s.opts.ServerID, id, DumpName))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	if err := s.opts.Restorer.Restore(ctx, rc); err != nil {
		return nil, err
	}

	result := &RestoreResult{ID: id, Objects: len(m.Objects), Bytes: m.Size(), Verified: true}
	if len(m.Tables) > 0 {
		now, err := s.opts.Dumper.Inventory(ctx)
		if err != nil {
			return result, fmt.Errorf("verifying the restore: %w", err)
		}
		if err := compareInventory(m.Tables, now); err != nil {
			result.Verified = false
			return result, err
		}
		result.InventoryChecked = true
	}
	return result, nil
}

// verifyObject reads an object and checks its size and SHA-256 against the
// manifest entry, so a tampered or truncated archive is refused before it is
// loaded.
func (s *Service) verifyObject(ctx context.Context, key string, want ObjectEntry) error {
	rc, size, err := s.opts.Destination.Open(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	if size != want.Size {
		return fmt.Errorf("%w: %s is %d bytes, the manifest records %d", ErrUnreadable, key, size, want.Size)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, rc); err != nil {
		return fmt.Errorf("backup: reading %s: %w", key, err)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != want.SHA256 {
		return fmt.Errorf("%w: %s checksum is %s, the manifest records %s", ErrUnreadable, key, got, want.SHA256)
	}
	return nil
}

// compareInventory confirms each table the manifest recorded as present is
// present with the same row count now.
func compareInventory(before, after []TableCount) error {
	now := make(map[string]TableCount, len(after))
	for _, t := range after {
		now[t.Name] = t
	}
	for _, want := range before {
		if !want.Present {
			continue
		}
		got, ok := now[want.Name]
		if !ok || !got.Present {
			return fmt.Errorf("%w: the table %q is absent after the restore, but the manifest recorded %d rows", ErrRestoreVerificationFailed, want.Name, want.Rows)
		}
		if got.Rows != want.Rows {
			return fmt.Errorf("%w: the table %q holds %d rows, the manifest recorded %d", ErrRestoreVerificationFailed, want.Name, got.Rows, want.Rows)
		}
	}
	return nil
}

func sortNewestFirst(folders []folder) {
	sort.Slice(folders, func(i, j int) bool {
		if folders[i].at.Equal(folders[j].at) {
			return folders[i].id > folders[j].id
		}
		return folders[i].at.After(folders[j].at)
	})
}
