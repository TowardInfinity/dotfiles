package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
)

// The action overlay. Panes dispatch a typed request through operations.go;
// the root receives the resulting inert Plan and puts this overlay up.
//
// Output is streamed rather than collected, because the things worth running
// here — brew install, apt, a plugin restore — take long enough that a frozen
// screen would read as a hang. Nothing runs without a keypress, and anything
// that touches the system asks first.
type runActionMsg struct{ plan ops.Plan }
type actionPlanErrorMsg struct{ err error }

type actLineMsg string
type actStreamClosedMsg struct{}
type actDoneMsg struct {
	result ops.Result
}

type actionModel struct {
	plan      ops.Plan
	lines     []string
	vp        viewport.Model
	w, h      int
	confirm   bool
	running   bool
	done      bool
	cancelled bool
	code      int
	err       error
	result    ops.Result
	ch        chan tea.Msg
	cancel    context.CancelFunc
}

var operationRunner = ops.NewRunner()

func newAction(plan ops.Plan, w, h int) actionModel {
	a := actionModel{
		plan:    plan,
		w:       w,
		h:       h,
		confirm: plan.Confirm != "",
		ch:      make(chan tea.Msg, 256),
	}
	a.vp = newViewport(w-4, h-6)
	return a
}

func (a actionModel) start() (actionModel, tea.Cmd) {
	a.confirm = false
	a.running = true
	timeout := a.plan.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	a.cancel = cancel

	plan := a.plan
	ch := a.ch

	go func() {
		writer := &actionWriter{send: func(line string) { ch <- actLineMsg(line) }}
		result := operationRunner.Run(ctx, plan, ops.IO{
			Stdout: writer,
			Stderr: writer,
			Event: func(event ops.Event) {
				if event.Kind == ops.EventStepStarted {
					ch <- actLineMsg("==> " + event.Title)
				} else if event.Kind == ops.EventStepDone && event.Status == ops.StatusSkipped && event.Err != nil {
					ch <- actLineMsg("skip " + event.Title + ": " + event.Err.Error())
				}
			},
		})
		writer.Flush()
		ch <- actDoneMsg{result: result}
	}()

	return a, waitFor(ch)
}

type actionWriter struct {
	mu   sync.Mutex
	buf  string
	send func(string)
}

func (w *actionWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf += string(p)
	for {
		i := strings.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.send(strings.TrimSuffix(w.buf[:i], "\r"))
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

func (w *actionWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf != "" {
		w.send(strings.TrimSuffix(w.buf, "\r"))
		w.buf = ""
	}
}

// waitFor pulls one message off the channel. Re-issued after each message,
// which is the standard Bubble Tea way to consume a stream without blocking
// the update loop.
func waitFor(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return actStreamClosedMsg{}
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
	case tea.KeyPressMsg:
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
			// Running: cancel the command, not the overlay, so a long install
			// can be stopped without killing the program.
			//
			// Re-arming the stream here is essential. Returning nil dropped the
			// only pending read on the channel, so the actDoneMsg that follows
			// cancellation was never consumed — the overlay stayed "running"
			// forever and, because it owns the keyboard, took the whole TUI
			// down with it. Cancelling has to keep listening for the end it
			// just caused.
			if a.running {
				if a.cancel != nil {
					a.cancel()
				}
				a.cancelled = true
				return a, waitFor(a.ch), false
			}
			return a, nil, false

		case "ctrl+c":
			// Always available. The overlay consumes every key, so without an
			// explicit case there was no way out of a wedged action at all.
			if a.running {
				if a.cancel != nil {
					a.cancel()
				}
				a.cancelled = true
				return a, waitFor(a.ch), false
			}
			return a, nil, true

		case "j", "down":
			a.vp.ScrollDown(1)
			return a, nil, false
		case "k", "up":
			a.vp.ScrollUp(1)
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
		a.result = msg.result
		a.code = msg.result.ExitCode
		if a.plan.Action == actionRollout || msg.result.Status == ops.StatusPartial {
			a.lines = append(a.lines, "")
			a.lines = append(a.lines, planResultLines(msg.result)...)
			a.vp.SetContent(strings.Join(a.lines, "\n"))
			a.vp.GotoBottom()
		}
		if msg.result.Error != "" {
			a.err = fmt.Errorf("%s", msg.result.Error)
		}
		if a.cancel != nil {
			a.cancel()
		}
		return a, nil, false
	case actStreamClosedMsg:
		return a, nil, false
	}
	return a, nil, false
}

func (a actionModel) view(spin string) string {
	var head string
	switch {
	case a.confirm:
		head = styPending.Render("? ") + styTitle.Render(a.plan.Title)
	case a.running:
		head = spin + " " + styTitle.Render(a.plan.Title)
	case a.cancelled:
		head = styPending.Render("⊘ ") + styTitle.Render(a.plan.Title) +
			styMuted.Render("  stopped")
	case a.code == 0:
		head = styOK.Render("✓ ") + styTitle.Render(a.plan.Title)
	default:
		head = styBad.Render("✗ ") + styTitle.Render(a.plan.Title)
	}

	cmdLine := styMuted.Render(truncate(a.plan.CommandSummary(), a.w-6))

	var body, foot string
	if a.confirm {
		meta := fmt.Sprintf("scope=%s  risk=%s", a.plan.Scope, a.plan.Risk)
		if a.plan.Target != "" {
			targetWidth := a.w - 14
			if targetWidth < 10 {
				targetWidth = 10
			}
			meta += "\n  target=" + truncate(a.plan.Target, targetWidth)
		}
		body = "\n  " + styMuted.Render(meta) + "\n\n  " + a.plan.Confirm + "\n"
		foot = styHint.Render("  y run  ·  n cancel")
	} else {
		body = a.vp.View()
		switch {
		case a.running && a.cancelled:
			foot = styPending.Render("  stopping command…") + styHint.Render("  ·  waiting for it to exit")
		case a.running:
			foot = styHint.Render("  esc stop  ·  j/k scroll")
		case a.cancelled:
			foot = styPending.Render("  stopped") + styHint.Render("  ·  enter close  ·  j/k scroll")
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

	// Return the box at its natural size. The root shell owns placement and
	// hit-testing; centering it here makes the visible buttons and their hit
	// rectangles disagree whenever the overlay is later placed in a full
	// terminal frame.
	return box
}
