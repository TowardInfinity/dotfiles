package main

import (
	"os"
	"path/filepath"
	"strings"
)

// OpenSSH stops at 16 levels of Include. Matching it means a config that ssh
// itself accepts is never rejected here, and a cyclic include terminates
// rather than looping.
const sshIncludeMaxDepth = 16

// sshConfigLines returns the lines of ~/.ssh/config with every `Include`
// expanded in place, which is where ssh itself would read them.
//
// This exists because both the Machines pane and `dots sync` need the host
// list, and a host they cannot see is one that silently never gets updated —
// the exact divergence sync is for. Anyone who splits their config into
// ~/.ssh/config.d/* would have had those machines quietly skipped.
//
// It stays a best-effort reader, not an ssh_config implementation. `Match`
// blocks, `Host` negation and per-host option inheritance are not modelled;
// the callers only need Host aliases and their HostName. Where this is
// wrong it should over-report a host, never hide one — the cost of a
// spurious entry is a failed connection you can see, and the cost of a
// missing one is a machine that drifts unnoticed.
func sshConfigLines() []string {
	home := os.Getenv("HOME")
	return readSSHConfig(filepath.Join(home, ".ssh", "config"), home, 0)
}

func readSSHConfig(path, home string, depth int) []string {
	if depth > sshIncludeMaxDepth {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var out []string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "include") {
			out = append(out, line)
			continue
		}
		for _, pat := range fields[1:] {
			for _, m := range sshIncludeMatches(pat, home) {
				out = append(out, readSSHConfig(m, home, depth+1)...)
			}
		}
	}
	return out
}

// sshIncludeMatches resolves one Include argument to concrete files.
// A relative pattern is relative to ~/.ssh, which is what ssh does for a
// user config — resolving it against the process working directory would
// make the host list depend on where dots happened to be run from.
func sshIncludeMatches(pat, home string) []string {
	switch {
	case strings.HasPrefix(pat, "~/"):
		pat = filepath.Join(home, pat[2:])
	case !filepath.IsAbs(pat):
		pat = filepath.Join(home, ".ssh", pat)
	}
	matches, err := filepath.Glob(pat)
	if err != nil {
		return nil
	}
	// Sorted by Glob already, which keeps the host order stable across runs.
	return matches
}
