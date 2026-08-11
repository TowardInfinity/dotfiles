package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Every rail row must occupy exactly one line.
//
// Neither the width nor the height test could catch a wrapped rail row: the
// frame clips it, so the total stays legal while the rail silently gains a
// blank highlighted line and loses a real one off the bottom. Count the rows
// instead.
func TestRailRowsDoNotWrap(t *testing.T) {
	for _, w := range []int{200, 160, 140, 132} {
		m := newModel()
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: 34})

		// Move onto a page with a long-ish title and long H2 headings.
		for i := 0; i < 6; i++ {
			tm, _ = tm.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
		}

		mm := tm.(model)
		nav := mm.docs.viewNav()
		for i, ln := range strings.Split(nav, "\n") {
			// -1: the rail's right border sits outside its Width.
			if got := lipgloss.Width(ln); got > navWidth+1 {
				t.Errorf("w=%d nav line %d is %d wide, rail is %d", w, i, got, navWidth)
				break
			}
		}

		if mm.docs.showOutline {
			out := mm.docs.viewOutline()
			for i, ln := range strings.Split(out, "\n") {
				if got := lipgloss.Width(ln); got > outlineWidth {
					t.Errorf("w=%d outline line %d is %d wide, rail is %d",
						w, i, got, outlineWidth)
					break
				}
			}
			// Markdown ticks are markup, not content, in a nav rail.
			if strings.Contains(out, "`") {
				t.Errorf("w=%d: outline still shows backticks", w)
			}
		}
	}
}

// The selected row must be exactly as many lines as an unselected one.
func TestSelectedRowIsOneLine(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 160, Height: 34})

	countRows := func() int {
		nav := tm.(model).docs.viewNav()
		n := 0
		for _, ln := range strings.Split(nav, "\n") {
			if strings.TrimSpace(ln) != "" {
				n++
			}
		}
		return n
	}
	before := countRows()
	for i := 0; i < 5; i++ {
		tm, _ = tm.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
		if got := countRows(); got != before {
			t.Fatalf("row count changed from %d to %d after moving the cursor —"+
				" the selected row is wrapping", before, got)
		}
	}
}
