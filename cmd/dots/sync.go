package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// dots sync — push this machine's config changes, then update every other one.
//
// The workflow this replaces: edit a config, commit, push, then ssh into a1,
// v1 and v2 in turn and run dots update on each. Four steps that must not be
// forgotten, on a four-machine setup, which is exactly the kind of thing that
// drifts.
//
// It confirms twice, because the two halves fail differently. Pushing is
// outward-facing and hard to walk back. Updating a remote is recoverable but
// touches machines you are not looking at, and one of them runs long-lived
// sessions.

const syncSSHTimeout = 60 * time.Second

func runSyncCLI(args []string) int {
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

	rc := 0
	if !remotesOnly {
		code, proceed := syncLocal(repo, message, assumeYes)
		if code != 0 {
			// A failed push means the remotes would pull something that is not
			// there. Stop rather than reporting success on three machines for a
			// change none of them received.
			fmt.Fprintln(os.Stderr, "dots sync: local push failed — not touching the remotes")
			return code
		}
		if !proceed {
			return 0
		}
	}
	if !pushOnly {
		rc = syncRemotes(assumeYes)
	}
	return rc
}

// syncLocal commits anything outstanding and pushes.
func syncLocal(repo, message string, assumeYes bool) (int, bool) {
	if _, ok := currentBranch(repo); !ok {
		fmt.Fprintln(os.Stderr, "dots sync: detached HEAD — check out a branch before syncing")
		return 1, false
	}
	out, err := exec.Command("git", "-C", repo, "status", "--porcelain").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots sync: cannot read git status: %v\n", err)
		return 1, false
	}
	changed := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(changed) == 1 && changed[0] == "" {
		changed = nil
	}

	if len(changed) > 0 {
		fmt.Printf("%s\n", styTitle.Render("Local changes"))
		for _, l := range changed {
			fmt.Printf("  %s\n", styMuted.Render(l))
		}
		if message == "" {
			message = fmt.Sprintf("sync: update %s", plural(len(changed), "config", "configs"))
		}
		fmt.Printf("\n  commit message: %s\n", styValue.Render(message))
		if !confirm(fmt.Sprintf("Commit %s and push?", plural(len(changed), "file", "files")), assumeYes) {
			fmt.Println("  skipped")
			return 0, false
		}
		if code := runStreaming(repo, "git", "-C", repo, "add", "-A"); code != 0 {
			return code, false
		}
		if code := runStreaming(repo, "git", "-C", repo, "commit", "-m", message); code != 0 {
			return code, false
		}
	} else {
		fmt.Println(styMuted.Render("No local changes to commit."))
	}

	// Push regardless of whether we just committed: there may be earlier
	// commits that never went out, and the remotes cannot pull those either.
	branch, ok := currentBranch(repo)
	if !ok {
		// The branch was removed or HEAD detached after the preflight. Never
		// fall back to main: that can push a different ref than the commit made.
		fmt.Fprintln(os.Stderr, "dots sync: detached HEAD — check out a branch before syncing")
		return 1, false
	}
	ahead := commitsAhead(repo, branch)
	if ahead == 0 {
		fmt.Println(styMuted.Render("Nothing to push."))
		return 0, true
	}

	fmt.Printf("\n%s %s\n", styTitle.Render("Push"),
		styMuted.Render(fmt.Sprintf("%s ahead on %s", plural(ahead, "commit", "commits"), branch)))
	if !confirm("Push to origin?", assumeYes) {
		fmt.Println("  skipped")
		return 0, false
	}
	return runStreaming(repo, "git", "-C", repo, "push", "origin", branch), true
}

// syncRemotes runs `dots update` on each reachable host from ~/.ssh/config.
func syncRemotes(assumeYes bool) int {
	hosts := sshHosts()
	if len(hosts) == 0 {
		fmt.Println(styMuted.Render("No hosts in ~/.ssh/config."))
		return 0
	}

	fmt.Printf("\n%s %s\n", styTitle.Render("Machines"),
		styMuted.Render(strings.Join(hosts, ", ")))
	if !confirm(fmt.Sprintf("Update %s over SSH?", plural(len(hosts), "host", "hosts")), assumeYes) {
		fmt.Println("  skipped")
		return 0
	}

	failed := 0
	for _, h := range hosts {
		fmt.Printf("\n%s\n", styTitle.Render(h))

		if !reachable(h) {
			fmt.Printf("  %s\n", styMuted.Render("unreachable — skipped"))
			continue
		}

		// `dots update` on the far side rather than raw git: it pulls AND
		// relinks, which is what the local one does, and it keeps the logic in
		// one place instead of two that can disagree.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		cmd := exec.CommandContext(ctx, "ssh",
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=8",
			h, remoteDots("update"))
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		err := cmd.Run()
		cancel()

		if err != nil {
			fmt.Printf("  %s\n", styBad.Render("failed: "+err.Error()))
			failed++
			continue
		}
		fmt.Printf("  %s\n", styOK.Render("updated"))
	}

	if failed > 0 {
		fmt.Fprintf(os.Stderr, "\n%s\n",
			styBad.Render(fmt.Sprintf("%s failed", plural(failed, "host", "hosts"))))
		return 1
	}
	return 0
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

// commitsAhead counts what origin does not have yet.
//
// Compares against origin/<branch> rather than @{u}: upstream tracking is not
// set by `git push origin main`, so a repo whose remote was re-added by hand
// has none — and reporting "nothing to push" there would be wrong in the most
// misleading direction.
func commitsAhead(repo, branch string) int {
	_ = exec.Command("git", "-C", repo, "fetch", "-q", "origin", branch).Run()
	out, err := exec.Command("git", "-C", repo,
		"rev-list", "--count", "origin/"+branch+"..HEAD").Output()
	if err != nil {
		return 1 // unknown: assume there is something, and let push decide
	}
	n := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n); err != nil {
		return 1
	}
	return n
}

// sshHosts is the Sync view of the shared parsed SSH configuration.
func sshHosts() []string {
	hosts := parseSSHConfig()
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

func reachable(host string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), syncSSHTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes", "-o", "ConnectTimeout=8",
		host, "true").Run() == nil
}

// confirm asks on the terminal. With no terminal it declines rather than
// assuming yes — an unattended `dots sync` should not push on its own unless
// -y says so explicitly.
func confirm(question string, assumeYes bool) bool {
	if assumeYes {
		return true
	}
	if !isTTY() {
		fmt.Fprintf(os.Stderr, "dots sync: %s — declining, no terminal to ask on (use -y)\n", question)
		return false
	}
	fmt.Printf("  %s %s ", question, styMuted.Render("[y/N]"))
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	a := strings.ToLower(strings.TrimSpace(sc.Text()))
	return a == "y" || a == "yes"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
