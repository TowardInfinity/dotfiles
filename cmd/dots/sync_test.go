package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Without a terminal, sync must decline rather than assume yes. An unattended
// run pushing to origin on its own is not a thing anyone asked for.
func TestSyncDeclinesWithoutTTY(t *testing.T) {
	bin := buildDots(t)
	cmd := exec.Command(bin, "sync", "--push-only")
	cmd.Env = append(os.Environ(), "DOTS_TEST=1")
	out, _ := cmd.CombinedOutput()
	got := string(out)
	// Either there is nothing to do, or it declined for want of a terminal.
	// What must NOT appear is evidence of a push.
	if strings.Contains(got, "To github.com") || strings.Contains(got, "-> main") {
		t.Fatalf("sync pushed with no terminal and no -y:\n%s", got)
	}
	t.Logf("output: %s", strings.TrimSpace(got))
}

func TestSyncRejectsContradictoryFlags(t *testing.T) {
	bin := buildDots(t)
	out, err := exec.Command(bin, "sync", "--push-only", "--remotes-only").CombinedOutput()
	if err == nil {
		t.Error("expected non-zero for contradictory flags")
	}
	if !strings.Contains(string(out), "contradictory") {
		t.Errorf("unhelpful message: %s", out)
	}
}

func TestSyncHelpNeedsNoRepo(t *testing.T) {
	bin := buildDots(t)
	cmd := exec.Command(bin, "sync", "--help")
	cmd.Env = noRepoEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sync --help failed with no checkout: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "dots sync —") {
		t.Errorf("help did not print:\n%s", out)
	}
}

// Host parsing must skip wildcard and negated aliases — offering to ssh to
// "*" or "!laptop" would be actively bad.
func TestSSHHostsSkipsWildcards(t *testing.T) {
	hosts := sshHosts()
	for _, h := range hosts {
		if strings.ContainsAny(h, "*?!") {
			t.Errorf("wildcard alias returned as a host: %q", h)
		}
	}
	t.Logf("hosts: %v", hosts)
}

func TestSyncRefusesDetachedHEADBeforeStagingOrPushing(t *testing.T) {
	repo := newTestRepo(t)
	makeDiscoverableCheckout(t, repo)
	remote := filepath.Join(filepath.Dir(repo), "remote.git")

	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	before := git("--git-dir", remote, "rev-parse", "main")
	git("-C", repo, "checkout", "--detach")
	file := filepath.Join(repo, "detached.txt")
	if err := os.WriteFile(file, []byte("must remain unstaged\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DOTFILES_DIR", repo)
	if code := runSyncCLI([]string{"--yes"}); code == 0 {
		t.Fatal("dots sync accepted a detached HEAD")
	}
	if staged := git("-C", repo, "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("detached sync staged files: %q", staged)
	}
	if after := git("--git-dir", remote, "rev-parse", "main"); after != before {
		t.Errorf("origin/main changed from %s to %s after refused sync", before, after)
	}
}

func TestSyncDecliningLocalCommitStopsWorkflow(t *testing.T) {
	repo := newTestRepo(t)
	makeDiscoverableCheckout(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "declined.txt"), []byte("keep local\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(t.TempDir(), "ssh-ran")
	binDir := t.TempDir()
	ssh := filepath.Join(binDir, "ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\n: > \"$DOTS_SSH_MARKER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeSSH(t, map[string]string{
		".ssh/config": "Host remote\n  HostName remote.example\n",
	})
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOTS_SSH_MARKER", marker)
	t.Setenv("DOTFILES_DIR", repo)

	// Force the no-terminal path even when a developer runs `go test` from an
	// interactive shell. A declined local half must end the whole workflow;
	// reaching syncRemotes would execute the fake ssh and create marker.
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = r.Close()
	})

	if code := runSyncCLI(nil); code != 0 {
		t.Fatalf("declining local sync returned %d, want 0", code)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("remote sync ran after local decline; marker stat = %v", err)
	}
	if staged, err := exec.Command("git", "-C", repo, "diff", "--cached", "--name-only").Output(); err != nil {
		t.Fatal(err)
	} else if strings.TrimSpace(string(staged)) != "" {
		t.Errorf("declined sync staged files: %q", staged)
	}
}

// findRepo deliberately rejects arbitrary Git repositories: a dotfiles
// checkout must also contain the installer and docs. Integration tests that
// exercise the public CLI path need those markers or they can fall through to
// a developer's real checkout and turn a test into a live sync.
func makeDiscoverableCheckout(t *testing.T, repo string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"install.sh":   "#!/bin/sh\n",
		"docs/test.md": "# test\n",
	} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"add", "install.sh", "docs/test.md"},
		{"commit", "-m", "make test checkout discoverable"},
		{"push", "-q", "origin", "main"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}
