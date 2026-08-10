package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Retired outbound switches must fail before discovering a checkout or touching
// Git. A safe default must not silently retain the old push behavior.
func TestSyncRejectsRetiredOutboundFlagsWithoutSideEffects(t *testing.T) {
	bin := buildDots(t)
	for _, args := range [][]string{{"--push-only"}, {"-m", "message"}, {"--remotes-only"}, {"--yes"}} {
		cmd := exec.Command(bin, append([]string{"sync"}, args...)...)
		cmd.Env = noRepoEnv()
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("dots sync %v succeeded", args)
			continue
		}
		got := string(out)
		if !strings.Contains(got, "retired") || (!strings.Contains(got, "dots publish") && !strings.Contains(got, "dots rollout")) {
			t.Errorf("dots sync %v gave an unhelpful migration error:\n%s", args, got)
		}
	}
}

func TestSyncRejectsOldFlagCombinationWithExplicitMapping(t *testing.T) {
	bin := buildDots(t)
	out, err := exec.Command(bin, "sync", "--push-only", "--remotes-only").CombinedOutput()
	if err == nil {
		t.Error("expected non-zero for retired flags")
	}
	if !strings.Contains(string(out), "dots publish") {
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

func TestSyncDryRunFetchesThenRendersTheFullInboundPlan(t *testing.T) {
	repo := newTestRepo(t)
	makeDiscoverableCheckout(t, repo)
	t.Setenv("DOTFILES_DIR", repo)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
		_ = r.Close()
		_ = w.Close()
	})

	code := runSyncCLI([]string{"--dry-run"})
	_ = w.Close()
	os.Stdout = oldStdout
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("sync --dry-run returned %d:\n%s", code, out)
	}
	text := string(out)
	for _, want := range []string{
		"Apply the checked-out configuration",
		"Verify applied configuration",
		"Dry run: nothing changed.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("sync --dry-run omitted %q:\n%s", want, text)
		}
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
	if code := runSyncCLI(nil); code == 0 {
		t.Fatal("dots sync accepted a detached HEAD")
	}
	if staged := git("-C", repo, "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("detached sync staged files: %q", staged)
	}
	if after := git("--git-dir", remote, "rev-parse", "main"); after != before {
		t.Errorf("origin/main changed from %s to %s after refused sync", before, after)
	}
}

func TestSyncRefusesDirtyWorktreeWithoutContactingRemotes(t *testing.T) {
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

	if code := runSyncCLI(nil); code == 0 {
		t.Fatal("dirty sync succeeded")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("remote sync ran after dirty refusal; marker stat = %v", err)
	}
	if staged, err := exec.Command("git", "-C", repo, "diff", "--cached", "--name-only").Output(); err != nil {
		t.Fatal(err)
	} else if strings.TrimSpace(string(staged)) != "" {
		t.Errorf("dirty sync staged files: %q", staged)
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
