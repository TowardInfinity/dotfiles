package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Renders every tab at several sizes and asserts nothing panics and nothing
// exceeds the terminal width. Width overflow is the failure mode that wrecks a
// TUI layout, and it only shows up at sizes you did not try by hand.
func TestTabsRenderWithinWidth(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 32}, {200, 50}, {60, 18}}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		m := newModel()
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: h})

		for tab := 0; tab < int(numTabs); tab++ {
			tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune('1' + tab)}})
			out := tm.View()
			if strings.TrimSpace(out) == "" {
				t.Fatalf("tab %d empty at %dx%d", tab, w, h)
			}
			for i, line := range strings.Split(out, "\n") {
				if got := lipgloss.Width(line); got > w {
					t.Errorf("tab %d line %d overflows at %dx%d: %d > %d",
						tab, i, w, h, got, w)
				}
			}
		}
	}
}

// Docs navigation must never land the cursor on a group label or out of range.
func TestDocsNavigation(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 120, Height: 32})

	for i := 0; i < 60; i++ {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		mm := tm.(model)
		if d := mm.docs.current(); d == nil {
			t.Fatalf("cursor landed on no document after %d downs", i+1)
		}
	}
	for i := 0; i < 60; i++ {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		mm := tm.(model)
		if d := mm.docs.current(); d == nil {
			t.Fatalf("cursor landed on no document after %d ups", i+1)
		}
	}
}

// A filter that matches nothing must render, not crash.
func TestDocsFilterNoMatches(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "zzzznope" {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if strings.TrimSpace(tm.View()) == "" {
		t.Fatal("empty view for a filter with no matches")
	}
}

// Every page must carry front-matter and have it stripped from the body.
func TestDocsHaveFrontMatter(t *testing.T) {
	docs, _ := loadDocs(findRepo())
	if len(docs) < 20 {
		t.Fatalf("expected the full set of pages, got %d", len(docs))
	}
	for _, d := range docs {
		if d.Group == "" || d.Title == d.Name && d.Order == 999 {
			t.Errorf("%s: missing or unparsed front-matter", d.Name)
		}
		if strings.HasPrefix(strings.TrimSpace(d.Body), "---") {
			t.Errorf("%s: front-matter leaked into the body", d.Name)
		}
		if _, ok := groupOrder[d.Group]; !ok {
			t.Errorf("%s: unknown group %q", d.Name, d.Group)
		}
	}
}

// The overlay must take the keyboard while it is up. A stray tab switching
// panes underneath a running install would be both confusing and unsafe.
func TestActionOverlayCapturesKeys(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	plan := testCommandPlan("Probe", "echo", "hello")
	plan.Confirm = "Run the probe?"
	tm, _ = tm.Update(runActionMsg{plan: plan})
	if tm.(model).act == nil {
		t.Fatal("overlay did not open")
	}
	if !tm.(model).act.confirm {
		t.Fatal("a spec with Confirm must wait for confirmation")
	}

	before := tm.(model).tab
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyTab})
	if tm.(model).tab != before {
		t.Error("tab switched panes while the overlay was up")
	}
	if strings.TrimSpace(tm.View()) == "" {
		t.Error("overlay rendered empty")
	}

	// Declining must close it without running anything.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if tm.(model).act != nil {
		t.Error("declining did not close the overlay")
	}
}

// A confirmed action must actually run and capture output.
func TestActionRuns(t *testing.T) {
	a := newAction(testCommandPlan("Probe", "echo", "marker-line"), 80, 20)
	a, cmd := a.start()
	if cmd == nil {
		t.Fatal("start returned no command")
	}
	deadline := time.After(10 * time.Second)
	for !a.done {
		select {
		case <-deadline:
			t.Fatal("action never completed")
		default:
		}
		msg := cmd()
		var closed bool
		a, cmd, closed = a.update(msg)
		_ = closed
		if cmd == nil && !a.done {
			t.Fatal("stream ended without a done message")
		}
	}
	if a.code != 0 {
		t.Errorf("exit %d, want 0", a.code)
	}
	if !strings.Contains(strings.Join(a.lines, "\n"), "marker-line") {
		t.Errorf("output not captured: %q", a.lines)
	}
}

// A failing command must be reported, not swallowed.
func TestActionReportsFailure(t *testing.T) {
	a := newAction(testCommandPlan("Fail", "sh", "-c", "exit 3"), 80, 20)
	a, cmd := a.start()
	deadline := time.After(10 * time.Second)
	for !a.done {
		select {
		case <-deadline:
			t.Fatal("action never completed")
		default:
		}
		a, cmd, _ = a.update(cmd())
		if cmd == nil && !a.done {
			t.Fatal("stream ended without a done message")
		}
	}
	if a.code == 0 {
		t.Error("a failing command reported success")
	}
}

