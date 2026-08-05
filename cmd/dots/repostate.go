package main

import (
	"os/exec"
	"strconv"
	"strings"
)

// repoState is what the checkout has that the other machines do not.
//
// The gap this closes: edit a config, relink locally, and everything looks
// right — on this box. a1 and the VMs stay on the old copy until someone
// remembers `dots sync`. Nothing surfaced that, so drift was silent and the
// only symptom was noticing weeks later that a machine behaved differently.
//
// This deliberately reports rather than acts. Pushing is outward-facing and
// updates machines you are not looking at, so it stays a deliberate command.
type repoState struct {
	dirty    int  // files with uncommitted changes
	unpushed int  // commits ahead of the tracking remote
	detached bool // not on a branch; sync would have nothing sensible to push
	ok       bool // false when this is not a git checkout, or git is absent
}

// needsSync is the one question callers ask.
func (s repoState) needsSync() bool { return s.ok && (s.dirty > 0 || s.unpushed > 0) }

// summary is the human sentence, e.g. "3 uncommitted files, 1 unpushed commit".
func (s repoState) summary() string {
	var parts []string
	if s.dirty > 0 {
		parts = append(parts, plural(s.dirty, "uncommitted file", "uncommitted files"))
	}
	if s.unpushed > 0 {
		parts = append(parts, plural(s.unpushed, "unpushed commit", "unpushed commits"))
	}
	return strings.Join(parts, ", ")
}

// readRepoState asks git. Every failure is treated as "nothing to report":
// doctor must stay useful on a machine installed with --copy, where there is
// no checkout at all, and a missing git is already its own doctor row.
func readRepoState(repo string) repoState {
	if repo == "" {
		return repoState{}
	}
	if _, err := exec.LookPath("git"); err != nil {
		return repoState{}
	}
	if err := exec.Command("git", "-C", repo, "rev-parse", "--git-dir").Run(); err != nil {
		return repoState{}
	}

	s := repoState{ok: true}

	// --porcelain is the stable, parseable form; -uno leaves untracked files
	// out because the repo is full of them by design on a working machine
	// (backups, caches) and counting those would cry wolf on every run.
	if out, err := exec.Command("git", "-C", repo, "status", "--porcelain", "-uno").Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.TrimSpace(line) != "" {
				s.dirty++
			}
		}
	}

	branch, err := exec.Command("git", "-C", repo, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		s.detached = true
		return s
	}
	b := strings.TrimSpace(string(branch))

	// Count against the remote branch directly rather than @{upstream}: a
	// checkout whose remote was re-added by hand has no tracking ref, and
	// dots update already works around the same gap.
	out, err := exec.Command("git", "-C", repo, "rev-list", "--count",
		"origin/"+b+"..HEAD").Output()
	if err != nil {
		return s // no origin ref yet (never fetched) — not something to nag about
	}
	if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
		s.unpushed = n
	}
	return s
}
