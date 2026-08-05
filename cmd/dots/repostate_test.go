package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// git repo with one commit, an origin it is level with, and no working changes.
func newTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	work := filepath.Join(dir, "work")

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "--bare", "-b", "main", remote)
	run("init", "-b", "main", work)
	run("-C", work, "config", "user.email", "t@example.com")
	run("-C", work, "config", "user.name", "t")
	run("-C", work, "commit", "--allow-empty", "-m", "first")
	run("-C", work, "remote", "add", "origin", remote)
	run("-C", work, "push", "-q", "origin", "main")
	return work
}

func TestRepoStateCleanNeedsNoSync(t *testing.T) {
	s := readRepoState(newTestRepo(t))
	if !s.ok {
		t.Fatal("expected a recognised git checkout")
	}
	if s.needsSync() {
		t.Errorf("clean repo level with origin should not ask for a sync: %+v", s)
	}
}

func TestRepoStateCountsUnpushedCommits(t *testing.T) {
	work := newTestRepo(t)
	for i := 0; i < 2; i++ {
		exec.Command("git", "-C", work, "commit", "--allow-empty", "-m", "later").Run()
	}
	s := readRepoState(work)
	if s.unpushed != 2 {
		t.Errorf("unpushed = %d, want 2", s.unpushed)
	}
	if !s.needsSync() {
		t.Error("two unpushed commits should ask for a sync")
	}
	if !strings.Contains(s.summary(), "2 unpushed commits") {
		t.Errorf("summary = %q", s.summary())
	}
}

// Untracked files are deliberately not counted: a working machine accumulates
// backups and caches inside the repo, and counting those would nag on every
// run until the badge meant nothing.
func TestRepoStateIgnoresUntracked(t *testing.T) {
	work := newTestRepo(t)
	if err := exec.Command("sh", "-c", "echo x > "+filepath.Join(work, "scratch.tmp")).Run(); err != nil {
		t.Fatal(err)
	}
	if s := readRepoState(work); s.needsSync() {
		t.Errorf("untracked file should not trigger a sync nudge: %+v", s)
	}
}

func TestRepoStateCountsModifiedTrackedFiles(t *testing.T) {
	work := newTestRepo(t)
	f := filepath.Join(work, "tracked.txt")
	exec.Command("sh", "-c", "echo one > "+f).Run()
	exec.Command("git", "-C", work, "add", "tracked.txt").Run()
	exec.Command("git", "-C", work, "commit", "-m", "add").Run()
	exec.Command("git", "-C", work, "push", "-q", "origin", "main").Run()
	exec.Command("sh", "-c", "echo two > "+f).Run()

	s := readRepoState(work)
	if s.dirty != 1 {
		t.Errorf("dirty = %d, want 1", s.dirty)
	}
	if !strings.Contains(s.summary(), "1 uncommitted file") {
		t.Errorf("summary = %q", s.summary())
	}
}

// Outside a checkout — the --copy install — there is nothing to report and
// nothing to warn about.
func TestRepoStateOutsideGitIsSilent(t *testing.T) {
	if s := readRepoState(t.TempDir()); s.ok || s.needsSync() {
		t.Errorf("non-repo should report nothing: %+v", s)
	}
	if s := readRepoState(""); s.ok {
		t.Error("empty path should report nothing")
	}
}
