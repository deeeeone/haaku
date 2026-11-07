package backup

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Status is what a listing reports about one backup folder.
type Status string

const (
	// StatusComplete is a folder with a readable manifest that names every
	// object the folder holds: a valid recovery point.
	StatusComplete Status = "complete"
	// StatusInProgress is an incomplete folder that the run holding the lock is
	// currently writing.
	StatusInProgress Status = "in_progress"
	// StatusIncomplete is a folder without a manifest, or whose manifest names
	// an object that is not there. It is never a valid backup.
	StatusIncomplete Status = "incomplete"
	// StatusUnreadable is a folder whose manifest is present but cannot be read
	// or verified. It is kept, never rotated and never swept.
	StatusUnreadable Status = "unreadable"
)

const (
	// manifestFormat identifies the file to a reader that finds it by accident.
	manifestFormat = "haaku-backup"
	// manifestVersion is the version this build writes. A reader accepts any
	// version up to this one and refuses a newer one.
	manifestVersion = 1
	// maxManifestBytes bounds a manifest a reader will read.
	maxManifestBytes = 16 << 20
)

// ObjectEntry records one object in a backup: its name, its stored size, and
// the SHA-256 of its stored bytes.
type ObjectEntry struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// TableCount is one core table's presence and row count in the snapshot.
type TableCount struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	Rows    int64  `json:"rows"`
}

// Manifest is the metadata of one backup. Its presence in a folder is the
// commit point: it is written after every other object, and it lists each of
// them with a size and a checksum so a reader can tell a complete backup from a
// folder the run abandoned.
type Manifest struct {
	Format          string        `json:"format"`
	Version         int           `json:"version"`
	ID              string        `json:"id"`
	Kind            Kind          `json:"kind"`
	ServerID        string        `json:"server_id,omitempty"`
	StartedAt       time.Time     `json:"started_at"`
	CompletedAt     time.Time     `json:"completed_at"`
	AppVersion      string        `json:"app_version,omitempty"`
	PostgresVersion string        `json:"postgres_version,omitempty"`
	PgDumpVersion   string        `json:"pg_dump_version,omitempty"`
	DatabaseName    string        `json:"database_name,omitempty"`
	Encrypted       bool          `json:"encrypted,omitempty"`
	Objects         []ObjectEntry `json:"objects"`
	Tables          []TableCount  `json:"tables,omitempty"`
}

// Size is the total stored bytes of every object the manifest lists.
func (m *Manifest) Size() int64 {
	var total int64
	for _, o := range m.Objects {
		total += o.Size
	}
	return total
}

// object returns the entry named name.
func (m *Manifest) object(name string) (ObjectEntry, bool) {
	for _, o := range m.Objects {
		if o.Name == name {
			return o, true
		}
	}
	return ObjectEntry{}, false
}

// encode marshals the manifest and refuses one larger than a reader would read.
func (m *Manifest) encode() ([]byte, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encoding the manifest: %w", err)
	}
	if len(data) > maxManifestBytes {
		return nil, fmt.Errorf("the manifest is %d bytes, past the %d a reader reads", len(data), maxManifestBytes)
	}
	return data, nil
}

// parseManifest reads a manifest and checks it describes the folder it was read
// from: the format, a version this build understands, and the id, kind and
// server id of the folder. A manifest that fails any of these is refused, not
// trusted in part.
func parseManifest(data []byte, id, serverID string) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("reading the manifest: %w", err)
	}
	switch {
	case m.Format != manifestFormat:
		return nil, fmt.Errorf("the manifest format is %q, want %q", m.Format, manifestFormat)
	case m.Version < 1:
		return nil, fmt.Errorf("the manifest version is %d", m.Version)
	case m.Version > manifestVersion:
		return nil, fmt.Errorf("the manifest is version %d, newer than this build reads (%d)", m.Version, manifestVersion)
	case m.ID != id:
		return nil, fmt.Errorf("the manifest names backup %q, not the folder %q it was read from", m.ID, id)
	case !m.Kind.Valid():
		return nil, fmt.Errorf("the manifest kind is %q", m.Kind)
	}
	if at, kind, ok := ParseFolderName(id); !ok {
		return nil, fmt.Errorf("the folder name %q is not a backup id", id)
	} else if kind != m.Kind {
		return nil, fmt.Errorf("the manifest kind is %q, not the folder's %q", m.Kind, kind)
	} else if !at.Equal(m.StartedAt.UTC().Truncate(time.Millisecond)) && !at.Equal(m.StartedAt.UTC()) {
		// The folder time is the run's start, to the millisecond. A manifest
		// whose start disagrees cannot be trusted about anything else.
		return nil, fmt.Errorf("the manifest start %s does not match the folder time %s", m.StartedAt.UTC(), at.UTC())
	}
	if m.ServerID != "" && serverID != "" && m.ServerID != serverID {
		return nil, fmt.Errorf("the manifest names server %q, not %q", m.ServerID, serverID)
	}
	if len(m.Objects) == 0 {
		return nil, fmt.Errorf("the manifest lists no objects")
	}
	for _, o := range m.Objects {
		if o.Name == "" || strings.ContainsAny(o.Name, "/\x00") || o.Name == "." || o.Name == ".." {
			return nil, fmt.Errorf("the manifest lists an object named %q", o.Name)
		}
		if o.Size < 0 {
			return nil, fmt.Errorf("the manifest lists %s with a negative size", o.Name)
		}
	}
	return &m, nil
}
