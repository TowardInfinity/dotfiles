package main

import "testing"

// ── brew ──────────────────────────────────────────────────────

func TestParseBrewList(t *testing.T) {
	out := `git 2.43.0
node 21.6.0 20.11.0
jq 1.7.1
`
	pkgs := parseBrewList(out)
	if len(pkgs) != 3 {
		t.Fatalf("got %d packages, want 3", len(pkgs))
	}
	// A formula with two installed versions (leftover after an upgrade) keeps
	// only the first — the one `brew outdated` also treats as current.
	if pkgs[1].Name != "node" || pkgs[1].Version != "21.6.0" {
		t.Errorf("node = %+v, want name=node version=21.6.0", pkgs[1])
	}
	for _, p := range pkgs {
		if p.Manager != pmBrew {
			t.Errorf("%s: manager = %v, want pmBrew", p.Name, p.Manager)
		}
	}
}

func TestParseBrewOutdated(t *testing.T) {
	out := `{"formulae":[{"name":"jq","current_version":"1.7.2"},{"name":"git","current_version":"2.44.0"}],"casks":[]}`
	latest := parseBrewOutdated(out)
	if latest["jq"] != "1.7.2" {
		t.Errorf("jq latest = %q, want 1.7.2", latest["jq"])
	}
	if latest["git"] != "2.44.0" {
		t.Errorf("git latest = %q, want 2.44.0", latest["git"])
	}
	if len(latest) != 2 {
		t.Errorf("got %d entries, want 2", len(latest))
	}
}

func TestParseBrewOutdatedGarbage(t *testing.T) {
	if latest := parseBrewOutdated("not json"); latest != nil {
		t.Errorf("garbage input = %v, want nil", latest)
	}
}

// ── pnpm ──────────────────────────────────────────────────────

func TestParsePnpmGlobalList(t *testing.T) {
	out := `[{"dependencies":{"@openai/codex":{"version":"0.63.0"},"typescript":{"version":"5.4.0"}}}]`
	pkgs := parsePnpmGlobalList(out)
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages, want 2", len(pkgs))
	}
	// sorted by name
	if pkgs[0].Name != "@openai/codex" || pkgs[1].Name != "typescript" {
		t.Errorf("not sorted by name: %+v", pkgs)
	}
	for _, p := range pkgs {
		if p.Manager != pmPnpm {
			t.Errorf("%s: manager = %v, want pmPnpm", p.Name, p.Manager)
		}
	}
}

func TestParsePnpmGlobalListEmpty(t *testing.T) {
	out := `[{"dependencies":{}}]`
	if pkgs := parsePnpmGlobalList(out); len(pkgs) != 0 {
		t.Errorf("empty dependencies = %v, want none", pkgs)
	}
}

// ── npm ───────────────────────────────────────────────────────

func TestParseNpmGlobalList(t *testing.T) {
	out := `{"name":"lib","dependencies":{"npm":{"version":"11.17.0"},"openclaw":{"version":"2026.6.11"},"@openai/codex":{"version":"0.63.0"}}}`
	pkgs := parseNpmGlobalList(out)
	// npm lists itself as a dependency — that entry must be filtered out.
	for _, p := range pkgs {
		if p.Name == "npm" {
			t.Errorf("npm's self-entry was not filtered out: %+v", pkgs)
		}
	}
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages, want 2 (npm self-entry excluded)", len(pkgs))
	}
	if pkgs[0].Name != "@openai/codex" || pkgs[1].Name != "openclaw" {
		t.Errorf("not sorted by name: %+v", pkgs)
	}
}

func TestParseNpmStyleOutdated(t *testing.T) {
	// Shared by both `npm outdated --json` and `pnpm outdated --format json` —
	// same {"<name>":{"current":...,"wanted":...,"latest":...}} shape.
	out := `{"@openai/codex":{"current":"0.63.0","wanted":"0.147.0","latest":"0.147.0"},"openclaw":{"current":"2026.6.11","wanted":"2026.7.1-2","latest":"2026.7.1-2"}}`
	latest := parseNpmStyleOutdated(out)
	if latest["@openai/codex"] != "0.147.0" {
		t.Errorf("codex latest = %q, want 0.147.0", latest["@openai/codex"])
	}
	if latest["openclaw"] != "2026.7.1-2" {
		t.Errorf("openclaw latest = %q, want 2026.7.1-2", latest["openclaw"])
	}
}

func TestParseNpmStyleOutdatedEmpty(t *testing.T) {
	// Nothing outdated: both npm and pnpm print "{}" (or empty stdout on some
	// versions), and that must not be treated as an error.
	if latest := parseNpmStyleOutdated("{}"); len(latest) != 0 {
		t.Errorf("got %v, want empty", latest)
	}
}

// ── uv tool ───────────────────────────────────────────────────

func TestParseUvToolList(t *testing.T) {
	out := `kaggle v2.2.4
- kaggle
jupyterlab v4.5.0
- jupyter
- jupyter-lab
`
	entries := parseUvToolList(out)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (exposed-command lines must be skipped)", len(entries))
	}
	if entries[0].Name != "kaggle" || entries[0].Version != "2.2.4" {
		t.Errorf("kaggle = %+v, want name=kaggle version=2.2.4", entries[0])
	}
	if entries[1].Name != "jupyterlab" || entries[1].Version != "4.5.0" {
		t.Errorf("jupyterlab = %+v, want name=jupyterlab version=4.5.0", entries[1])
	}
}

