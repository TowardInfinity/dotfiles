package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestServicesRouteOwnsFilteringAndInspection(t *testing.T) {
	m := newServicesRouteModel()
	var cmd tea.Cmd
	m, cmd = m.update(servicesFoundMsg{services: []service{
		{Name: "api", ID: "api", Source: srcLaunchd, Running: true, Detail: "pid 12"},
		{Name: "worker", ID: "worker", Source: srcLaunchd, Running: false, Detail: "stopped"},
	}})
	if cmd == nil {
		t.Fatal("service discovery did not schedule health probing")
	}
	m, _ = m.updateKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	m, _ = m.updateKey(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !m.filtering || m.filter != "a" {
		t.Fatalf("service filter = filtering:%v value:%q, want active a", m.filtering, m.filter)
	}
	m, _ = m.updateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.filtering {
		t.Fatal("service filter did not close on Enter")
	}
	m, _ = m.updateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(m.detail, "api") {
		t.Fatalf("service detail = %q, want api", m.detail)
	}
	if got := m.summary(); !strings.Contains(got, "1 of 2 running") {
		t.Fatalf("service summary = %q", got)
	}
}

func TestServicesRunningFilterUsesFNotGlobalActionsKey(t *testing.T) {
	m := newServicesRouteModel()
	m, _ = m.update(servicesFoundMsg{services: []service{
		{Name: "api", Source: srcLaunchd, Running: true},
		{Name: "worker", Source: srcLaunchd, Running: false},
	}})
	m, _ = m.updateKey(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if !m.runningOnly || len(m.visible()) != 1 || m.visible()[0].Name != "api" {
		t.Fatalf("running filter = enabled:%v rows:%v, want only api", m.runningOnly, m.visible())
	}
}

func TestPackagesRouteOwnsSortManagerFilterAndUpgrade(t *testing.T) {
	m := newPackagesRouteModel()
	m, _ = m.update(packagesFoundMsg{packages: []pkg{
		{Manager: pmBrew, Name: "jq", Version: "1", Latest: "2"},
		{Manager: pmNpm, Name: "eslint", Version: "8", Latest: "8"},
	}, sources: []string{"brew", "npm"}})
	m, _ = m.updateKey(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if m.manager != pmPnpm {
		t.Fatalf("manager filter = %v, want pnpm", m.manager)
	}
	// Cycle back to All, then inspect the first package and toggle sort.
	for m.manager != pkgManagerAll {
		m, _ = m.updateKey(tea.KeyPressMsg{Code: 'm', Text: "m"})
	}
	m.cursor = 1 // manager grouping puts npm before brew
	m, _ = m.updateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(m.detail, "jq") {
		t.Fatalf("package detail = %q, want jq", m.detail)
	}
	m, _ = m.updateKey(tea.KeyPressMsg{Code: 's', Text: "s"})
	if m.sortMode != pkgSortName {
		t.Fatalf("sort mode = %v, want name", m.sortMode)
	}
	if got := m.summary(); !strings.Contains(got, "2 packages, 1 outdated") {
		t.Fatalf("package summary = %q", got)
	}
}

func TestNativeCollectionRoutesRenderWithinBounds(t *testing.T) {
	services := newServicesRouteModel()
	services, _ = services.update(servicesFoundMsg{services: []service{{Name: "api", Source: srcLaunchd, Running: true}}})
	packages := newPackagesRouteModel()
	packages, _ = packages.update(packagesFoundMsg{packages: []pkg{{Manager: pmBrew, Name: "jq", Version: "1", Latest: "2"}}})
	for _, size := range [][2]int{{60, 18}, {100, 30}, {160, 45}} {
		for name, body := range map[string]string{
			"services": services.view(size[0], size[1], "·"),
			"packages": packages.view(size[0], size[1], "·"),
		} {
			for i, line := range strings.Split(body, "\n") {
				if lipgloss.Width(line) > measureFor(size[0]) {
					t.Errorf("%s at %dx%d line %d exceeds content width", name, size[0], size[1], i)
				}
			}
		}
	}
}

func TestServicesSelectedRowHasVisibleCursor(t *testing.T) {
	m := newServicesRouteModel()
	m, _ = m.update(servicesFoundMsg{services: []service{{Name: "api", Source: srcLaunchd, Running: true}}})
	plain := stripANSI(m.view(80, 24, "·"))
	if !strings.Contains(plain, "▌") {
		t.Fatal("selected service row has no visible cursor stripe")
	}
}

func TestSelectableLineKeepsColumnsAligned(t *testing.T) {
	plain := stripANSI(selectableLine("● api          launchd  running", 40, false))
	selected := stripANSI(selectableLine("● api          launchd  running", 40, true))
	plainAPI := strings.Index(plain, "api")
	selectedAPI := strings.Index(selected, "api")
	if plainAPI < 0 || selectedAPI < 0 {
		t.Fatal("test row lost its api label")
	}
	if lipgloss.Width(plain[:plainAPI]) != lipgloss.Width(selected[:selectedAPI]) {
		t.Fatalf("content shifted: unselected api at column %d, selected at %d", lipgloss.Width(plain[:plainAPI]), lipgloss.Width(selected[:selectedAPI]))
	}
}
