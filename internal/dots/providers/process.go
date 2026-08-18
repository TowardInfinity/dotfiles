// Package providers contains the side-effecting implementations used by ops
// Plans. Keeping process construction here prevents views from assembling
// shell commands and gives CLI and TUI one execution path.
package providers

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
	"golang.org/x/term"
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
	// Do not use CommandContext here. It kills only the direct process, while
	// package managers commonly leave helpers (downloaders, build scripts, or
	// shells) behind. A cancelled TUI action must stop the whole operation, not
	// merely detach its visible parent and leave the overlay waiting forever.
	cmd := exec.Command(p.Argv[0], p.Argv[1:]...)
	grouped := !isTerminal(streams.Stdin)
	configureProcessGroup(cmd, grouped)
	cmd.Dir = p.Dir
	cmd.Stdin = streams.Stdin
	pipes, err := configureOutputPipes(cmd, streams)
	if err != nil {
		return err
	}
	if len(p.Env) > 0 {
		cmd.Env = append(os.Environ(), p.Env...)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	copiesDone := startOutputCopies(pipes)
	done := make(chan error, 1)
	go func() {
		// Wait closes StdoutPipe and StderrPipe. Let their readers drain first,
		// otherwise a short-lived command can lose its final output in the race
		// between Wait closing the pipe and io.Copy reading it.
		<-copiesDone
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// Process completion and context cancellation can race. Never signal a
		// PID that Wait has already reaped: on a long-lived machine, that PID may
		// eventually refer to an unrelated process group.
		select {
		case err := <-done:
			return err
		default:
		}
		// Give package managers a brief chance to clean up before escalating.
		// The TUI has no terminal stdin, so its isolated group includes every
		// helper it started. Interactive CLI commands deliberately remain in the
		// foreground terminal group so a real prompt cannot be stopped by SIGTTIN.
		terminateProcess(cmd, grouped, false)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			terminateProcess(cmd, grouped, true)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				// Cmd.Wait normally returns once the parent is reaped. If an escaped
				// descendant has kept an output pipe open, close our read end and
				// return cancellation rather than trapping the action overlay forever.
				closeOutputPipes(pipes)
				if streams.Stderr != nil {
					_, _ = fmt.Fprintln(streams.Stderr, "dots: command ignored cancellation; output detached")
				}
			}
		}
		return ctx.Err()
	}
}

func isTerminal(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

type outputPipe struct {
	reader io.ReadCloser
	writer io.Writer
}

func configureOutputPipes(cmd *exec.Cmd, streams ops.IO) ([]outputPipe, error) {
	var pipes []outputPipe
	if streams.Stdout != nil {
		if _, direct := streams.Stdout.(*os.File); direct {
			cmd.Stdout = streams.Stdout
		} else {
			reader, err := cmd.StdoutPipe()
			if err != nil {
				return nil, err
			}
			pipes = append(pipes, outputPipe{reader: reader, writer: streams.Stdout})
		}
	}
	if streams.Stderr != nil {
		if _, direct := streams.Stderr.(*os.File); direct {
			cmd.Stderr = streams.Stderr
		} else {
			reader, err := cmd.StderrPipe()
			if err != nil {
				return nil, err
			}
			pipes = append(pipes, outputPipe{reader: reader, writer: streams.Stderr})
		}
	}
	return pipes, nil
}

func startOutputCopies(pipes []outputPipe) <-chan struct{} {
	done := make(chan struct{})
	if len(pipes) == 0 {
		close(done)
		return done
	}
	go func() {
		var copies sync.WaitGroup
		for _, pipe := range pipes {
			copies.Add(1)
			go func(pipe outputPipe) {
				defer copies.Done()
				_, _ = io.Copy(pipe.writer, pipe.reader)
			}(pipe)
		}
		copies.Wait()
		close(done)
	}()
	return done
}

func closeOutputPipes(pipes []outputPipe) {
	for _, pipe := range pipes {
		_ = pipe.reader.Close()
	}
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
