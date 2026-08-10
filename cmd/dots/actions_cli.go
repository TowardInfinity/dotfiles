package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
)

// The state-changing subcommands.
//
// These names collide with doc pages — docs/install.md and docs/update.md both
// exist — and the collision was silently resolved the wrong way: `dots update`
// printed the page about updating instead of updating anything, which the bash
// implementation it replaced did do. A verb typed at a shell should act. The
// pages are still reachable through `dots docs <topic>` and the Docs tab.
//
// Output is streamed straight to the terminal rather than captured. These
// commands run installers that take minutes and ask for sudo; a captured,
// silent run that then prints everything at once would look like a hang and
// could not answer a password prompt at all.

type cliPlanOptions struct {
	DryRun          bool
	AssumeYes       bool
	AskConfirm      bool
	CancelIsSuccess bool
}

func runPlanCLI(plan ops.Plan, options cliPlanOptions) int {
	fmt.Printf("Plan: %s\n", plan.Title)
	if plan.Summary != "" {
		fmt.Printf("  %s\n", plan.Summary)
	}
	fmt.Printf("  scope=%s  risk=%s\n", plan.Scope, plan.Risk)
	if plan.Target != "" {
		fmt.Printf("  target=%s\n", plan.Target)
	}
	for i, step := range plan.Steps {
		fmt.Printf("  %d. %s\n     %s\n", i+1, step.Title, step.Exec.Describe())
	}
	if options.DryRun {
		fmt.Println("Dry run: nothing changed.")
		return 0
	}
	if options.AskConfirm && plan.Confirm != "" && !options.AssumeYes {
		if !stdinIsTerminal() {
			fmt.Fprintln(os.Stderr, "dots: confirmation required outside a terminal; re-run with -y")
			if options.CancelIsSuccess {
				return 0
			}
			return 1
		}
		fmt.Fprintf(os.Stderr, "%s [y/N] ", plan.Confirm)
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(os.Stderr, "Cancelled; nothing changed.")
			if options.CancelIsSuccess {
				return 0
			}
			return 1
		}
	}

	timeout := plan.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result := operationRunner.Run(ctx, plan, ops.IO{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Event: func(event ops.Event) {
			if event.Kind == ops.EventStepStarted {
				fmt.Fprintf(os.Stderr, "==> %s\n", event.Title)
			} else if event.Kind == ops.EventStepDone && event.Status == ops.StatusSkipped && event.Err != nil {
				fmt.Fprintf(os.Stderr, "skip %s: %s\n", event.Title, event.Err)
			}
		},
	})
	if plan.Action == actionRollout || result.Status == ops.StatusPartial {
		printPlanResult(result)
	}
	if result.OK() {
		fmt.Printf("done  %s\n", plan.Title)
		return 0
	}
	if result.Error != "" {
		fmt.Fprintf(os.Stderr, "dots: %s failed: %s\n", plan.Action, result.Error)
	}
	if result.ExitCode != 0 {
		return result.ExitCode
	}
	return 1
}

func printPlanResult(result ops.Result) {
	for _, line := range planResultLines(result) {
		fmt.Println(line)
	}
}

func planResultLines(result ops.Result) []string {
	lines := []string{"Result: " + string(result.Status)}
	for _, step := range result.Steps {
		mark := "-"
		switch step.Status {
		case ops.StatusCompleted:
			mark = "ok"
		case ops.StatusFailed, ops.StatusCancelled:
			mark = "failed"
		case ops.StatusSkipped:
			mark = "skipped"
		}
		lines = append(lines, fmt.Sprintf("  %-7s %s", mark, step.Title))
	}
	return lines
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// needRepo returns the checkout, or explains why there isn't one and how to
// get one. A binary from the cache with no repo beside it is a normal state,
// not an error to be cryptic about.
func needRepo(what string) (string, bool) {
	repo := findRepo()
	if repo != "" {
		return repo, true
	}
	fmt.Fprintf(os.Stderr, "dots: no checkout found, so there is nothing to %s.\n", what)
	fmt.Fprintf(os.Stderr, "      Set it up with:\n")
	fmt.Fprintf(os.Stderr, "        sh -c \"$(curl -fsSL https://toin.in/install)\" -- --deps\n")
	fmt.Fprintf(os.Stderr, "      Or point DOTFILES_DIR at an existing one.\n")
	return "", false
}

// runInstallCLI re-runs the installer against an existing checkout: relink
// everything, and with --deps install the tools the configs call.
//
// This is repair and top-up, not first-time setup — you already have `dots`,
// so the machine is already installed. The curl one-liner is what bootstraps a
// bare machine, and the error path above says so.
func runInstallCLI(args []string) int {
	// Parse before touching the filesystem. Checking for a checkout first meant
	// `--help` and even an unknown flag reported "no checkout found" — help has
	// to work everywhere, and a typo deserves to be named as a typo. This
	// passed locally only because the tests happened to run inside the repo.
	deps, dry := false, false
	for _, a := range args {
		switch a {
		case "--deps":
			deps = true
		case "--dry", "-n":
			dry = true
		case "-h", "--help":
			fmt.Print(`dots install — re-run the installer against this checkout

  dots install            relink every config
  dots install --deps     also install the tools the configs call
  dots install --dry      print what would happen, change nothing

To set up a machine that has none of this yet:
  sh -c "$(curl -fsSL https://toin.in/install)" -- --deps
`)
			return 0
		default:
			fmt.Fprintf(os.Stderr, "dots install: unknown option: %s\n", a)
			return 1
		}
	}

	repo, ok := needRepo("install")
	if !ok {
		return 1
	}

	var req ops.Request
	if deps {
		req = depsRequest{Repo: repo}
	} else {
		req = applyRequest{Repo: repo}
	}
	plan, err := buildOperation(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots install: %v\n", err)
		return 1
	}
	return runPlanCLI(plan, cliPlanOptions{DryRun: dry, AskConfirm: true})
}

// runUpdateCLI remains for scripts and muscle memory. It is intentionally only
// an inbound alias now; bare sync owns the short, safe spelling.
func runUpdateCLI(args []string) int {
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Print(`dots update — deprecated alias for inbound sync

  dots update    same safe fetch, fast-forward, and apply as dots sync

Prefer: dots sync
`)
			return 0
		default:
			fmt.Fprintf(os.Stderr, "dots update: unknown option: %s\n", a)
			return 1
		}
	}

	fmt.Fprintln(os.Stderr, "notice: `dots update` is deprecated; use `dots sync`")
	return runInboundSyncCLI(nil)
}

// runDocsCLI is the explicit way to reach a page whose name is also a verb.
func runDocsCLI(args []string) int {
	if len(args) == 0 {
		docs, _ := loadDocs(findRepo())
		for _, d := range docs {
			fmt.Printf("%-18s %s\n", d.Name, d.Summary)
		}
		return 0
	}
	if !printDoc(args[0]) {
		return 1
	}
	return 0
}
