package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// All four arrows have to do something wherever something can move, and none of
// them may quietly change meaning depending on where you are. The awkward case
// is Manage: its rail is drawn vertically, so up/down reads as the way to move
// it, but three of its five sections put a list in the body that up/down must
// drive instead. These tests pin that split down, because it is exactly the
// kind of rule that a later "just make it uniform" edit would flatten.

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

// Up and down move the rail only where the body has no list to claim them.
func TestManageUpDownMovesRailOnlyWithoutAList(t *testing.T) {
	for s := manageSection(0); s < numSections; s++ {
		railed := s == secOverview || s == secDotfiles

		tm := manageAt(t, s)
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyDown})
		got := tm.(model).man.section
		want := s
		if railed {
			want = (s + 1) % numSections
		}
		if got != want {
			t.Errorf("down from %v landed on %v, want %v (railed=%v)", s, got, want, railed)
		}

		tm = manageAt(t, s)
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyUp})
		got = tm.(model).man.section
		want = s
		if railed {
			want = (s + numSections - 1) % numSections
		}
		if got != want {
			t.Errorf("up from %v landed on %v, want %v (railed=%v)", s, got, want, railed)
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