// Async results must reach their pane even when another tab is in front.
// Init() starts the doctor check while Docs is active, so routing by active
// tab dropped it and Doctor sat on "checking…" forever.
func TestAsyncResultReachesInactiveTab(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if tm.(model).tab != tabDocs {
		t.Fatal("expected Docs to be the starting tab")
	}
	tm, _ = tm.Update(runDoctorChecks())
	if strings.Contains(tm.(model).doc.view("⣾"), "checking") {
		t.Error("doctor still shows 'checking' after its results arrived")
	}
}

// Cancelling a running action must not wedge the overlay. The overlay owns
// the keyboard, so a stuck one takes the whole program with it.
func TestCancelDoesNotWedge(t *testing.T) {
	a := newAction(testCommandPlan("Sleep", "sleep", "30"), 80, 20)
	a, cmd := a.start()

	a, cmd, _ = a.update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("cancel returned no command — the done message can never arrive")
	}
	if !a.cancelled {
		t.Error("cancel did not mark the action cancelled")
	}

	done := make(chan actionModel, 1)
	go func() {
		aa, c := a, cmd
		for !aa.done && c != nil {
			aa, c, _ = aa.update(c())
		}
		done <- aa
	}()
	select {
	case aa := <-done:
		if !aa.done {
			t.Error("overlay never reached a finished state after cancelling")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("overlay wedged after cancel — this is the freeze")
	}
}

// A second action must not replace a running one, orphaning its process.
func TestOneActionAtATime(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.Update(runActionMsg{plan: testCommandPlan("First", "sleep", "5")})
	first := tm.(model).act
	if first == nil {
		t.Fatal("first action did not open")
	}
	tm, _ = tm.Update(runActionMsg{plan: testCommandPlan("Second", "echo", "hi")})
	if tm.(model).act.plan.Title != "First" {
		t.Error("a second action replaced the running one")
	}

	// Do not leave the package-global Runner occupied for whichever test runs
	// next. The production Program would keep consuming the action stream; this
	// unit test deliberately bypasses that event loop after the assertion.
	if first.cancel != nil {
		first.cancel()
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg, ok := <-first.ch:
			if !ok {
				t.Fatal("first action stream closed without a result")
			}
			if _, ok := msg.(actDoneMsg); ok {
				return
			}
		case <-deadline:
			t.Fatal("first action did not stop during test cleanup")
		}
	}
}

// Every tab must fit the terminal height. A pane one line too tall pushes the
// tab bar off the top of the screen, which is exactly what the Docs tab did.
func TestTabsFitHeight(t *testing.T) {
	for _, sz := range [][2]int{{120, 40}, {200, 50}, {100, 30}, {80, 24}, {60, 18}, {70, 20}} {
		w, h := sz[0], sz[1]
		m := newModel()
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: h})
		for tab := 0; tab < int(numTabs); tab++ {
			tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune('1' + tab)}})
			got := len(strings.Split(tm.View(), "\n"))
			if got > h {
				t.Errorf("tab %d at %dx%d renders %d lines, %d too many",
					tab, w, h, got, got-h)
			}
		}
	}
}

// A key pressed in one tab must not reach the others.
func TestKeysDoNotLeakAcrossTabs(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 120, Height: 34})

	// Docs advertises d/u for scrolling. Manage binds u to "update dotfiles".
	if tm.(model).tab != tabDocs {
		t.Fatal("expected Docs")
	}
	var cmd tea.Cmd
	tm, cmd = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	// Bubble Tea runs whatever a pane returns; not running it here is what
	// made the first version of this test pass against a real leak.
	if cmd != nil {
		if msg := cmd(); msg != nil {
			tm, _ = tm.Update(msg)
		}
	}
	if tm.(model).act != nil {
		t.Errorf("pressing 'u' in Docs started an action: %q",
			tm.(model).act.plan.Title)
	}

	// And 'i' in Docs must not trigger Doctor's installer.
	tm, _ = tm.Update(runDoctorChecks())
	tm, cmd = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			tm, _ = tm.Update(msg)
		}
	}
	if tm.(model).act != nil {
		t.Errorf("pressing 'i' in Docs started an action: %q",
			tm.(model).act.plan.Title)
	}
}
