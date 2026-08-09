// Package providers contains the side-effecting implementations used by ops
// Plans. Keeping process construction here prevents views from assembling
// shell commands and gives CLI and TUI one execution path.
package providers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
)

type Process struct {
	Argv []string
	Dir  string
	Env  []string
}

func Command(dir string, argv ...string) Process {
	return Process{Argv: append([]string(nil), argv...), Dir: dir}
}

func (p Process) Describe() string {
	if len(p.Argv) == 0 {
		return "<empty command>"
	}
	parts := make([]string, len(p.Argv))
	for i, arg := range p.Argv {
		parts[i] = quoteDisplay(arg)
	}
	return strings.Join(parts, " ")
}

func (p Process) Run(ctx context.Context, streams ops.IO) error {
	if len(p.Argv) == 0 {
		return fmt.Errorf("nothing to run")
	}
	cmd := exec.CommandContext(ctx, p.Argv[0], p.Argv[1:]...)
	cmd.Dir = p.Dir
	cmd.Stdin = streams.Stdin
	cmd.Stdout = streams.Stdout
	cmd.Stderr = streams.Stderr
	if len(p.Env) > 0 {
		cmd.Env = append(os.Environ(), p.Env...)
	}
	return cmd.Run()
}

func quoteDisplay(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\n'\"\\$;&|<>()[]{}*?!") {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}

type Func struct {
	Label string
	Do    func(context.Context, ops.IO) error
}

type Message string

func (m Message) Describe() string { return string(m) }

func (m Message) Run(_ context.Context, streams ops.IO) error {
	if streams.Stdout != nil {
		_, _ = fmt.Fprintln(streams.Stdout, string(m))
	}
	return nil
}

func (f Func) Describe() string { return f.Label }

func (f Func) Run(ctx context.Context, streams ops.IO) error {
	if f.Do == nil {
		return fmt.Errorf("provider %q has no implementation", f.Label)
	}
	return f.Do(ctx, streams)
}
