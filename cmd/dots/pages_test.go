package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// sizes worth caring about: a wide monitor, a normal split, a narrow SSH
// window, and the smallest thing anyone would plausibly use.
var testSizes = [][2]int{{200, 50}, {140, 40}, {118, 30}, {96, 28}, {80, 24}, {64, 20}}

func checkFrame(t *testing.T, label string, v string, w, h int) {
	t.Helper()
	if strings.TrimSpace(v) == "" {
		t.Errorf("%s: rendered empty", label)
		return
	}
	lines := strings.Split(v, "\n")
	if len(lines) > h {
		t.Errorf("%s: %d lines at height %d", label, len(lines), h)
	}
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got > w {
			t.Errorf("%s: line %d width %d > %d", label, i, got, w)
			return
		}
	}
	// The tab bar must survive on every page; losing it was a real bug.
	if !strings.Contains(v, "Docs") || !strings.Contains(v, "Manage") {
		t.Errorf("%s: tab bar missing", label)
	}
}

// Every one of the 22 doc pages, at every size.
func TestEveryDocPageRenders(t *testing.T) {
	docs, _ := loadDocs(findRepo())
	if len(docs) < 20 {
		t.Fatalf("expected the full doc set, got %d", len(docs))
	}
	for _, sz := range testSizes {
		w, h := sz[0], sz[1]
		m := newModel()
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: h})

		for i := 0; i < len(docs); i++ {
			cur := tm.(model).docs.current()
			if cur == nil {
				t.Fatalf("%dx%d: no current page at index %d", w, h, i)
			}
			checkFrame(t, cur.Name+" @"+itoa(w)+"x"+itoa(h), tm.View().Content, w, h)
			tm, _ = tm.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
		}
	}
}

// Doctor in both states, and Manage's four sections, at every size.
func TestEveryPaneRenders(t *testing.T) {
	// These messages are layout fixtures, not integration probes. Running SSH,
	// service discovery, and a git walk for every breakpoint made this test
	// measure the machine rather than the renderer (and made an offline test
	// suite wait on connection timeouts). Keep enough rows to exercise the
	// crowded states while making the data deterministic.
	checks := make([]checkResult, 0, len(checkNames())+5)
	for _, name := range checkNames() {
		checks = append(checks, checkResult{name: name, state: checkOK, path: "/usr/local/bin/" + name})
	}
	checks = append(checks,
		checkResult{name: "codex config", state: checkOK, path: "~/.codex/config.toml"},
		checkResult{name: "codex mode", state: checkOK, path: "0600"},
		checkResult{name: "managed block", state: checkOK, path: "matches policy"},
		checkResult{name: "dots binary", state: checkOK, path: "v0.1.16"},
		checkResult{name: "release", state: checkOK, path: "v0.1.16 (current)"},
	)
	doctorResult := doctorMsg{results: checks}
	servicesResult := servicesFoundMsg{
		services: []service{
			{ID: "api", Name: "api", Source: srcLaunchd, Running: true, Pid: 101, Port: 8080, Probed: true, Healthy: true},
			{ID: "worker", Name: "worker", Source: srcLaunchd, Running: true, Pid: 102, Port: 8081, Probed: true},
			{ID: "old", Name: "old worker", Source: srcLaunchd, Detail: "exited (0)"},
		},
		sources: []string{"launchd"},
	}
	projectsResult := projectsInfoMsg{projects: []projectInfo{
		{name: "dotfiles", path: "/workspace/dotfiles", branch: "main", dirtyKnown: true, ahead: 2},
		{name: "terminal", path: "/workspace/terminal", branch: "main", dirtyKnown: true, dirty: true, ahead: 0},
	}}
	machinesResult := machinesInfoMsg{machines: []machineInfo{
		{alias: "a1", hostname: "a1.example", checked: true, reachable: true},
		{alias: "v1", hostname: "v1.example", checked: true, reachable: true},
		{alias: "v2", hostname: "v2.example", checked: true},
	}}
	dotfilesResult := dotfilesInfoMsg{info: dotfilesInfo{
		sha: "abcdef1", branch: "main", dirtyKnown: true, dirty: true, behind: 3,
	}}

	for _, sz := range testSizes {
		w, h := sz[0], sz[1]
		m := newModel()
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: h})

		// Doctor: loading, then loaded.
		tm, _ = tm.Update(tea.KeyPressMsg{Code: '2', Text: string('2')})
		checkFrame(t, "doctor-loading @"+itoa(w)+"x"+itoa(h), tm.View().Content, w, h)
		tm, _ = tm.Update(doctorResult)
		checkFrame(t, "doctor-loaded @"+itoa(w)+"x"+itoa(h), tm.View().Content, w, h)

		// Manage: loading, then every section with representative data.
		tm, _ = tm.Update(tea.KeyPressMsg{Code: '3', Text: string('3')})
		checkFrame(t, "manage-loading @"+itoa(w)+"x"+itoa(h), tm.View().Content, w, h)
		tm, _ = tm.Update(servicesResult)
		tm, _ = tm.Update(projectsResult)
		tm, _ = tm.Update(machinesResult)
		tm, _ = tm.Update(dotfilesResult)
		// Synthetic rather than a real discoverPackages() — that shells out to
		// brew's network-hitting `outdated` on every one of the six sizes this
		// loop runs at. Deliberately pins the worst case rather than whatever
		// happens to be installed: a long formula name, an advisory line, and
		// six outdated rows across two managers — enough to exceed the Outdated
		// overview block's cap (pkgOutdatedOverviewCap) so the "+N more"
		// overflow line and the advisory block are both live at once, at the
		// same width their combined row budget has to share with the table.
		tm, _ = tm.Update(packagesFoundMsg{
			packages: []pkg{
				{Manager: pmBrew, Name: "font-jetbrains-mono-nerd-font", Version: "3.4.0"},
				{Manager: pmBrew, Name: "the-unarchiver", Version: "4.3.8,146,1715865652", Latest: "4.4.0"},
				{Manager: pmBrew, Name: "git", Version: "2.43.0", Latest: "2.44.0"},
				{Manager: pmBrew, Name: "jq", Version: "1.7.1", Latest: "1.7.2"},
				{Manager: pmBrew, Name: "node", Version: "21.6.0", Latest: "21.7.0"},
				{Manager: pmNpm, Name: "@earendil-works/pi-coding-agent", Version: "0.84.1", Latest: "0.90.0"},
				{Manager: pmNpm, Name: "typescript", Version: "5.4.0", Latest: "5.5.0"},
				{Manager: pmGo, Name: "staticcheck", Version: "v0.6.1"},
			},
			advisories: []advisory{{Text: "3 package(s) on npm global — pnpm is set up and reachable here; `pnpm add -g <name>` moves them over."}},
			sources:    []string{"brew", "go", "npm"},
		})

		for _, name := range sectionNames {
			checkFrame(t, "manage-"+name+" @"+itoa(w)+"x"+itoa(h), tm.View().Content, w, h)
			tm, _ = tm.Update(tea.KeyPressMsg{Code: 'l', Text: string('l')})
		}
	}
}

