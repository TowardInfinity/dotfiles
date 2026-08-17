package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// indexSchema is bumped whenever Session or Index changes shape. A mismatch
// discards the file and rebuilds rather than migrating: this is derived state,
// reconstructible from transcripts and the vault by `dots memory reindex`, so a
// migration path would be code to maintain forever in exchange for saving a few
// seconds once. Same call as cmd/dots/fleetcache.go makes.
const indexSchema = 2

// Index is the whole store: every distilled session this machine knows about.
type Index struct {
	Schema   int       `json:"schema"`
	Sessions []Session `json:"sessions"`
	Built    time.Time `json:"built_at"`
}

// IndexPath is the cache file. XDG_CACHE_HOME then ~/.cache, matching
// fleetCachePath so both of this binary's caches live in one directory.
func IndexPath() string {
	root := os.Getenv("XDG_CACHE_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".cache")
	}
	return filepath.Join(root, "dots", "memory-index-v1.json")
}

// LoadIndex never fails. A missing, unreadable, corrupt or stale-schema file
// all mean the same thing to every caller — start empty — and one of those
// callers is a SessionStart hook that must not be able to stop Claude
// launching.
func LoadIndex() Index {
	b, err := os.ReadFile(IndexPath())
	if err != nil {
		return Index{Schema: indexSchema}
	}
	var idx Index
	if json.Unmarshal(b, &idx) != nil || idx.Schema != indexSchema {
		return Index{Schema: indexSchema}
	}
	return idx
}

// SaveIndex writes atomically: a half-written index is read by the hook on the
// next session start, and a truncated file there is a parse error at exactly
// the wrong moment. Temp file in the same directory (so Rename stays within one
// filesystem and is atomic), 0600 before any content lands, Sync before Rename.
func SaveIndex(idx Index) error {
	idx.Schema = indexSchema
	idx.Built = time.Now()

	path := IndexPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}

	return writeFileAtomic(path, b, 0o600)
}

// writeFileAtomic writes via a temp file in the same directory, so Rename stays
// within one filesystem and is atomic. Permissions are set before any content
// lands. Both callers need this: a half-written index is a parse error in the
// SessionStart hook, and a half-written note is what iCloud picks up and syncs
// to every other machine.
func writeFileAtomic(path string, b []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".dots-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Upsert replaces the session with the same tool and id, or appends it.
// Adapters rescan overlapping windows, so this is the common path, not an edge.
func (idx *Index) Upsert(s Session) {
	for i := range idx.Sessions {
		if idx.Sessions[i].Tool == s.Tool && idx.Sessions[i].ID == s.ID {
			idx.Sessions[i] = s
			return
		}
	}
	idx.Sessions = append(idx.Sessions, s)
}

// Find returns the stored session for a tool and id.
func (idx Index) Find(tool, id string) (Session, bool) {
	for _, s := range idx.Sessions {
		if s.Tool == tool && s.ID == id {
			return s, true
		}
	}
	return Session{}, false
}
