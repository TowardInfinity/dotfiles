package main

import (
	"os/exec"
	"strings"
	"testing"
)

// install/update/docs are verbs. They collide with docs/install.md and
// docs/update.md, and the collision used to resolve the wrong way: `dots
// update` printed the page about updating rather than updating.
func TestVerbsBeatPageNames(t *testing.T) {
	bin := buildDots(t)

	// --help proves the verb was dispatched, without changing anything.
	for _, verb := range []string{"install", "update"} {
		out, err := exec.Command(bin, verb, "--help").CombinedOutput()
		if err != nil {
			t.Fatalf("%s --help: %v\n%s", verb, err, out)
		}
		got := string(out)
		if !strings.Contains(got, "dots "+verb+" —") {
			t.Errorf("`dots %s --help` did not run the command; got:\n%s", verb, got)
		}
		// The doc page starts with prose, never with this header.
		if strings.Contains(got, "## The short version") {
			t.Errorf("`dots %s --help` printed the doc page instead", verb)
		}
	}

	// The pages must still be reachable, or the verbs have eaten them.
	for _, page := range []string{"install", "update"} {
		out, err := exec.Command(bin, "docs", page).CombinedOutput()
		if err != nil {
			t.Fatalf("docs %s: %v\n%s", page, err, out)
		}
		if len(strings.TrimSpace(string(out))) == 0 {
			t.Errorf("`dots docs %s` rendered nothing", page)
		}
	}
}

// A machine with no checkout is a normal state for a cached binary; it must
// say what to do rather than fail cryptically.
func TestInstallWithoutRepoExplains(t *testing.T) {
	bin := buildDots(t)
	cmd := exec.Command(bin, "install")
	cmd.Env = []string{"HOME=/tmp/dots-no-such-home", "PATH=/usr/bin:/bin", "DOTFILES_DIR=/tmp/dots-no-such-repo"}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("expected a non-zero exit with no checkout")
	}
	if !strings.Contains(string(out), "toin.in/install") {
		t.Errorf("error did not say how to get a checkout:\n%s", out)
	}
}

func TestInstallRejectsUnknownFlag(t *testing.T) {
	bin := buildDots(t)
	out, err := exec.Command(bin, "install", "--definitely-not-a-flag").CombinedOutput()
	if err == nil {
		t.Error("unknown flag should be a non-zero exit")
	}
	if !strings.Contains(string(out), "unknown option") {
		t.Errorf("unhelpful message: %s", out)
	}
}

func buildDots(t *testing.T) string {
	t.Helper()
	bin := t.TempDir() + "/dots"
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}
