package ai

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/TowardInfinity/dotfiles/internal/dots/memory"
)

func TestRecentSessionsAndBestToolDoNotNeedMemoryIndex(t *testing.T) {
	home := t.TempDir()
	project := memory.ProjectKey("github.com/TowardInfinity/almanac")
	installLookupSeams(t, home, project)

	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	writeLookupFile(t, filepath.Join(home, ".claude", "projects", "x", "claude-1.jsonl"),
		`{"sessionId":"claude-1","cwd":"/work/almanac"}`+"\n", now.Add(-3*time.Hour))
	writeLookupFile(t, filepath.Join(home, ".claude", "projects", "y", "claude-1.jsonl"),
		`{"sessionId":"claude-1","cwd":"/work/almanac"}`+"\n", now.Add(-4*time.Hour))
	writeLookupFile(t, filepath.Join(home, ".codex", "sessions", "2026", "08", "18", "rollout-codex.jsonl"),
		`{"type":"session_meta","payload":{"id":"codex-1","cwd":"/work/almanac"}}`+"\n"+
			`{"type":"response_item","payload":{"type":"message","role":"user"}}`+"\n", now.Add(-time.Hour))
	// A newer subagent rollout must never win BestTool or be offered to resume.
	writeLookupFile(t, filepath.Join(home, ".codex", "sessions", "2026", "08", "18", "rollout-subagent.jsonl"),
		`{"type":"session_meta","payload":{"id":"subagent-newest","cwd":"/work/almanac","thread_source":"subagent"}}`+"\n"+
			`{"type":"response_item","payload":{"type":"message","role":"assistant"}}`+"\n", now)
	writeLookupFile(t, filepath.Join(home, ".grok", "sessions", "x", "grok-1", "summary.json"),
		`{"info":{"id":"grok-1","cwd":"/work/almanac"},"updated_at":"2026-08-18T10:00:00Z"}`+"\n", now.Add(-2*time.Hour))
	writeLookupFile(t, filepath.Join(home, ".cursor", "chats", "x", "cursor-1", "meta.json"),
		`{"cwd":"/work/almanac","updatedAtMs":1787040000000}`+"\n", now.Add(-4*time.Hour))
	// A newer session for another project must never affect this project.
	writeLookupFile(t, filepath.Join(home, ".codex", "sessions", "2026", "08", "18", "rollout-other.jsonl"),
		`{"type":"session_meta","payload":{"id":"other","cwd":"/work/other"}}`+"\n"+
			`{"type":"response_item","payload":{"type":"message","role":"user"}}`+"\n", now)

	got := RecentSessions(project)
	wantTools := []string{"codex", "grok", "claude", "cursor"}
	if len(got) != len(wantTools) {
		t.Fatalf("sessions = %#v, want %d", got, len(wantTools))
	}
	for i, want := range wantTools {
		if got[i].Tool != want {
			t.Fatalf("session %d tool = %q, want %q; all = %#v", i, got[i].Tool, want, got)
		}
	}
	tool, ref, _, ok := BestTool(project)
	if !ok || tool != "codex" || ref != "codex-1" {
		t.Fatalf("BestTool = %q, %q, ok=%v", tool, ref, ok)
	}
	ref, at := LastTouched("grok", project)
	if ref != "grok-1" || !at.Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("LastTouched(grok) = %q, %s", ref, at)
	}
}

func TestRecentSessionsUsesRecordedGrokRemoteWhenCWDIsGone(t *testing.T) {
	home := t.TempDir()
	project := memory.ProjectKey("github.com/TowardInfinity/almanac")
	installLookupSeams(t, home, project)
	writeLookupFile(t, filepath.Join(home, ".grok", "sessions", "x", "grok-1", "summary.json"),
		`{"info":{"id":"grok-1","cwd":"/gone"},"updated_at":"2026-08-18T10:00:00Z","git_remotes":["git@github.com:TowardInfinity/almanac.git"]}`+"\n", time.Now())

	got := RecentSessions(project)
	if want := []SessionRef{{Tool: "grok", ID: "grok-1", Updated: time.Date(2026, time.August, 18, 10, 0, 0, 0, time.UTC)}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sessions = %#v, want %#v", got, want)
	}
}

func installLookupSeams(t *testing.T, home string, project memory.ProjectKey) {
	t.Helper()
	oldHome, oldProject := lookupHome, lookupProject
	t.Cleanup(func() {
		lookupHome = oldHome
		lookupProject = oldProject
	})
	lookupHome = func() string { return home }
	lookupProject = func(dir string) memory.ProjectKey {
		if dir == "/work/almanac" {
			return project
		}
		return memory.ProjectKey("other")
	}
}

func writeLookupFile(t *testing.T, path, contents string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}
