package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestParseChangeFilesKeepsStatusAndRenameTarget(t *testing.T) {
	files := parseChangeFiles(" M cmd/dots/main.go\nA  new.txt\n?? plans/dots-redesign.md\nR  old.txt -> new-name.txt\n")
	if len(files) != 4 {
		t.Fatalf("got %d files, want 4", len(files))
	}
	if files[0].Path != "cmd/dots/main.go" || files[0].Status != " M" || files[0].Staged {
		t.Fatalf("unexpected unstaged entry: %#v", files[0])
	}
	var renamed bool
	for _, file := range files {
		if file.Path == "new-name.txt" {
			renamed = true
		}
	}
	if !renamed {
		t.Fatal("rename target was not retained")
	}
}

func TestChangesSelectionBuildsTypedPublishRequest(t *testing.T) {
	m := changesModel{repo: findRepo(), files: []changeFile{{Path: "a.txt", Status: " M"}}, selected: map[string]bool{}}
	var cmd tea.Cmd
	m, _ = m.update(tea.KeyPressMsg{Code: tea.KeySpace})
	if !m.selected["a.txt"] {
		t.Fatal("space did not select the focused path")
	}
	m, cmd = m.update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if cmd == nil {
		t.Fatal("publish did not create an operation command")
	}
	msg := cmd()
	if _, ok := msg.(runActionMsg); !ok {
		t.Fatalf("publish command returned %T, want runActionMsg", msg)
	}
}

func TestChangesCommitMessageCanBeEditedBeforePublish(t *testing.T) {
	m := newChangesModel(findRepo())
	m.loading = false
	m.files = []changeFile{{Path: "a.txt", Status: " M"}}
	m, _ = m.update(tea.KeyPressMsg{Code: tea.KeySpace})
	m, _ = m.update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if !m.messageEditing {
		t.Fatal("m did not open the commit-message editor")
	}
	for _, r := range " review" {
		m, _ = m.update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m, _ = m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.messageEditing || m.commitMessage != "changes: publish selected paths review" {
		t.Fatalf("commit message = %q, editing=%t", m.commitMessage, m.messageEditing)
	}
	m, cmd := m.update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if cmd == nil {
		t.Fatal("publish did not create an operation command")
	}
	msg := cmd()
	planMsg, ok := msg.(runActionMsg)
	if !ok {
		t.Fatalf("publish command returned %T, want runActionMsg", msg)
	}
	if !strings.Contains(planMsg.plan.CommandSummary(), "review") {
		t.Fatalf("edited message did not reach publish plan: %s", planMsg.plan.CommandSummary())
	}
}

func TestFleetSnapshotRoundTripIsPrivateAndAtomic(t *testing.T) {
	cache := t.TempDir()
	old := os.Getenv("XDG_CACHE_HOME")
	if err := os.Setenv("XDG_CACHE_HOME", cache); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Setenv("XDG_CACHE_HOME", old) }()
	now := time.Now().UTC().Truncate(time.Second)
	want := fleetSnapshot{Schema: fleetCacheSchema, Checked: now, Hosts: []fleetSnapshotHost{{Alias: "a1", Outcome: "ok", ConfigOK: true, CheckedAt: now}}}
	if err := saveFleetSnapshot(want); err != nil {
		t.Fatal(err)
	}
	got := loadFleetSnapshot()
	if len(got.Hosts) != 1 || got.Hosts[0].Alias != "a1" || !got.Hosts[0].ConfigOK {
		t.Fatalf("round trip lost host state: %#v", got)
	}
	path := filepath.Join(cache, "dots", "fleet-status-v1.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode = %o, want 600", info.Mode().Perm())
	}
}

func TestFreshFleetSnapshotDoesNotNeedRefresh(t *testing.T) {
	now := time.Now()
	fresh := fleetSnapshot{Checked: now.Add(-time.Hour), Hosts: []fleetSnapshotHost{{Alias: "a1"}}}
	if fleetSnapshotNeedsRefresh(fresh, now) {
		t.Fatal("fresh fleet cache scheduled an unnecessary probe")
	}
	if !fleetSnapshotNeedsRefresh(fleetSnapshot{Checked: now, Hosts: nil}, now) {
		t.Fatal("empty fleet cache did not request its initial probe")
	}
	stale := fresh
	stale.Checked = now.Add(-13 * time.Hour)
	if !fleetSnapshotNeedsRefresh(stale, now) {
		t.Fatal("stale fleet cache did not request a refresh")
	}
}
