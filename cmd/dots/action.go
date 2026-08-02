package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The action runner. Any pane can ask for a command to be run by returning a
// runActionMsg; the root model takes it from there and puts an overlay up.
//
// Output is streamed rather than collected, because the things worth running
// here — brew install, apt, a plugin restore — take long enough that a frozen
// screen would read as a hang. Nothing runs without a keypress, and anything
// that touches the system asks first.
type actionSpec struct {
	Title   string
	Argv    []string
	Dir     string
	Confirm string        // when set, require y before running
	Timeout time.Duration // 0 means the default
}

type runActionMsg struct{ spec actionSpec }

type actLineMsg string
type actDoneMsg struct {
	code int
	err  error
}

type actionModel struct {
	spec    actionSpec
	lines   []string
	vp      viewport.Model
	w, h    int
	confirm bool
	running bool
	done    bool
	code    int
	err     error
	ch      chan tea.Msg
	cancel  context.CancelFunc
}

func newAction(spec actionSpec, w, h int) actionModel {
	a := actionModel{
		spec:    spec,
		w:       w,
		h:       h,
		confirm: spec.Confirm != "",
		ch:      make(chan tea.Msg, 256),
	}
	a.vp = newViewport(w-4, h-6)
	return a
}

func (a actionModel) start() (actionModel, tea.Cmd) {
	a.confirm = false
	a.running = true
	timeout := a.spec.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	a.cancel = cancel

	spec := a.spec
	ch := a.ch

	go func() {
		defer close(ch)
		if len(spec.Argv) == 0 {
			ch <- actDoneMsg{code: 1, err: fmt.Errorf("nothing to run")}
			return
		}
		cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
		cmd.Dir = spec.Dir

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			ch <- actDoneMsg{code: 1, err: err}
			return
		}
		cmd.Stderr = cmd.Stdout.(io.Writer) // interleave, as a terminal would

		if err := cmd.Start(); err != nil {
			ch <- actDoneMsg{code: 1, err: err}
			return
		}

		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			ch <- actLineMsg(sc.Text())
		}

		code := 0
		if err := cmd.Wait(); err != nil {
			code = 1
			var ee *exec.ExitError
			if ok := asExitError(err, &ee); ok {
				code = ee.ExitCode()
			}
			ch <- actDoneMsg{code: code, err: err}
			return
		}
		ch <- actDoneMsg{code: code}
	}()

	return a, waitFor(ch)
}

func asExitError(err error, out **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*out = ee
		return true
	}
	return false
}

// waitFor pulls one message off the channel. Re-issued after each message,
// which is the standard Bubble Tea way to consume a stream without blocking
// the update loop.
func waitFor(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return actDoneMsg{code: 0}
		}
		return msg
	}
}

func (a actionModel) resize(w, h int) actionModel {
	a.w, a.h = w, h
	a.vp = newViewport(w-4, h-6)
	a.vp.SetContent(strings.Join(a.lines, "\n"))
	a.vp.GotoBottom()
	return a
}

// update returns done=true when the overlay should close.
func (a actionModel) update(msg tea.Msg) (actionModel, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y", "enter":
			if a.confirm {
				var cmd tea.Cmd
				a, cmd = a.start()
				return a, cmd, false
			}
			if a.done {
				return a, nil, true
			}
		case "n", "N", "esc", "q":
			if a.confirm {
				return a, nil, true // declined, nothing ran
			}
			if a.done {
				return a, nil, true
			}
			// Running: esc cancels the command rather than the overlay, so a
			// long install can be stopped without killing the whole program.
			if a.running && a.cancel != nil {
				a.cancel()
			}
			return a, nil, false
		case "j", "down":
			a.vp.LineDown(1)
			return a, nil, false
		case "k", "up":
			a.vp.LineUp(1)
			return a, nil, false
		}
		return a, nil, false

	case actLineMsg:
		a.lines = append(a.lines, string(msg))
		// Keep the buffer bounded; a verbose apt run is thousands of lines and
		// none of the early ones matter once it has scrolled past.
		if len(a.lines) > 2000 {
			a.lines = a.lines[len(a.lines)-2000:]
		}
		a.vp.SetContent(strings.Join(a.lines, "\n"))
		a.vp.GotoBottom()
		return a, waitFor(a.ch), false

	case actDoneMsg:
		a.running = false
		a.done = true
		a.code = msg.code
		a.err = msg.err
		if a.cancel != nil {
			a.cancel()
		}
		return a, nil, false
	}
	return a, nil, false
}

func (a actionModel) view() string {
	var head string
	switch {
	case a.confirm:
		head = styPending.Render("? ") + styTitle.Render(a.spec.Title)
	case a.running:
		head = styPending.Render("⟳ ") + styTitle.Render(a.spec.Title)
	case a.code == 0:
		head = styOK.Render("✓ ") + styTitle.Render(a.spec.Title)
	default:
		head = styBad.Render("✗ ") + styTitle.Render(a.spec.Title)
	}

	cmdLine := styMuted.Render(truncate(strings.Join(a.spec.Argv, " "), a.w-6))

	var body, foot string
	if a.confirm {
		body = "\n  " + a.spec.Confirm + "\n"
		foot = styHint.Render("  y run  ·  n cancel")
	} else {
		body = a.vp.View()
		switch {
		case a.running:
			foot = styHint.Render("  esc stop  ·  j/k scroll")
		case a.err != nil && a.code != 0:
			foot = styBad.Render(fmt.Sprintf("  exit %d", a.code)) +
				styHint.Render("  ·  enter close  ·  j/k scroll")
		default:
			foot = styOK.Render("  done") + styHint.Render("  ·  enter close  ·  j/k scroll")
		}
	}

	inner := lipgloss.JoinVertical(lipgloss.Left, head, cmdLine, body, foot)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cMac).
		Padding(0, 1).
		Width(a.w - 4).
		Render(inner)

	return lipgloss.Place(a.w, a.h, lipgloss.Center, lipgloss.Center, box)
}
