package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// dots sync — push this machine's config changes, then update every other one.
//
// The workflow this replaces: edit a config, commit, push, then ssh into a1,
// v1 and v2 in turn and run dots update on each. Four steps that must not be
// forgotten, on a four-machine setup, which is exactly the kind of thing that
// drifts.
//
// The compatibility action now renders both halves as one typed plan and asks
// once for that full outbound scope. Phase -1B removes this action entirely;
// new work uses separate publish and rollout plans and confirmations.

func runSyncCLI(args []string) int {
	for i := 0; i < len(args); i++ {
		if args[i] == "-m" || args[i] == "--message" {
			i++ // the message may itself be "--check"; it is still data
			continue
		}
		if args[i] == "--check" || args[i] == "--dry-run" {
			return runInboundCheckCLI(args)
		}
	}
	message := ""
	assumeYes := false
	pushOnly := false
	remotesOnly := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			fmt.Print(`dots sync — push local config changes, then update every machine

  dots sync                 commit + push here, then update each reachable host
  dots sync -m "message"    use your own commit message
  dots sync --push-only     commit and push, touch no remotes
  dots sync --remotes-only  update the remotes, never write to the repo
  dots sync -y              do not ask (for scripts; it still prints what it did)
  dots sync --check         preview the new inbound-only behavior (safe now)
  dots sync --dry-run       fetch and render the inbound plan; change nothing

Hosts come from ~/.ssh/config. Unreachable ones are skipped, not retried.
`)
			return 0
		case "-m", "--message":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "dots sync: -m needs a message")
				return 1
			}
			i++
			message = args[i]
		case "-y", "--yes":
			assumeYes = true
		case "--push-only":
			pushOnly = true
		case "--remotes-only":
			remotesOnly = true
		default:
			fmt.Fprintf(os.Stderr, "dots sync: unknown option: %s\n", args[i])
			return 1
		}
	}

	if pushOnly && remotesOnly {
		fmt.Fprintln(os.Stderr, "dots sync: --push-only and --remotes-only are contradictory")
		return 1
	}

	repo, ok := needRepo("sync")
	if !ok {
		return 1
	}

	plan, err := buildOperation(syncLegacyRequest{
		Repo: repo, Message: message, PushOnly: pushOnly, RemotesOnly: remotesOnly,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots sync: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "WARNING: `dots sync` still has legacy OUTBOUND scope in this compatibility release.")
	fmt.Fprintln(os.Stderr, "         Use `dots publish` and `dots rollout` separately; bare sync becomes inbound after fleet convergence.")
	return runPlanCLI(plan, cliPlanOptions{AssumeYes: assumeYes, AskConfirm: true, CancelIsSuccess: true})
}

func runInboundCheckCLI(args []string) int {
	dryRun := false
	for _, arg := range args {
		switch arg {
		case "--check":
		case "--dry-run":
			dryRun = true
		case "-m", "--message", "--push-only":
			fmt.Fprintln(os.Stderr, "dots sync: outbound options belong to `dots publish`")
			return 2
		case "--remotes-only", "-y", "--yes":
			fmt.Fprintln(os.Stderr, "dots sync: fleet options belong to `dots rollout`")
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
	plan, err := buildOperation(syncInboundRequest{Repo: repo, Check: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots sync: %v\n", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "preview: inbound-only sync; fetches and reports, but does not change the checkout or configs")
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

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
