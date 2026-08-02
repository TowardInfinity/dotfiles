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
func runDoctorCLI() int {
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
