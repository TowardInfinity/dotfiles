package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The launchd restart is the one action here that can leave a service in a
// worse state than it found it: bootout succeeds, bootstrap races the teardown
// and fails with "Bootstrap failed: 5: Input/output error", and the service
// that was running is now stopped. It did exactly that to open-webui.

func launchdSpec(t *testing.T, verb string) actionSpec {
	t.Helper()
	spec, ok := launchdActionSpec(service{ID: "com.example.thing", Name: "thing", Source: srcLaunchd}, verb)
	if !ok {
		t.Fatalf("no spec for %q", verb)
	}
	return spec
}

func TestLaunchdRestartWaitsForTeardown(t *testing.T) {
	script := strings.Join(launchdSpec(t, "restart").Argv, " ")

	if !strings.Contains(script, "bootout") || !strings.Contains(script, "bootstrap") {
		t.Fatalf("restart does not bootout then bootstrap: %s", script)
	}
	// The whole point: something between the two that blocks on the label
	// going away. Without it the two calls race and the service stays down.
	if !strings.Contains(script, "while launchctl print") {
		t.Errorf("restart does not wait for the label to clear — it will race:\n%s", script)
	}
	if !strings.Contains(script, "sleep") {
		t.Errorf("the wait does not sleep, so it is a busy loop:\n%s", script)
	}
}

// Bounded, or a stuck job hangs the TUI until the action timeout rather than
// reporting a failure.
func TestLaunchdRestartWaitIsBounded(t *testing.T) {
	script := strings.Join(launchdSpec(t, "restart").Argv, " ")
	if !strings.Contains(script, "-lt 50") {
		t.Errorf("the wait has no iteration cap:\n%s", script)
	}
	if spec := launchdSpec(t, "restart"); spec.Timeout <= 0 {
		t.Error("restart has no timeout")
	}
}

// bootstrap must run even when the job was not loaded, which is why the
// separator is `;` and not `&&` — bootout fails in that case.
func TestLaunchdRestartBootstrapsEvenWhenNotLoaded(t *testing.T) {
	script := strings.Join(launchdSpec(t, "restart").Argv, " ")
	if strings.Contains(script, "bootout") && strings.Contains(script, "&& launchctl bootstrap") {
		t.Errorf("bootstrap is gated on bootout succeeding; an unloaded job would never start:\n%s", script)
	}
}

// The script is handed to `sh -c`, so a syntax error would surface only when
// someone pressed R on a real service.
func TestLaunchdRestartScriptParses(t *testing.T) {
	argv := launchdSpec(t, "restart").Argv
	if len(argv) != 3 || argv[0] != "sh" || argv[1] != "-c" {
		t.Fatalf("expected sh -c <script>, got %v", argv)
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(argv[2])
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("restart script does not parse: %v\n%s\n%s", err, out, argv[2])
	}
}

// The label and plist path are interpolated into a shell string, so a service
// ID with a space or a quote in it must not become extra shell words.
func TestLaunchdRestartQuotesItsArguments(t *testing.T) {
	if os.Getenv("HOME") == "" {
		t.Skip("no HOME")
	}
	spec, ok := launchdActionSpec(service{ID: "com.example.odd name", Name: "odd", Source: srcLaunchd}, "restart")
	if !ok {
		t.Fatal("no spec")
	}
	script := spec.Argv[2]
	if strings.Contains(script, "gui/501/com.example.odd name") &&
		!strings.Contains(script, "'") {
		t.Errorf("an ID with a space is unquoted:\n%s", script)
	}
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("an ID with a space breaks the script: %v\n%s", err, out)
	}
}
