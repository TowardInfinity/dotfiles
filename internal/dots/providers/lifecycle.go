package providers

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
)

// InboundGit is the guarded git half of dots sync. Its command vocabulary is
// intentionally closed: fetch, status/rev-list/log, and merge --ff-only. It
// has no add, commit, push, reset, stash, rebase, or SSH path.
type InboundGit struct {
	Repo   string
	Branch string
	Check  bool
	State  *InboundState
}

// InboundState carries the observed result to later conditional steps in the
// same Plan. A Plan owns its instance and Runner executes its steps serially.
type InboundState struct{ Changed bool }

func (g InboundGit) Describe() string {
	if g.Check {
		return "git fetch origin " + g.Branch + " (inspect only)"
	}
	return "git fetch origin " + g.Branch + " → git merge --ff-only origin/" + g.Branch
}

func (g InboundGit) Run(ctx context.Context, streams ops.IO) error {
	if g.Repo == "" || g.Branch == "" {
		return fmt.Errorf("inbound sync requires a checkout on a branch")
	}
	if err := requireBranch(ctx, g.Repo, g.Branch); err != nil {
		return err
	}
	if err := run(ctx, streams, g.Repo, "git", "-C", g.Repo, "fetch", "origin", g.Branch); err != nil {
		return fmt.Errorf("fetch origin/%s: %w", g.Branch, err)
	}
	if err := requireBranch(ctx, g.Repo, g.Branch); err != nil {
		return err
	}
	status, err := output(ctx, g.Repo, "git", "-C", g.Repo, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("read worktree status: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("worktree is dirty; refusing to fetch-and-apply over local changes")
	}
	counts, err := output(ctx, g.Repo, "git", "-C", g.Repo, "rev-list", "--left-right", "--count", "HEAD...origin/"+g.Branch)
	if err != nil {
		return fmt.Errorf("compare HEAD with origin/%s: %w", g.Branch, err)
	}
	fields := strings.Fields(counts)
	if len(fields) != 2 {
		return fmt.Errorf("unexpected git divergence result %q", strings.TrimSpace(counts))
	}
	ahead, aerr := strconv.Atoi(fields[0])
	behind, berr := strconv.Atoi(fields[1])
	if aerr != nil || berr != nil {
		return fmt.Errorf("unexpected git divergence result %q", strings.TrimSpace(counts))
	}
	switch {
	case ahead > 0 && behind > 0:
		return fmt.Errorf("checkout has diverged (%d ahead, %d behind); refusing", ahead, behind)
	case ahead > 0:
		return fmt.Errorf("checkout is %d commit(s) ahead; publish or reconcile it before syncing", ahead)
	case behind == 0:
		write(streams.Stdout, "Already current with origin/%s.\n", g.Branch)
		return nil
	}
	log, _ := output(ctx, g.Repo, "git", "-C", g.Repo, "log", "--oneline", "--no-decorate", "HEAD..origin/"+g.Branch)
	write(streams.Stdout, "Incoming %d commit(s):\n%s\n", behind, strings.TrimSpace(log))
	if g.Check {
		write(streams.Stdout, "Check only; checkout and configs were not changed.\n")
		return nil
	}
	if err := run(ctx, streams, g.Repo, "git", "-C", g.Repo, "merge", "--ff-only", "origin/"+g.Branch); err != nil {
		return fmt.Errorf("fast-forward checkout: %w", err)
	}
	if g.State != nil {
		g.State.Changed = true
	}
	return nil
}

// SSHScript runs a fixed provider-owned script with data passed as positional
// parameters. No host, path, or revision is interpolated into shell source.
type SSHScript struct {
	Host    string
	Script  string
	Args    []string
	Timeout int
	Label   string
}

func (s SSHScript) Describe() string {
	if s.Label != "" {
		return s.Label
	}
	return "ssh " + s.Host + " <typed script>"
}

func (s SSHScript) Run(ctx context.Context, streams ops.IO) error {
	if s.Host == "" || s.Script == "" {
		return fmt.Errorf("SSH provider needs a host and script")
	}
	if strings.HasPrefix(s.Host, "-") || strings.ContainsAny(s.Host, " \t\r\n") {
		return fmt.Errorf("unsafe SSH host %q", s.Host)
	}
	argv := []string{"-o", "BatchMode=yes"}
	if s.Timeout > 0 {
		argv = append(argv, "-o", fmt.Sprintf("ConnectTimeout=%d", s.Timeout))
	}
	remote := "sh -s --"
	for _, arg := range s.Args {
		remote += " " + shellQuote(arg)
	}
	argv = append(argv, s.Host, remote)
	cmd := exec.CommandContext(ctx, "ssh", argv...)
	cmd.Stdin = strings.NewReader(s.Script)
	cmd.Stdout, cmd.Stderr = streams.Stdout, streams.Stderr
	return cmd.Run()
}

func requireBranch(ctx context.Context, repo, expected string) error {
	branch, err := output(ctx, repo, "git", "-C", repo, "symbolic-ref", "--short", "HEAD")
	if err != nil || strings.TrimSpace(branch) == "" {
		return fmt.Errorf("detached HEAD — check out a branch before syncing")
	}
	if got := strings.TrimSpace(branch); got != expected {
		return fmt.Errorf("branch changed from %s to %s; refusing", expected, got)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func run(ctx context.Context, streams ops.IO, dir string, argv ...string) error {
	return Command(dir, argv...).Run(ctx, streams)
}

func output(ctx context.Context, dir string, argv ...string) (string, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	b, err := cmd.Output()
	return string(b), err
}

func write(w interface{ Write([]byte) (int, error) }, format string, args ...any) {
	if w != nil {
		_, _ = fmt.Fprintf(w, format, args...)
	}
}
