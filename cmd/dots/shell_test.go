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

func TestShellEveryRouteFitsSupportedSizes(t *testing.T) {
	routes := []routeID{routeOverview, routeChanges, routeFleet, routeHealth, routeServices, routePackages, routeProjects, routeDocs}
	for _, size := range [][2]int{{60, 18}, {76, 22}, {100, 30}, {160, 45}} {
		for _, route := range routes {
			m := shellAt(t, size[0], size[1])
			m.route = route
			m.focus = shellFocusContent
			assertShellFits(t, m, size[0], size[1])
		}
	}
}

func TestShellMouseHitMapNavigatesRoutes(t *testing.T) {
	m := shellAt(t, 100, 30)
	_ = m.View() // hit rectangles are built during rendering
	if len(m.hits) < len(shellNavRows()) {
		t.Fatalf("hit map has %d entries, want at least %d", len(m.hits), len(shellNavRows()))
	}
	// Overview is the first row after its group heading, so y=2 in the shell.
	updated, _ := m.Update(tea.MouseClickMsg{X: 4, Y: 6, Button: tea.MouseLeft})
	m = updated.(*shellModel)
	if m.route != routeChanges {
		t.Fatalf("mouse click selected %s, want changes", m.route)
	}
	if m.focus != shellFocusContent {
		t.Fatal("mouse route click did not focus content")
	}
}

func TestShellHealthDefaultsToProblemsAndCanInspect(t *testing.T) {
	m := shellAt(t, 100, 30)
	m.route = routeHealth
	m.focus = shellFocusContent
	m.doc.loading = false
	m.doc.checks = []checkResult{
		{name: "git", state: checkOK, path: "/usr/bin/git"},
		{name: "pnpm", state: checkBad, path: "not found"},
		{name: "release", state: checkWarn, path: "offline"},
	}
	m.healthProblems = true
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*shellModel)
	if m.healthDetail == "" || !strings.Contains(m.healthDetail, "pnpm") {
		t.Fatalf("health inspection = %q, want pnpm detail", m.healthDetail)
	}
	assertShellFits(t, m, 100, 30)
}

func TestShellFleetSelectionUsesCacheRows(t *testing.T) {
	m := shellAt(t, 100, 30)
	m.route = routeFleet
	m.focus = shellFocusContent
	m.fleet.Hosts = []fleetSnapshotHost{{Alias: "a1", Outcome: "ok", ConfigOK: true, Version: "v0.1.15"}, {Alias: "v1", Outcome: "unreachable"}}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = updated.(*shellModel)
	if !m.fleetSelected["a1"] {
		t.Fatal("space did not select the focused fleet host")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(*shellModel)
	if m.fleetCursor != 1 {
		t.Fatalf("fleet cursor = %d, want 1", m.fleetCursor)
	}
	assertShellFits(t, m, 100, 30)
}

func TestShellMouseSelectsRows(t *testing.T) {
	m := shellAt(t, 100, 30)
	m.route = routeFleet
	m.focus = shellFocusContent
	m.fleet.Hosts = []fleetSnapshotHost{{Alias: "a1", Outcome: "ok", ConfigOK: true}}
	_ = m.View()
	updated, _ := m.Update(tea.MouseClickMsg{X: shellSidebarWidth + 5, Y: shellBodyTop + 3 + 1, Button: tea.MouseLeft})
	m = updated.(*shellModel)
	if m.fleetCursor != 0 || m.focus != shellFocusContent {
		t.Fatalf("fleet row click did not focus row: cursor=%d focus=%d", m.fleetCursor, m.focus)
	}
}

func TestShellHonorsNoColor(t *testing.T) {
	m := shellAt(t, 100, 30)
	t.Setenv("NO_COLOR", "1")
	if strings.Contains(m.View().Content, "\x1b[") {
		t.Fatal("NO_COLOR view still contains ANSI styling")
	}
}