func TestParseUvToolListEmpty(t *testing.T) {
	if entries := parseUvToolList(""); len(entries) != 0 {
		t.Errorf("empty input = %v, want none", entries)
	}
}

// ── pip ───────────────────────────────────────────────────────

func TestParsePipList(t *testing.T) {
	out := `[{"name":"requests","version":"2.31.0"},{"name":"pip","version":"24.0"}]`
	pkgs := parsePipList(out)
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages, want 2", len(pkgs))
	}
	for _, p := range pkgs {
		if p.Manager != pmPip {
			t.Errorf("%s: manager = %v, want pmPip", p.Name, p.Manager)
		}
	}
}

func TestParsePipOutdated(t *testing.T) {
	out := `[{"name":"requests","version":"2.31.0","latest_version":"2.32.0","latest_filetype":"wheel"}]`
	latest := parsePipOutdated(out)
	if latest["requests"] != "2.32.0" {
		t.Errorf("requests latest = %q, want 2.32.0", latest["requests"])
	}
}

// ── go ────────────────────────────────────────────────────────

func TestParseGoVersionM(t *testing.T) {
	out := `/Users/x/go/bin/dlv: go1.24.1
	path	github.com/go-delve/delve/cmd/dlv
	mod	github.com/go-delve/delve	v1.24.1	h1:abc123=
	dep	golang.org/x/arch	v0.7.0	h1:def456=
`
	p, ok := parseGoVersionM("dlv", out)
	if !ok {
		t.Fatal("expected a mod line to be found")
	}
	if p.Name != "dlv" {
		t.Errorf("name = %q, want dlv (named after the binary on disk, not the module)", p.Name)
	}
	if p.Version != "v1.24.1" {
		t.Errorf("version = %q, want v1.24.1", p.Version)
	}
	if p.Manager != pmGo {
		t.Errorf("manager = %v, want pmGo", p.Manager)
	}
}

func TestParseGoVersionMNoModLine(t *testing.T) {
	// Built without embedded module info (e.g. GOFLAGS=-mod=vendor edge
	// cases, or a binary that isn't a Go build at all).
	out := "/Users/x/go/bin/weird: go1.24.1\n\tpath\tcommand-line-arguments\n"
	if _, ok := parseGoVersionM("weird", out); ok {
		t.Error("expected no match without a mod line")
	}
}

// ── pkg.Outdated ──────────────────────────────────────────────

func TestPkgOutdated(t *testing.T) {
	cases := []struct {
		name     string
		p        pkg
		outdated bool
	}{
		{"blank latest is not outdated", pkg{Version: "1.0", Latest: ""}, false},
		{"equal versions are not outdated", pkg{Version: "1.0", Latest: "1.0"}, false},
		{"differing versions are outdated", pkg{Version: "1.0", Latest: "1.1"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.Outdated(); got != c.outdated {
				t.Errorf("Outdated() = %v, want %v", got, c.outdated)
			}
		})
	}
}

// ── pkgManager.String ────────────────────────────────────────

func TestPkgManagerString(t *testing.T) {
	cases := map[pkgManager]string{
		pmBrew:   "brew",
		pmPnpm:   "pnpm",
		pmNpm:    "npm",
		pmUvTool: "uv tool",
		pmPip:    "pip",
		pmGo:     "go",
	}
	for m, want := range cases {
		if got := m.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", m, got, want)
		}
	}
}

// ── computeAdvisories ─────────────────────────────────────────

// Only the zero-count branches are portable across machines: computeAdvisories
// shells out (via pnpmGlobalBinCheck/have) for the "is the preferred manager
// actually reachable" half of its logic, so a positive case depends on what's
// installed on whatever box runs `go test`. The negative case does not.
func TestComputeAdvisoriesNoNpmOrPipPackages(t *testing.T) {
	pkgs := []pkg{
		{Manager: pmBrew, Name: "git", Version: "1.0"},
		{Manager: pmPnpm, Name: "typescript", Version: "5.0"},
	}
	if adv := computeAdvisories(pkgs); len(adv) != 0 {
		t.Errorf("no npm/pip packages present, got advisories: %+v", adv)
	}
}

func TestComputeAdvisoriesEmptyInput(t *testing.T) {
	if adv := computeAdvisories(nil); len(adv) != 0 {
		t.Errorf("nil input, got advisories: %+v", adv)
	}
}

// ── packageAction ─────────────────────────────────────────────

func TestPackageActionGoHasNoAction(t *testing.T) {
	// `go install <module>@latest` re-fetches rather than upgrading in place —
	// a different action than every other row's "u", so none is offered.
	if _, ok := packageAction(pkg{Manager: pmGo, Name: "dlv", Version: "v1.0"}); ok {
		t.Error("pmGo got an action, want none")
	}
}

// ── sortPackagesFor / outdatedOverview ─────────────────────────

