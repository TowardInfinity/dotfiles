package main

import (
	"os"
	"strings"
	"testing"

	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
	"github.com/TowardInfinity/dotfiles/internal/dots/providers"
)

// The launchd restart is the one action here that can leave a service in a
// worse state than it found it: bootout succeeds, bootstrap races the teardown
// and fails with "Bootstrap failed: 5: Input/output error", and the service
// that was running is now stopped. It did exactly that to open-webui.

func launchdPlan(t *testing.T, verb string) ops.Plan {
	t.Helper()
	plan, ok := serviceAction(service{ID: "com.example.thing", Name: "thing", Source: srcLaunchd}, verb)
	if !ok {
		t.Fatalf("no plan for %q", verb)
	}
	return plan
}

func TestLaunchdRestartWaitsForTeardown(t *testing.T) {
	plan := launchdPlan(t, "restart")
	if len(plan.Steps) != 3 {
		t.Fatalf("restart has %d steps, want bootout/wait/bootstrap: %#v", len(plan.Steps), plan)
	}
	if plan.Steps[0].ID != "bootout" || plan.Steps[1].ID != "wait" || plan.Steps[2].ID != "bootstrap" {
		t.Fatalf("restart order = %q/%q/%q", plan.Steps[0].ID, plan.Steps[1].ID, plan.Steps[2].ID)
	}
	if _, ok := plan.Steps[1].Exec.(providers.Func); !ok {
		t.Errorf("wait uses %T, want typed provider", plan.Steps[1].Exec)
	}
}

// Bounded, or a stuck job hangs the TUI until the action timeout rather than
// reporting a failure.
func TestLaunchdRestartWaitIsBounded(t *testing.T) {
	if plan := launchdPlan(t, "restart"); plan.Timeout <= 0 {
		t.Error("restart has no timeout")
	}
}

// bootstrap must run even when the job was not loaded, which is why the
// separator is `;` and not `&&` — bootout fails in that case.
func TestLaunchdRestartBootstrapsEvenWhenNotLoaded(t *testing.T) {
	plan := launchdPlan(t, "restart")
	if _, ok := plan.Steps[0].Exec.(providers.Func); !ok {
		t.Errorf("bootout uses %T; expected provider that treats an unloaded label as ready", plan.Steps[0].Exec)
	}
}

// Arguments remain argv entries rather than being interpolated into a shell.
func TestLaunchdRestartQuotesItsArguments(t *testing.T) {
	if os.Getenv("HOME") == "" {
		t.Skip("no HOME")
	}
	plan, ok := serviceAction(service{ID: "com.example.odd name", Name: "odd", Source: srcLaunchd}, "restart")
	if !ok {
		t.Fatal("no plan")
	}
	for _, step := range []int{2} {
		process := planProcess(t, plan, step)
		if len(process.Argv) >= 2 && process.Argv[0] == "sh" && process.Argv[1] == "-c" {
			t.Fatalf("step %d regressed to shell interpolation: %v", step, process.Argv)
		}
	}
	if got := plan.Steps[0].Exec.Describe(); !strings.Contains(got, "odd name") || strings.Contains(got, "sh -c") {
		t.Errorf("service ID was not preserved by the typed provider: %q", got)
	}
}

// ── remote invocation ─────────────────────────────────────────

// `ssh host dots doctor` fails with "dots: command not found" on a machine
// where dots is installed and working: a non-interactive ssh shell reads
// neither .zshrc nor .profile, so ~/.local/bin is not on PATH. sync.go had
// already worked around this and the Manage pane had not, which is exactly the
// kind of divergence a shared helper exists to prevent.
func TestRemoteDotsCarriesLocalBinOnPath(t *testing.T) {
	got := remoteDots("doctor")
	if !strings.Contains(got, ".local/bin") {
		t.Errorf("remote command does not add ~/.local/bin to PATH: %s", got)
	}
	if !strings.Contains(got, "dots doctor") {
		t.Errorf("remote command lost its arguments: %s", got)
	}
	// $HOME must reach the far side unexpanded — the remote home is not ours.
	if strings.Contains(got, os.Getenv("HOME")) {
		t.Errorf("the local HOME was baked in; it must expand remotely: %s", got)
	}
	if !strings.Contains(got, "$HOME") {
		t.Errorf("expected $HOME to be passed through literally: %s", got)
	}
}

func TestRemoteDotsJoinsMultipleArgs(t *testing.T) {
	if got := remoteDots("doctor", "--online"); !strings.HasSuffix(got, "dots doctor --online") {
		t.Errorf("got %q", got)
	}
}

// Both remote entry points must go through the helper, or one of them drifts
// back to a bare `dots` and fails only on a machine nobody is looking at.
func TestRemoteDotsIsUsedByBothCallSites(t *testing.T) {
	for _, f := range []string{"manage.go", "sync.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		src := string(b)
		if strings.Contains(src, `"$HOME/.local/bin/dots`) {
			t.Errorf("%s spells out the install path instead of using remoteDots", f)
		}
		if strings.Contains(src, `alias, "dots",`) || strings.Contains(src, `h, "dots `) {
			t.Errorf("%s invokes a bare `dots` over ssh; it will not be on PATH", f)
		}
	}
}
