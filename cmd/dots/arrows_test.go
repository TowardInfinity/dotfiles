package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// All four arrows have to do something wherever something can move, and none
// of them may quietly change meaning depending on where you are.
//
// This file used to pin the opposite of that: up/down switched the rail in
// Overview/Dotfiles (the two sections with no row list) and moved a cursor
// everywhere else, on the theory that giving up/down *some* job in every
// section beat leaving it dead in two of them. In practice that meant the
// same keys meant two different things depending on which section you were
// on, and a user walking the rail with j/k hit the seam the moment they
// landed on Services — reported directly as "when i reach services it fall
// to other inner." Up/down now never switches sections, full stop; see the
// navigation contract above manageModel.keys() in keys.go. Overview and
// Dotfiles get their up/down back as a scroll offset instead (bodyRows/
// scrollWindow in manage.go), so nothing is left dead — it just no longer
// means something else depending on where you are.

func manageAt(t *testing.T, s manageSection) tea.Model {
	t.Helper()
	var tm tea.Model = newModel()
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})

	// Walk to the wanted section with right, which works in every section.
	for i := 0; i < int(numSections); i++ {
		if tm.(model).man.section == s {
			return tm
		}
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRight})
	}
	t.Fatalf("never reached section %v", s)
	return nil
}

// Left and right move the rail from every section, list or not.
func TestManageLeftRightAlwaysMovesRail(t *testing.T) {
	for s := manageSection(0); s < numSections; s++ {
		tm := manageAt(t, s)

		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRight})
		if got, want := tm.(model).man.section, (s+1)%numSections; got != want {
			t.Errorf("right from %v landed on %v, want %v", s, got, want)
		}

		tm = manageAt(t, s)
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyLeft})
		if got, want := tm.(model).man.section, (s+numSections-1)%numSections; got != want {
			t.Errorf("left from %v landed on %v, want %v", s, got, want)
		}
	}
}

// Up and down never move the rail, in any section — that's left/right's job
// alone now. Where a section has nothing to move (an empty list, content
// that already fits), up/down is inert rather than falling back to a second
// meaning.
func TestManageUpDownNeverMovesRail(t *testing.T) {
	for s := manageSection(0); s < numSections; s++ {
		tm := manageAt(t, s)
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyDown})
		if got := tm.(model).man.section; got != s {
			t.Errorf("down from %v landed on %v, want no change", s, got)
		}

		tm = manageAt(t, s)
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyUp})
		if got := tm.(model).man.section; got != s {
			t.Errorf("up from %v landed on %v, want no change", s, got)
		}
	}
}

// Docs has one axis and nothing that scrolls sideways, so all four arrows move
// the sidebar. Left/right were dead keys here before.
func TestDocsAllFourArrowsMoveTheSidebar(t *testing.T) {
	start := func() tea.Model {
		var tm tea.Model = newModel()
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
		return tm
	}

	base := start().(model).docs.cur

	for _, k := range []tea.KeyType{tea.KeyDown, tea.KeyRight} {
		tm := start()
		tm, _ = tm.Update(tea.KeyMsg{Type: k})
		if got := tm.(model).docs.cur; got <= base {
			t.Errorf("%v did not move the sidebar forward: %d -> %d", k, base, got)
		}
	}

	// Move off the first entry so back has somewhere to go.
	for _, k := range []tea.KeyType{tea.KeyUp, tea.KeyLeft} {
		tm := start()
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyDown})
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyDown})
		mid := tm.(model).docs.cur
		tm, _ = tm.Update(tea.KeyMsg{Type: k})
		if got := tm.(model).docs.cur; got >= mid {
			t.Errorf("%v did not move the sidebar back: %d -> %d", k, mid, got)
		}
	}
}