// Manager grouping is the outer order and must never move, regardless of
// sort mode — the redesign kept manager groups as label headers, not
// reorderable sections, so this is the one invariant both modes share.
func TestSortPackagesForKeepsManagerGrouping(t *testing.T) {
	for _, mode := range []pkgSortMode{pkgSortOutdated, pkgSortName} {
		pkgs := []pkg{
			{Manager: pmNpm, Name: "zeta", Version: "1.0"},
			{Manager: pmBrew, Name: "git", Version: "1.0"},
			{Manager: pmNpm, Name: "alpha", Version: "1.0"},
			{Manager: pmBrew, Name: "jq", Version: "1.0"},
		}
		sortPackagesFor(pkgs, mode)
		for i := 1; i < len(pkgs); i++ {
			if pkgs[i].Manager < pkgs[i-1].Manager {
				t.Fatalf("mode %v: manager grouping broken at %d: %+v", mode, i, pkgs)
			}
		}
	}
}

func TestSortPackagesForOutdatedFirst(t *testing.T) {
	pkgs := []pkg{
		{Manager: pmBrew, Name: "current-a", Version: "1.0"},
		{Manager: pmBrew, Name: "stale-b", Version: "1.0", Latest: "2.0"},
		{Manager: pmBrew, Name: "current-c", Version: "1.0"},
		{Manager: pmBrew, Name: "stale-a", Version: "1.0", Latest: "2.0"},
	}
	sortPackagesFor(pkgs, pkgSortOutdated)
	// Both outdated rows first (alphabetical among themselves), then the
	// non-outdated rows (also alphabetical).
	want := []string{"stale-a", "stale-b", "current-a", "current-c"}
	for i, name := range want {
		if pkgs[i].Name != name {
			t.Errorf("position %d = %q, want %q (%+v)", i, pkgs[i].Name, name, pkgs)
		}
	}
}

func TestSortPackagesForName(t *testing.T) {
	pkgs := []pkg{
		{Manager: pmBrew, Name: "zeta", Version: "1.0", Latest: "2.0"}, // outdated, but name mode ignores that
		{Manager: pmBrew, Name: "alpha", Version: "1.0"},
		{Manager: pmBrew, Name: "mid", Version: "1.0"},
	}
	sortPackagesFor(pkgs, pkgSortName)
	want := []string{"alpha", "mid", "zeta"}
	for i, name := range want {
		if pkgs[i].Name != name {
			t.Errorf("position %d = %q, want %q (%+v)", i, pkgs[i].Name, name, pkgs)
		}
	}
}

func TestOutdatedOverviewCapAndOverflow(t *testing.T) {
	pkgs := []pkg{
		{Manager: pmBrew, Name: "a", Version: "1.0", Latest: "2.0"},
		{Manager: pmBrew, Name: "b", Version: "1.0"}, // not outdated
		{Manager: pmBrew, Name: "c", Version: "1.0", Latest: "2.0"},
		{Manager: pmNpm, Name: "d", Version: "1.0", Latest: "2.0"},
		{Manager: pmNpm, Name: "e", Version: "1.0", Latest: "2.0"},
	}
	shown, more := outdatedOverview(pkgs, 3)
	if len(shown) != 3 {
		t.Fatalf("got %d shown, want 3 (cap)", len(shown))
	}
	if more != 1 {
		t.Errorf("more = %d, want 1 (4 outdated total, capped at 3)", more)
	}
	for _, p := range shown {
		if !p.Outdated() {
			t.Errorf("non-outdated package in overview: %+v", p)
		}
	}
}

func TestOutdatedOverviewEmpty(t *testing.T) {
	shown, more := outdatedOverview(nil, 5)
	if len(shown) != 0 || more != 0 {
		t.Errorf("nil input: shown=%v more=%d, want none", shown, more)
	}
	allCurrent := []pkg{{Manager: pmBrew, Name: "a", Version: "1.0"}}
	shown, more = outdatedOverview(allCurrent, 5)
	if len(shown) != 0 || more != 0 {
		t.Errorf("nothing outdated: shown=%v more=%d, want none", shown, more)
	}
}

func TestOutdatedOverviewUnderCap(t *testing.T) {
	pkgs := []pkg{{Manager: pmBrew, Name: "a", Version: "1.0", Latest: "2.0"}}
	shown, more := outdatedOverview(pkgs, 5)
	if len(shown) != 1 || more != 0 {
		t.Errorf("shown=%v more=%d, want 1 shown, 0 more", shown, more)
	}
}

func TestPackageActionKnownManagers(t *testing.T) {
	for _, m := range []pkgManager{pmBrew, pmPnpm, pmNpm, pmUvTool, pmPip} {
		p := pkg{Manager: m, Name: "thing", Version: "1.0", Latest: "1.1"}
		spec, ok := packageAction(p)
		if !ok {
			t.Errorf("%v: expected an action", m)
			continue
		}
		if len(spec.Argv) == 0 {
			t.Errorf("%v: empty Argv", m)
		}
		if spec.Confirm == "" {
			t.Errorf("%v: empty Confirm", m)
		}
	}
}
