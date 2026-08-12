package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
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

func TestShellSlashFiltersFocusedCollection(t *testing.T) {
	m := shellAt(t, 100, 30)
	m.route = routeDocs
	m.focus = shellFocusContent
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(*shellModel)
	if !m.docs.filtering || m.palette != nil {
		t.Fatal("Docs slash opened a palette instead of its filter")
	}
	m.route = routeChanges
	m.docs.filtering = false
	updated, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(*shellModel)
	if !m.changes.filtering || m.palette != nil {
		t.Fatal("Changes slash opened a palette instead of its filter")
	}
}

func TestShellEscUnwindsDetailBeforeFocus(t *testing.T) {
	m := shellAt(t, 100, 30)
	m.route = routeFleet
	m.focus = shellFocusContent
	m.fleetDetail = "a1 · healthy"
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(*shellModel)
	if m.fleetDetail != "" || m.focus != shellFocusContent {
		t.Fatalf("Esc did not close detail first: detail=%q focus=%d", m.fleetDetail, m.focus)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(*shellModel)
	if m.focus != shellFocusSidebar {
		t.Fatal("second Esc did not return focus to the sidebar")
	}
}

func TestShellResponsiveGeometry(t *testing.T) {
	for _, size := range [][2]int{{44, 14}, {60, 18}, {76, 22}, {100, 30}, {120, 32}, {160, 45}} {
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
	for _, size := range [][2]int{{60, 18}, {76, 22}, {100, 30}, {120, 32}, {160, 45}} {
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

func TestShellKeyboardAndMouseSelectTheSameRoute(t *testing.T) {
	keyboard := shellAt(t, 100, 30)
	updated, _ := keyboard.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	keyboard = updated.(*shellModel)
	updated, _ = keyboard.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	keyboard = updated.(*shellModel)

	mouse := shellAt(t, 100, 30)
	_ = mouse.View()
	var changes shellHit
	for _, hit := range mouse.hits {
		if hit.kind == shellHitRoute && hit.route == routeChanges {
			changes = hit
			break
		}
	}
	if changes.h == 0 {
		t.Fatal("Changes route has no mouse hit target")
	}
	updated, _ = mouse.Update(tea.MouseClickMsg{X: changes.x + 1, Y: changes.y, Button: tea.MouseLeft})
	mouse = updated.(*shellModel)
	if keyboard.route != mouse.route || keyboard.focus != mouse.focus {
		t.Fatalf("keyboard selected %s/%d, mouse selected %s/%d", keyboard.route, keyboard.focus, mouse.route, mouse.focus)
	}
}

func TestSidebarWheelOnlyBrowsesRoutes(t *testing.T) {
	m := shellAt(t, 100, 30)
	for i := 0; i < 2; i++ {
		updated, cmd := m.Update(tea.MouseWheelMsg{X: 4, Y: 8, Button: tea.MouseWheelDown})
		m = updated.(*shellModel)
		if cmd != nil {
			t.Fatal("sidebar wheel started a route provider")
		}
	}
	if m.route != routeFleet || m.loaded[routeFleet] {
		t.Fatalf("sidebar wheel route=%s loaded=%v, want Fleet without probing", m.route, m.loaded[routeFleet])
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

func TestShellHealthClampsCursorAfterResultsShrink(t *testing.T) {
	m := shellAt(t, 100, 30)
	m.route = routeHealth
	m.focus = shellFocusContent
	m.healthProblems = true
	m.healthCursor = 7
	m.doc.checks = []checkResult{
		{name: "pnpm", state: checkBad, path: "not found"},
	}
	updated, _ := m.Update(doctorMsg{results: m.doc.checks})
	m = updated.(*shellModel)
	if m.healthCursor != 0 {
		t.Fatalf("health cursor = %d after shrinking to one row, want 0", m.healthCursor)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*shellModel)
	if !strings.Contains(m.healthDetail, "pnpm") {
		t.Fatalf("health detail = %q, want pnpm", m.healthDetail)
	}
}

func TestShellActionHitMatchesVisibleConfirmButton(t *testing.T) {
	m := shellAt(t, 100, 30)
	plan := testCommandPlan("Roll out", "sleep", "10")
	plan.Scope = ops.ScopeFleet
	plan.Risk = ops.RiskDisruptive
	plan.Confirm = "Roll out now?"
	updated, _ := m.Update(runActionMsg{plan: plan})
	m = updated.(*shellModel)
	_ = m.View()
	if m.act == nil || !m.act.confirm {
		t.Fatal("confirm overlay did not open")
	}
	var button shellHit
	for _, hit := range m.hits {
		if hit.kind == shellHitAction && hit.index == 0 {
			button = hit
			break
		}
	}
	if button.h == 0 {
		t.Fatal("confirm button was not registered")
	}
	frame := strings.Split(stripANSI(m.View().Content), "\n")
	visibleButtonY := -1
	for y, line := range frame {
		if strings.Contains(line, "y run") {
			visibleButtonY = y
			break
		}
	}
	if visibleButtonY < 0 {
		t.Fatal("visible confirm button was not rendered")
	}
	if button.y != visibleButtonY {
		t.Fatalf("button y=%d, want rendered button row %d", button.y, visibleButtonY)
	}
	// A click in the header/blank area must not run the action.
	updated, _ = m.Update(tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft})
	m = updated.(*shellModel)
	if m.act == nil || !m.act.confirm {
		t.Fatal("blank modal click changed confirmation state")
	}
	// Clicking the actual visible Run half starts it; cancel immediately so
	// the test never leaves a child process behind.
	updated, _ = m.Update(tea.MouseClickMsg{X: button.x + max(1, button.w/2), Y: button.y, Button: tea.MouseLeft})
	m = updated.(*shellModel)
	if m.act == nil || m.act.confirm || !m.act.running {
		t.Fatal("visible confirm button did not start the action")
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(*shellModel)
	if cmd == nil {
		t.Fatal("cancel after modal click returned no stream command")
	}
	for m.act != nil && cmd != nil {
		msg := cmd()
		updated, cmd = m.Update(msg)
		m = updated.(*shellModel)
	}
}

func TestShellReportsRouteErrors(t *testing.T) {
	m := shellAt(t, 100, 30)
	updated, _ := m.Update(actionPlanErrorMsg{err: fmt.Errorf("select a machine")})
	m = updated.(*shellModel)
	if !strings.Contains(stripANSI(m.View().Content), "select a machine") {
		t.Fatal("route error was not rendered in the shell footer")
	}
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

// Row hit rectangles are computed from a hand-counted offset while the rows
// themselves come from dataTable, so the two drift apart silently whenever the
// table's chrome changes. Assert against the rendered frame, not against the
// same arithmetic the production code uses, and do it at every breakpoint
// because column collapsing changes the body without changing the offset.
func TestShellFleetRowHitsMatchRenderedRows(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {100, 30}, {76, 24}, {60, 20}, {44, 14}} {
		width, height := size[0], size[1]
		m := shellAt(t, width, height)
		m.route = routeFleet
		m.focus = shellFocusContent
		m.fleet = fleetSnapshot{Schema: fleetCacheSchema, Hosts: []fleetSnapshotHost{
			{Alias: "a1", Outcome: "ok", ConfigOK: true, Version: "v1.2.3", Revision: "abcdef1234"},
			{Alias: "v1", Outcome: "unhealthy", Version: "v1.2.2", Revision: "bbcdef1234"},
			{Alias: "v2", Outcome: "unreachable"},
		}}
		rendered := strings.Split(stripANSI(m.View().Content), "\n")
		for i, host := range m.fleet.Hosts {
			var row shellHit
			for _, hit := range m.hits {
				if hit.kind == shellHitRow && hit.route == routeFleet && hit.index == i {
					row = hit
					break
				}
			}
			if row.h == 0 {
				t.Fatalf("%dx%d: host %s has no row hit target", width, height, host.Alias)
			}
			line := ""
			if row.y >= 0 && row.y < len(rendered) {
				line = rendered[row.y]
			}
			if !strings.Contains(line, host.Alias) {
				t.Errorf("%dx%d: row hit for %s points at %q", width, height, host.Alias, strings.TrimSpace(line))
			}
		}
	}
}

func TestShellMouseSelectsRows(t *testing.T) {
	m := shellAt(t, 100, 30)
	m.route = routeFleet
	m.focus = shellFocusContent
	m.fleet.Hosts = []fleetSnapshotHost{{Alias: "a1", Outcome: "ok", ConfigOK: true}}
	_ = m.View()
	var row shellHit
	for _, hit := range m.hits {
		if hit.kind == shellHitRow && hit.route == routeFleet && hit.index == 0 {
			row = hit
			break
		}
	}
	if row.h == 0 {
		t.Fatal("fleet row has no mouse hit target")
	}
	updated, _ := m.Update(tea.MouseClickMsg{X: row.x + 1, Y: row.y, Button: tea.MouseLeft})
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

func TestStripANSIRemovesOSCLinksButKeepsText(t *testing.T) {
	got := stripANSI("before\x1b]8;;https://example.com\aLink\x1b]8;;\aafter")
	if got != "beforeLinkafter" {
		t.Fatalf("OSC stripping = %q, want visible link text only", got)
	}
}

func TestShellASCIICompatibilityMode(t *testing.T) {
	m := shellAt(t, 100, 30)
	t.Setenv("DOTS_ASCII", "1")
	view := m.View().Content
	for _, glyph := range []string{"▌", "●", "○", "✓", "×", "─"} {
		if strings.Contains(view, glyph) {
			t.Fatalf("ASCII view still contains %q", glyph)
		}
	}
}

func TestShellQUnwindsContentBeforeQuitting(t *testing.T) {
	m := shellAt(t, 100, 30)
	m.route = routeFleet
	m.focus = shellFocusContent
	m.fleetDetail = "a1 · healthy"
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = updated.(*shellModel)
	if cmd != nil || m.fleetDetail != "" || m.focus != shellFocusContent {
		t.Fatalf("q did not close detail without quitting: detail=%q focus=%d cmd=%v", m.fleetDetail, m.focus, cmd != nil)
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = updated.(*shellModel)
	if cmd != nil || m.focus != shellFocusSidebar {
		t.Fatalf("second q did not return to sidebar: focus=%d cmd=%v", m.focus, cmd != nil)
	}
}

func TestShellFooterClickOpensPalette(t *testing.T) {
	m := shellAt(t, 100, 30)
	_ = m.View()
	updated, _ := m.Update(tea.MouseClickMsg{X: 10, Y: 28, Button: tea.MouseLeft})
	m = updated.(*shellModel)
	if m.palette == nil {
		t.Fatal("clicking the visible footer did not open the action palette")
	}
}

func TestShellPaletteRowsAreClickable(t *testing.T) {
	m := shellAt(t, 100, 30)
	m.openPalette(false)
	_ = m.View()
	// Changes is the second route item; the palette is centered and each item
	// occupies one registered row in the hit map.
	var hit *shellHit
	for i := range m.hits {
		if m.hits[i].kind == shellHitPalette && m.hits[i].index == 1 {
			hit = &m.hits[i]
			break
		}
	}
	if hit == nil {
		t.Fatal("palette route row was not registered")
	}
	updated, _ := m.Update(tea.MouseClickMsg{X: hit.x + 2, Y: hit.y, Button: tea.MouseLeft})
	m = updated.(*shellModel)
	if m.route != routeChanges || m.palette != nil {
		t.Fatalf("palette click route=%s palette=%v, want changes/closed", m.route, m.palette != nil)
	}
}

func TestShellActionModalMouseCancelIsSafe(t *testing.T) {
	m := shellAt(t, 100, 30)
	plan := testCommandPlan("Probe", "echo", "ok")
	plan.Confirm = "Run the probe?"
	updated, _ := m.Update(runActionMsg{plan: plan})
	m = updated.(*shellModel)
	_ = m.View()
	var cancel shellHit
	for _, hit := range m.hits {
		if hit.kind == shellHitAction && hit.index == 1 {
			cancel = hit
			break
		}
	}
	if cancel.h == 0 {
		t.Fatal("cancel button was not registered")
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: cancel.x + max(1, cancel.w/2), Y: cancel.y, Button: tea.MouseLeft})
	m = updated.(*shellModel)
	if m.act != nil {
		t.Fatal("clicking the cancel half of an action modal did not close it")
	}
}
