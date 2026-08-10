package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// dots sync is deliberately inbound-only. Publishing local changes and applying
// a published revision to machines are separate operations because combining
// them made a harmless-looking command commit, push, and SSH into the fleet.

func runSyncCLI(args []string) int {
	return runInboundSyncCLI(args)
}

func runInboundSyncCLI(args []string) int {
	check := false
	dryRun := false
	for _, arg := range args {
		switch arg {
		case "-h", "--help":
			fmt.Print(`dots sync — safely update this machine from origin

  dots sync             fetch origin, refuse local drift, fast-forward, then apply
  dots sync --check     fetch and report whether this checkout can advance
  dots sync --dry-run   fetch and render the remaining plan without changing files

Publishing is explicit: dots publish <paths> -m "message"
Fleet rollout is explicit: dots rollout <hosts>|--all
`)
			return 0
		case "--check":
			check = true
		case "--dry-run":
			dryRun = true
		case "-m", "--message", "--push-only":
			fmt.Fprintln(os.Stderr, "dots sync: this outbound option was retired; use `dots publish` to validate, commit, and push selected paths")
			return 2
		case "--remotes-only":
			fmt.Fprintln(os.Stderr, "dots sync: --remotes-only was retired; use `dots rollout --all` or name the machines")
			return 2
		case "-y", "--yes":
			fmt.Fprintln(os.Stderr, "dots sync: -y was for the retired combined operation; use -y with `dots publish` or `dots rollout` explicitly")
			return 2
		default:
			fmt.Fprintf(os.Stderr, "dots sync: unknown option: %s\n", arg)
			return 2
		}
	}
	repo, ok := needRepo("sync")
	if !ok {
		return 1
	}
	// --check and --dry-run make the fetch/inspection phase explicit. Bare
	// sync carries that same inspection forward into a fast-forward and apply.
	plan, err := buildOperation(syncInboundRequest{Repo: repo, Check: check || dryRun})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots sync: %v\n", err)
		return 1
	}
	if check || dryRun {
		fmt.Fprintln(os.Stderr, "preview: inbound-only sync; fetches and reports, but does not change the checkout or configs")
	}
	if code := runPlanCLI(plan, cliPlanOptions{}); code != 0 || !dryRun {
		return code
	}
	fullPlan, err := buildOperation(syncInboundRequest{Repo: repo})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots sync: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "Dry-run continuation: the fetch above is complete; these local steps were not run.")
	return runPlanCLI(fullPlan, cliPlanOptions{DryRun: true})
}

// ── helpers ──────────────────────────────────────────────────

func currentBranch(repo string) (string, bool) {
	out, err := exec.Command("git", "-C", repo, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return "", false
	}
	if b := strings.TrimSpace(string(out)); b != "" {
		return b, true
	}
	return "", false
}

// sshHosts is the Sync view of the shared parsed SSH configuration.
func sshHosts() []string {
	hosts := parseSSHConfig()
	if len(hosts) == 0 {
		return nil
	}
	out := make([]string, 0, len(hosts))
	for _, host := range hosts {
		out = append(out, host.alias)
	}
	return out
}

// remoteDots builds the command string for running dots on another machine.
//
// `ssh host dots doctor` fails with "dots: command not found" even when dots is
// installed and working. A non-interactive ssh command runs a shell that reads
// neither .zshrc nor .profile, so ~/.local/bin — where install.sh puts the
// symlink — is not on PATH. The machine is fine; only the lookup is broken, and
// the error points squarely at the wrong thing.
//
// Prepending to PATH rather than spelling out ~/.local/bin/dots: the explicit
// path is what sync used, and it silently assumes an install location. This
// finds dots there if it is there, and still finds it if it moved to
// /usr/local/bin or is already on the remote PATH.
//
// Single-quoted for the LOCAL shell to leave alone; $HOME expands on the far
// side, which is what we want — the remote home is not necessarily ours.
func remoteDots(args ...string) string {
	return `PATH="$HOME/.local/bin:$PATH" dots ` + strings.Join(args, " ")
}
