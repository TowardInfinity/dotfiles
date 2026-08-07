package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Writes a fake ~/.ssh tree and points HOME at it.
func fakeSSH(t *testing.T, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	for name, body := range files {
		p := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	return home
}

// A host in an Include'd file is a real machine. Missing it means `dots sync`
// silently never updates that box — the divergence sync exists to prevent.
func TestIncludedHostsAreFound(t *testing.T) {
	fakeSSH(t, map[string]string{
		".ssh/config":                "Host direct\n  HostName direct.example\n\nInclude config.d/*.conf\n",
		".ssh/config.d/servers.conf": "Host included\n  HostName included.example\n",
	})

	got := sshHosts()
	want := map[string]bool{"direct": true, "included": true}
	for _, h := range got {
		delete(want, h)
	}
	if len(want) != 0 {
		t.Errorf("sshHosts() = %v, missing %v", got, want)
	}

	// The Machines pane must agree, or the two views of "my machines" drift.
	var aliases []string
	for _, h := range parseSSHConfig() {
		aliases = append(aliases, h.alias)
	}
	if len(aliases) != len(got) {
		t.Errorf("parseSSHConfig() = %v but sshHosts() = %v — the two disagree", aliases, got)
	}
}

// Relative Include is resolved against ~/.ssh, not the working directory —
// otherwise the host list depends on where dots was launched from.
func TestRelativeIncludeIsResolvedAgainstSSHDir(t *testing.T) {
	fakeSSH(t, map[string]string{
		".ssh/config": "Include extra\n",
		".ssh/extra":  "Host viarel\n  HostName rel.example\n",
	})

	// Run from somewhere else entirely; the result must not change.
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	got := sshHosts()
	if len(got) != 1 || got[0] != "viarel" {
		t.Errorf("sshHosts() = %v, want [viarel]", got)
	}
}

// A tilde path in Include is expanded, and a cycle terminates instead of
// hanging the TUI.
func TestTildeIncludeAndCycleTerminates(t *testing.T) {
	fakeSSH(t, map[string]string{
		".ssh/config": "Include ~/.ssh/loop\n",
		".ssh/loop":   "Include ~/.ssh/loop\nHost looped\n  HostName loop.example\n",
	})

	done := make(chan []string, 1)
	go func() { done <- sshHosts() }()
	select {
	case got := <-done:
		if len(got) == 0 {
			t.Error("expected the looped host to still be reported once the depth cap stops recursion")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("sshConfigLines did not terminate on a cyclic Include")
	}
}

// Wildcards are templates, not machines, and an entry with no HostName is not
// a reachable target.
func TestWildcardsAndHostNamelessEntriesAreSkipped(t *testing.T) {
	fakeSSH(t, map[string]string{
		".ssh/config": "Host *\n  User me\n\nHost tmpl?\n  HostName t.example\n\n" +
			"Host nohost\n  User me\n\nHost real\n  HostName real.example\n",
	})

	got := sshHosts()
	if len(got) != 1 || got[0] != "real" {
		t.Errorf("sshHosts() = %v, want [real]", got)
	}
}

// No ~/.ssh/config at all is normal on a fresh box, not an error.
func TestMissingConfigIsSilent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got := sshHosts(); got != nil {
		t.Errorf("sshHosts() = %v, want nil", got)
	}
}
