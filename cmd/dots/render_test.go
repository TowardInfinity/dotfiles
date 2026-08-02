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

	tm, _ = tm.Update(runActionMsg{spec: actionSpec{
		Title:   "Probe",
		Argv:    []string{"echo", "hello"},
		Confirm: "Run the probe?",
	}})
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
	a := newAction(actionSpec{Title: "Probe", Argv: []string{"echo", "marker-line"}}, 80, 20)
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
	a := newAction(actionSpec{Title: "Fail", Argv: []string{"sh", "-c", "exit 3"}}, 80, 20)
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