// Filters, the action overlay, and the empty-result states.
func TestInteractiveStatesRender(t *testing.T) {
	servicesResult := servicesFoundMsg{
		services: []service{{ID: "api", Name: "api", Source: srcLaunchd, Running: true}},
		sources:  []string{"launchd"},
	}
	for _, sz := range testSizes {
		w, h := sz[0], sz[1]
		m := newModel()
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: h})
		tm, _ = tm.Update(servicesResult)

		// Docs filter: matching, then matching nothing.
		tm, _ = tm.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
		for _, r := range "tmux" {
			tm, _ = tm.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		checkFrame(t, "docs-filter-hit @"+itoa(w)+"x"+itoa(h), tm.View().Content, w, h)
		for _, r := range "zzzzz" {
			tm, _ = tm.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		checkFrame(t, "docs-filter-miss @"+itoa(w)+"x"+itoa(h), tm.View().Content, w, h)
		tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

		// Services filter.
		tm, _ = tm.Update(tea.KeyPressMsg{Code: '3', Text: string('3')})
		tm, _ = tm.Update(tea.KeyPressMsg{Code: 'l', Text: string('l')}) // Dotfiles
		tm, _ = tm.Update(tea.KeyPressMsg{Code: 'l', Text: string('l')}) // Services
		tm, _ = tm.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
		for _, r := range "zzz" {
			tm, _ = tm.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		checkFrame(t, "svc-filter-miss @"+itoa(w)+"x"+itoa(h), tm.View().Content, w, h)
		tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

		// Action overlay, awaiting confirmation.
		plan := testCommandPlan("Probe", "echo", "x")
		plan.Confirm = "Run the probe?"
		tm, _ = tm.Update(runActionMsg{plan: plan})
		checkFrame(t, "overlay-confirm @"+itoa(w)+"x"+itoa(h), tm.View().Content, w, h)
		tm, _ = tm.Update(tea.KeyPressMsg{Code: 'n', Text: string('n')})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// install and update must be COMMANDS, not doc pages. docs/install.md and
// docs/update.md both exist, and the collision was previously resolved the
// wrong way — `dots update` printed the page about updating rather than
// updating anything, which the bash version it replaced did do.
func TestVerbsAreCommandsNotPages(t *testing.T) {
	docs, _ := loadDocs(findRepo())
	byName := map[string]bool{}
	for _, d := range docs {
		byName[d.Name] = true
	}
	// The pages still exist — the point is that the verbs shadow them, and
	// `dots docs <name>` is the way back to them.
	for _, n := range []string{"install", "update"} {
		if !byName[n] {
			t.Errorf("docs/%s.md is gone; the collision this guards no longer exists", n)
		}
	}
	// printDoc must still find them, which is what `dots docs <name>` calls.
	for _, n := range []string{"install", "update"} {
		found := false
		for _, d := range docs {
			if d.Name == n {
				found = true
			}
		}
		if !found {
			t.Errorf("dots docs %s would not resolve", n)
		}
	}
}
