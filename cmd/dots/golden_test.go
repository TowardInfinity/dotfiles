package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var updateGoldens = flag.Bool("update-goldens", false, "rewrite canonical shell golden fixtures")

func canonicalShell(width, height int, route routeID) *shellModel {
	m := newShellModel()
	m.repo = "/workspace/dotfiles"
	m.route = route
	m.focus = shellFocusContent
	m.loaded = map[routeID]bool{route: true}
	m.ovLoading = false
	m.ovInfo = overviewInfo{host: "workstation", arch: "arm64", osName: "macOS 15.6", version: "v0.1.16", toolsHave: 8, toolsTotal: 8}
	m.sync = repoState{ok: true}
	m.fleet = fleetSnapshot{Schema: fleetCacheSchema, Hosts: []fleetSnapshotHost{
		{Alias: "a1", Outcome: "ok", ConfigOK: true, Version: "v0.1.16", Revision: "1111111111111111111111111111111111111111"},
		{Alias: "v1", Outcome: "ok", ConfigOK: true, Version: "v0.1.16", Revision: "2222222222222222222222222222222222222222"},
		{Alias: "v2", Outcome: "unreachable", Version: "—"},
	}}
	m.fleetSelected = map[string]bool{"a1": true}
	m.changes = changesModel{
		repo:     m.repo,
		branch:   "main",
		files:    []changeFile{{Path: "common/claude/settings.json", Status: " M"}, {Path: "docs/dots-panes.md", Status: "??"}},
		incoming: []changeCommit{{Hash: "abc1234", Subject: "refresh navigation guide"}},
		selected: map[string]bool{"common/claude/settings.json": true},
	}
	m.servicesView = newServicesRouteModel()
	m.servicesView.loading = false
	m.packagesView = newPackagesRouteModel()
	m.packagesView.loading = false
	m.projects = []projectInfo{{name: "dotfiles", path: m.repo, branch: "main", dirtyKnown: true}}
	m.projectsLoading = false
	m.docs = newDocsModel([]doc{{
		Name: "compact", Title: "Compact Docs", Group: "Reference", Order: 1,
		Summary: "A small canonical Markdown page.",
		Body:    "# Compact Docs\n\nA short paragraph with **strong text**, a [link](https://example.com), and a table.\n\n| key | value |\n| --- | --- |\n| mode | compact |\n\n```sh\ndots status\n```\n",
	}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(*shellModel)
}

func assertCanonicalGolden(t *testing.T, name, got string) {
	t.Helper()
	got = canonicalGoldenText(stripANSI(got))
	path := filepath.Join("testdata", "goldens", name+".txt")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run go test ./cmd/dots -run TestCanonicalGoldens -update-goldens)", path, err)
	}
	if canonicalGoldenText(string(want)) != got {
		wantText := canonicalGoldenText(string(want))
		wantLines, gotLines := strings.Split(wantText, "\n"), strings.Split(got, "\n")
		for i := 0; i < len(wantLines) && i < len(gotLines); i++ {
			if wantLines[i] != gotLines[i] {
				t.Fatalf("%s golden mismatch at line %d: want %q, got %q", name, i+1, wantLines[i], gotLines[i])
			}
		}
		t.Fatalf("%s golden mismatch (%d want lines, %d got); run with -update-goldens only after reviewing the layout", name, len(wantLines), len(gotLines))
	}
}

func canonicalGoldenText(s string) string {
	// Remove only the terminal newline. Blank rows inside the fixed-size
	// terminal frame are meaningful and must not collapse during normalization.
	s = strings.TrimSuffix(s, "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

func TestCanonicalGoldens(t *testing.T) {
	t.Run("wide-fleet", func(t *testing.T) {
		m := canonicalShell(160, 45, routeFleet)
		assertCanonicalGolden(t, "wide-fleet", m.View().Content)
	})

	t.Run("standard-changes-dialog", func(t *testing.T) {
		m := canonicalShell(100, 30, routeChanges)
		plan := testCommandPlan("Publish selected changes", "dots", "publish", "common/claude/settings.json")
		plan.Summary = "Validate and publish the reviewed selection"
		plan.Target = "/workspace/dotfiles"
		plan.Confirm = "Publish the selected changes to the canonical repository?"
		updated, _ := m.Update(runActionMsg{plan: plan})
		m = updated.(*shellModel)
		assertCanonicalGolden(t, "standard-changes-dialog", m.View().Content)
	})

	t.Run("compact-docs", func(t *testing.T) {
		m := canonicalShell(60, 18, routeDocs)
		assertCanonicalGolden(t, "compact-docs", m.View().Content)
	})
}
