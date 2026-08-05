package main

import (
	"fmt"
	"os"
)

// runDoctorCLI is `dots doctor` outside the TUI, sharing checkNames/evalCheck
// with the pane so the two can never disagree about what "installed" means —
// that distinction has already caused one round of confusion.
//
// Exits non-zero when something is missing, so it is usable in a script or a
// CI step, not only by eye.
func runDoctorCLI(repo string) int {
	results := make([]checkResult, 0, len(checkNames()))
	for _, n := range checkNames() {
		results = append(results, evalCheck(n))
	}

	var missing []string
	fmt.Println()
	for _, r := range results {
		switch r.state {
		case checkOK:
			fmt.Printf("  %s  %-14s %s\n",
				styOK.Render("✓"), r.name, styMuted.Render(r.path))
		default:
			fmt.Printf("  %s  %s\n", styBad.Render("✗"), r.name)
			missing = append(missing, r.name)
		}
	}

	// Config drift is reported next to tool drift because they fail the same
	// way: this box looks fine while the others quietly disagree with it.
	// It is a note, never a failure — unpushed work is normal mid-edit, so it
	// must not change the exit status a script is reading.
	state := readRepoState(repo)
	if state.needsSync() {
		fmt.Println()
		fmt.Printf("  %s  %-14s %s\n", styPending.Render("!"), "repo", state.summary())
		fmt.Printf("     %s\n", styMuted.Render("run `dots sync` to push and update the other machines"))
	}

	fmt.Println()
	if len(missing) == 0 {
		fmt.Printf("  %s\n\n", styOK.Render("Everything the configs call is installed."))
		return 0
	}

	fmt.Printf("  %s %v\n", styPending.Render(fmt.Sprintf("%d missing:", len(missing))), missing)
	fmt.Printf("  Install with:\n    %s\n\n",
		styValue.Render(`sh -c "$(curl -fsSL https://toin.in/install)" -- --deps`))
	_ = os.Stdout.Sync()
	return 1
}
