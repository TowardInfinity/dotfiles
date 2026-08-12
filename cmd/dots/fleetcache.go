package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const fleetCacheSchema = 1

type fleetSnapshotMsg struct{ snapshot fleetSnapshot }

type fleetSnapshot struct {
	Schema    int                 `json:"schema"`
	Hosts     []fleetSnapshotHost `json:"hosts"`
	Checked   time.Time           `json:"checked_at"`
	Partial   bool                `json:"partial"`
	LastError string              `json:"last_error,omitempty"`
}

type fleetSnapshotHost struct {
	Alias        string    `json:"alias"`
	Revision     string    `json:"revision,omitempty"`
	Version      string    `json:"version,omitempty"`
	BinarySource string    `json:"binary_source,omitempty"`
	KeyMarker    string    `json:"key_marker,omitempty"`
	ConfigOK     bool      `json:"config_ok"`
	Outcome      string    `json:"outcome"`
	CheckedAt    time.Time `json:"checked_at"`
}

func fleetCachePath() string {
	root := os.Getenv("XDG_CACHE_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".cache")
	}
	return filepath.Join(root, "dots", "fleet-status-v1.json")
}

func loadFleetSnapshot() fleetSnapshot {
	b, err := os.ReadFile(fleetCachePath())
	if err != nil {
		return fleetSnapshot{Schema: fleetCacheSchema}
	}
	var snapshot fleetSnapshot
	if json.Unmarshal(b, &snapshot) != nil || snapshot.Schema != fleetCacheSchema {
		return fleetSnapshot{Schema: fleetCacheSchema}
	}
	return snapshot
}

func saveFleetSnapshot(snapshot fleetSnapshot) error {
	snapshot.Schema = fleetCacheSchema
	path := fleetCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".fleet-status-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
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

func fleetSnapshotAge(snapshot fleetSnapshot) (time.Duration, bool) {
	if snapshot.Checked.IsZero() {
		return 0, false
	}
	return time.Since(snapshot.Checked), true
}

func (s fleetSnapshot) fresh(now time.Time) bool {
	if s.Checked.IsZero() {
		return false
	}
	return now.Sub(s.Checked) < 12*time.Hour
}

func (s fleetSnapshot) summary() string {
	if len(s.Hosts) == 0 {
		return "unknown"
	}
	good, partial := 0, 0
	for _, host := range s.Hosts {
		if host.Outcome == "ok" && host.ConfigOK {
			good++
		} else {
			partial++
		}
	}
	if partial > 0 {
		return plural(good, "healthy host", "healthy hosts") + ", " + plural(partial, "needs attention", "need attention")
	}
	return plural(good, "healthy host", "healthy hosts")
}

func fetchFleetSnapshot() tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		hosts := sshHosts()
		snapshot := fleetSnapshot{Schema: fleetCacheSchema, Checked: started}
		if len(hosts) == 0 {
			snapshot.LastError = "no concrete SSH hosts found"
			return fleetSnapshotMsg{snapshot: snapshot}
		}
		results := collectFleetStatus(hosts)
		snapshot.Hosts = make([]fleetSnapshotHost, 0, len(results))
		for _, result := range results {
			item := fleetSnapshotHost{Alias: result.Host, Revision: result.Revision, Version: result.Version, BinarySource: result.BinarySource, KeyMarker: result.KeyMarker, ConfigOK: result.ConfigOK, CheckedAt: started}
			if result.Error != "" {
				item.Outcome = "unreachable"
				snapshot.Partial = true
				if snapshot.LastError == "" {
					snapshot.LastError = strings.TrimSpace(result.Error)
				}
			} else if !result.ConfigOK {
				item.Outcome = "unhealthy"
				snapshot.Partial = true
			} else {
				item.Outcome = "ok"
			}
			snapshot.Hosts = append(snapshot.Hosts, item)
		}
		sort.Slice(snapshot.Hosts, func(i, j int) bool { return snapshot.Hosts[i].Alias < snapshot.Hosts[j].Alias })
		if err := saveFleetSnapshot(snapshot); err != nil && snapshot.LastError == "" {
			snapshot.LastError = "cache: " + err.Error()
		}
		return fleetSnapshotMsg{snapshot: snapshot}
	}
}

func fleetSnapshotNeedsRefresh(snapshot fleetSnapshot, now time.Time) bool {
	return len(snapshot.Hosts) == 0 || !snapshot.fresh(now)
}
