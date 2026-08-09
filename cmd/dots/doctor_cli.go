package main

import (
	"fmt"
	"os"
)

// runDoctorCLI is `dots doctor` outside the TUI, sharing checkNames/evalCheck,
// configChecks, and packageChecks with the pane so the two can never disagree
// about what "installed" or "healthy" means — that distinction has already
// caused one round of confusion.
//
// Exits non-zero when something is missing or drifted, so it is usable in a
// script or a CI step, not only by eye. Warnings never affect the exit status:
// see checkWarn.
//
// The default run is offline. `--online` adds a single request to resolve the
// latest release, which is opt-in because doctor is the thing you run when
// something is already wrong, and that is often exactly when the network is
// the thing that is wrong.
func runDoctorCLI(repo string, args []string) int {
	online := false
	for _, a := range args {
		switch a {
		case "--online":
			online = true
		case "-h", "--help":
			fmt.Print(`dots doctor — check this machine's tools and configuration

  dots doctor            offline: tools, config files, managed block
  dots doctor --online   also compare the installed dots against the latest release

Exits non-zero if anything is missing, malformed, insecure or stale.
Warnings (offline, no checkout to compare against) do not affect the exit code.
`)
			return 0
		default:
			fmt.Fprintf(os.Stderr, "dots doctor: unknown flag %q\n", a)
			return 2
		}
	}

	results := make([]checkResult, 0, len(checkNames()))
	for _, n := range checkNames() {
		results = append(results, evalCheck(n))
	}
	results = append(results, configChecks(repo)...)
	results = append(results, packageChecks()...)
	if online {
		results = append(results, onlineCheck())
	}

	failed, warned := classify(results)
	lastGroup := ""
	fmt.Println()
	for _, r := range results {
		if g := checkGroup(r.name); g != lastGroup {
			if lastGroup != "" {
				fmt.Println()
			}
			fmt.Printf("  %s\n", styMuted.Render(g))
			lastGroup = g
		}
		switch r.state {
		case checkOK:
			fmt.Printf("  %s  %-14s %s\n",
				styOK.Render("✓"), r.name, styMuted.Render(r.path))
		case checkWarn:
			fmt.Printf("  %s  %-14s %s\n",
				styPending.Render("!"), r.name, styMuted.Render(r.path))
		default:
			detail := r.path
			if detail == "" {
				detail = "not found"
			}
			fmt.Printf("  %s  %-14s %s\n",
				styBad.Render("✗"), r.name, styMuted.Render(detail))
		}
	}

	// Repo drift is reported next to tool drift because they fail the same
	// way: this box looks fine while the others quietly disagree with it.
	// It is a note, never a failure — unpushed work is normal mid-edit, so it
	// must not change the exit status a script is reading.
	state := readRepoState(repo)
	if state.needsSync() {
		fmt.Println()
		fmt.Printf("  %s  %-14s %s\n", styPending.Render("!"), "repo", state.summary())
		fmt.Printf("     %s\n", styMuted.Render("publish reviewed paths with `dots publish`, then use `dots rollout`"))
	}

	fmt.Println()
	if len(failed) == 0 {
		msg := "Everything the configs call is installed, and the configuration matches the repo."
		if warned > 0 {
			msg = fmt.Sprintf("%s (%d check could not be answered — see the ! rows above.)", msg, warned)
		}
		fmt.Printf("  %s\n\n", styOK.Render(msg))
		return 0
	}

	fmt.Printf("  %s %v\n", styPending.Render(fmt.Sprintf("%d failing:", len(failed))), failed)

	// Three different repairs, and offering the wrong one wastes a --deps run
	// that installs nothing. Split the advice the same way the TUI splits the
	// keys.
	if anyConfigFailed(failed) {
		fmt.Printf("  Repair configuration with:\n    %s\n", styValue.Render("dots install"))
	}
	if anyToolFailed(failed) {
		fmt.Printf("  Install missing tools with:\n    %s\n",
			styValue.Render(`sh -c "$(curl -fsSL https://toin.in/install)" -- --deps`))
	}
	if anyPackageFailed(failed) {
		// No single command covers this group — a PATH fix and a missing
		// `uv tool install` aren't the same repair, so there is nothing to
		// hand `styValue` the way the two blocks above do. Point back at the
		// row instead of guessing which one applies.
		fmt.Printf("  Package fixes are per-row above — %s\n",
			styMuted.Render("--deps installs nothing new here"))
	}
	fmt.Println()
	_ = os.Stdout.Sync()
	return 1
}

// classify splits results into what fails the run and what merely could not be
// answered. Kept separate from the printing so the exit-status rule — warnings
// never fail, everything else does — can be tested without depending on which
// tools happen to be installed on the machine running the test.
func classify(results []checkResult) (failed []string, warned int) {
	for _, r := range results {
		switch r.state {
		case checkOK:
		case checkWarn:
			warned++
		default:
			failed = append(failed, r.name)
		}
	}
	return failed, warned
}

func anyConfigFailed(names []string) bool {
	for _, n := range names {
		if isConfigCheck(n) {
			return true
		}
	}
	return false
}

func anyToolFailed(names []string) bool {
	for _, n := range names {
		if !isConfigCheck(n) && !isPackageCheck(n) {
			return true
		}
	}
	return false
}

func anyPackageFailed(names []string) bool {
	for _, n := range names {
		if isPackageCheck(n) {
			return true
		}
	}
	return false
}
