package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func runStreaming(dir string, argv ...string) int {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if asExitError(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "dots: %s: %v\n", argv[0], err)
		return 1
	}
	return 0
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

	// --deps lives in bootstrap.sh, everything else in install.sh. Routing by
	// flag rather than always going through bootstrap keeps a plain relink from
	// re-running package detection it does not need.
	var argv []string
	if deps {
		argv = []string{"sh", filepath.Join(repo, "bootstrap.sh"), "--deps"}
	} else {
		argv = []string{filepath.Join(repo, "install.sh")}
	}
	if dry {
		argv = append(argv, "--dry")
	}
	return runStreaming(repo, argv...)
}

// runUpdateCLI is the command-line twin of Manage's `u`: pull, then relink.
func runUpdateCLI(args []string) int {
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Print(`dots update — pull the latest configs and relink

  dots update    git pull --ff-only, then re-run install.sh

The docs page about updating is: dots docs update
`)
			return 0
		default:
			fmt.Fprintf(os.Stderr, "dots update: unknown option: %s\n", a)
			return 1
		}
	}

	repo, ok := needRepo("update")
	if !ok {
		return 1
	}

	// Name the remote and branch explicitly. `git pull --ff-only` alone needs
	// upstream tracking, which `git push origin main` does not set — a repo
	// whose remote was re-added by hand has none, and the failure is a
	// screenful of git's own help text.
	branch, ok := currentBranch(repo)
	if !ok {
		fmt.Fprintln(os.Stderr, "dots update: detached HEAD — check out a branch before updating")
		return 1
	}

	if code := runStreaming(repo, "git", "-C", repo, "pull", "--ff-only", "origin", branch); code != 0 {
		fmt.Fprintln(os.Stderr, "dots: pull failed — leaving the checkout alone")
		return code
	}
	return runStreaming(repo, filepath.Join(repo, "install.sh"))
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
