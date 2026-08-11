package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func shellAt(t *testing.T, width, height int) *shellModel {
	t.Helper()
	m := newShellModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(*shellModel)
}

func assertShellFits(t *testing.T, m *shellModel, width, height int) {
	t.Helper()
	v := m.View()
	if strings.TrimSpace(v.Content) == "" {
		t.Fatal("shell rendered an empty view")
	}
	lines := strings.Split(v.Content, "\n")
	if len(lines) > height+1 { // a terminal renderer may retain one final newline
		t.Fatalf("shell rendered %d lines at %dx%d", len(lines), width, height)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got > width {
			t.Errorf("line %d overflows at %dx%d: %d > %d", i, width, height, got, width)
		}
	}
}

func TestShellRoutesUseOneNavigationModel(t *testing.T) {
	m := shellAt(t, 100, 30)
	if m.route != routeOverview || m.focus != shellFocusSidebar {
		t.Fatalf("initial shell state = %s/%d, want overview/sidebar", m.route, m.focus)
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(*shellModel)
	if m.route != routeChanges {
		t.Fatalf("down selected %s, want changes", m.route)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = updated.(*shellModel)
	if m.focus != shellFocusContent {
		t.Fatal("right did not move focus to the content region")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = updated.(*shellModel)
	if m.focus != shellFocusSidebar {
		t.Fatal("left did not move focus back to the sidebar")
	}
}

func TestShellPaletteNavigatesWithoutExecuting(t *testing.T) {
	m := shellAt(t, 100, 30)
	updated, _ := m.Update(tea.KeyPressMsg{Code: ':', Text: ":"})
	m = updated.(*shellModel)
	if m.palette == nil {
		t.Fatal("palette did not open")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = updated.(*shellModel)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*shellModel)
	if m.palette != nil {
		t.Fatal("palette stayed open after selection")
	}
	if m.route != routeDocs {
		t.Fatalf("palette query selected %s, want docs", m.route)
	}
}

func TestShellResponsiveGeometry(t *testing.T) {
	for _, size := range [][2]int{{44, 14}, {60, 18}, {76, 22}, {100, 30}, {160, 45}} {
		m := shellAt(t, size[0], size[1])
		assertShellFits(t, m, size[0], size[1])
	}
	tooSmall := shellAt(t, 40, 12)
	if !strings.Contains(tooSmall.View().Content, "Terminal too small") {
		t.Fatal("too-small shell did not render the recovery message")
	}
}

func TestShellMouseHitMapNavigatesRoutes(t *testing.T) {
	m := shellAt(t, 100, 30)
	_ = m.View() // hit rectangles are built during rendering
	if len(m.hits) < len(shellNavRows()) {
		t.Fatalf("hit map has %d entries, want at least %d", len(m.hits), len(shellNavRows()))
	}
	// Overview is the first row after its group heading, so y=2 in the shell.
	updated, _ := m.Update(tea.MouseClickMsg{X: 4, Y: 4, Button: tea.MouseLeft})
	m = updated.(*shellModel)
	if m.route != routeChanges {
		t.Fatalf("mouse click selected %s, want changes", m.route)
	}
	if m.focus != shellFocusContent {
		t.Fatal("mouse route click did not focus content")
	}
}
