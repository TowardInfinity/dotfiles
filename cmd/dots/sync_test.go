package main

import (
	"os"
	"os/exec"
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

// Host parsing must skip wildcards and entries with no HostName — offering to
// ssh to "*" would be actively bad.
func TestSSHHostsSkipsWildcards(t *testing.T) {
	hosts := sshHosts()
	for _, h := range hosts {
		if strings.ContainsAny(h, "*?!") {
			t.Errorf("wildcard alias returned as a host: %q", h)
		}
	}
	t.Logf("hosts: %v", hosts)
}
