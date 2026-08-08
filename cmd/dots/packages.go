package main

// Package inventory: what pnpm/npm/brew/uv/pip actually have installed
// globally, with versions and — where the manager can say so cheaply and
// offline — the latest available version. Companion to packagehealth.go's
// narrower doctor checks ("is the one thing bootstrap.sh declared still
// there"); this file answers the bigger question, "what's here at all",
// browsable in the Manage tab and one keypress from an upgrade.
//
// Same shape as services.go: backends run concurrently, contribute what they
// can see, and skip themselves silently when their manager isn't installed.
// No rendering here — that lives in manage.go's viewPackages.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ── data shapes ───────────────────────────────────────────────

type pkgManager int

// Declaration order is display order: sortPackagesFor and discoverPackages
// both group by raw Manager value, so this list is the one place that
// decides which manager's group renders first. Brew goes last on purpose —
// it typically outnumbers every other manager combined, so putting it first
// pushed the shorter, often more interesting groups below the fold.
//
// pkgManagerAll leads the block as a sentinel rather than a real manager —
// no pkg ever carries it, only manageModel.pkgMgrFilter does, to mean "no
// filter, show every group." Giving it value 0 means a manageModel's zero
// value already starts unfiltered with no explicit initializer, the same
// trick pkgSortMode's zero value already relies on. numPkgManagers closes
// the block so the manager-filter cycle key can wrap with `% numPkgManagers`
// instead of a hand-maintained count that drifts if a manager is added.
const (
	pkgManagerAll pkgManager = iota
	pmPnpm
	pmNpm
	pmUvTool
	pmPip
	pmGo
	pmInstaller // claude, opencode — see discoverInstallerCLIs
	pmBrew
	numPkgManagers
)

func (m pkgManager) String() string {
	switch m {
	case pkgManagerAll:
		return "all"
	case pmPnpm:
		return "pnpm"
	case pmNpm:
		return "npm"
	case pmUvTool:
		return "uv tool"
	case pmPip:
		return "pip"
	case pmGo:
		return "go"
	case pmInstaller:
		return "installer"
	case pmBrew:
		return "brew"
	}
	return "?"
}

// managerTitle is the capitalized form used in the Packages breadcrumb, to
// match sectionNames' style ("Overview", "Dotfiles", …) rather than
// String()'s all-lowercase form used everywhere else (search text, the
// summary line, upgrade confirmations). strings.Title is deprecated and
// this covers exactly seven known values, so a plain switch beats pulling in
// golang.org/x/text/cases for one label.
func (m pkgManager) managerTitle() string {
	switch m {
	case pmPnpm:
		return "Pnpm"
	case pmNpm:
		return "Npm"
	case pmUvTool:
		return "Uv Tool"
	case pmPip:
		return "Pip"
	case pmGo:
		return "Go"
	case pmInstaller:
		return "Installer"
	case pmBrew:
		return "Brew"
	}
	return "?"
}

type pkg struct {
	Manager pkgManager
	Name    string
	Version string
	Latest  string // "" = not checked, or not knowable for this manager
}

// Outdated reports whether an upgrade is known to be available. Blank Latest
// means "not knowable offline" (uv, go) or "the manager didn't mention it",
// not "up to date" — absence of evidence is not evidence of currency.
func (p pkg) Outdated() bool {
	return p.Latest != "" && p.Latest != p.Version
}

type advisory struct {
	Text string
}

type packagesFoundMsg struct {
	packages   []pkg
	advisories []advisory
	sources    []string
}

// pkgSortMode is the secondary sort key within a manager group — manager
// grouping itself is never reordered, only what comes within one. "Sort by
// used" was considered and dropped: no manager here tracks install/usage
// frequency, so there is nothing real to sort by.
type pkgSortMode int

const (
	pkgSortOutdated pkgSortMode = iota // outdated rows first, default
	pkgSortName                        // alphabetical
)

func (s pkgSortMode) String() string {
	switch s {
	case pkgSortOutdated:
		return "outdated"
	case pkgSortName:
		return "name"
	}
	return "?"
}

// sortPackagesFor sorts in place. Manager is always the primary key — the
// manager groups stay exactly where they already are, since the redesign
// keeps them as label headers rather than reorderable sections — and mode
// picks the secondary key within each group.
func sortPackagesFor(pkgs []pkg, mode pkgSortMode) {
	sort.SliceStable(pkgs, func(i, j int) bool {
		if pkgs[i].Manager != pkgs[j].Manager {
			return pkgs[i].Manager < pkgs[j].Manager
		}
		if mode == pkgSortOutdated && pkgs[i].Outdated() != pkgs[j].Outdated() {
			return pkgs[i].Outdated()
		}
		return pkgs[i].Name < pkgs[j].Name
	})
}

// outdatedOverview picks out what's outdated across every manager, for the
// summary block at the top of the Packages section — capped at max rows so
// a machine with dozens of stale packages doesn't push the manager-grouped
// table below off screen. Order follows the input slice's own order rather
// than re-sorting, so it agrees with whatever's currently shown beneath it.
func outdatedOverview(pkgs []pkg, max int) (shown []pkg, more int) {
	for _, p := range pkgs {
		if !p.Outdated() {
			continue
		}
		if len(shown) < max {
			shown = append(shown, p)
		} else {
			more++
		}
	}
	return shown, more
}

// ── discovery ─────────────────────────────────────────────────

func discoverPackages() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		var (
			mu       sync.Mutex
			packages []pkg
			sources  []string
			wg       sync.WaitGroup
		)

		contribute := func(pkgs []pkg, source string) {
			mu.Lock()
			defer mu.Unlock()
			packages = append(packages, pkgs...)
			sources = append(sources, source)
		}

		backends := []struct {
			name string
			run  func(context.Context) ([]pkg, bool)
		}{
			{"brew", discoverBrew},
			{"pnpm", discoverPnpmGlobal},
			{"npm", discoverNpmGlobal},
			{"uv", discoverUvTools},
			{"pip", discoverPip},
			{"go", discoverGoBin},
			{"installer", discoverInstallerCLIs},
		}
		for _, b := range backends {
			b := b
			wg.Add(1)
			go func() {
				defer wg.Done()
				if pkgs, ran := b.run(ctx); ran {
					contribute(pkgs, b.name)
				}
			}()
		}
		wg.Wait()

		sort.SliceStable(packages, func(i, j int) bool {
			if packages[i].Manager != packages[j].Manager {
				return packages[i].Manager < packages[j].Manager
			}
			return packages[i].Name < packages[j].Name
		})
		sort.Strings(sources)

		return packagesFoundMsg{
			packages:   packages,
			advisories: computeAdvisories(packages),
			sources:    sources,
		}
	}
}

// applyLatest fills in Latest for whatever names a manager's own "outdated"
// query mentioned. Anything not mentioned keeps Latest blank rather than
// being assumed current — the manager simply wasn't asked about it, or (for
// uv/go) has no such query at all.
func applyLatest(pkgs []pkg, latest map[string]string) {
	if len(latest) == 0 {
		return
	}
	for i := range pkgs {
		if v, ok := latest[pkgs[i].Name]; ok {
			pkgs[i].Latest = v
		}
	}
}

// ── backend: brew ──────────────────────────────────────────────

func discoverBrew(ctx context.Context) ([]pkg, bool) {
	p, ok := have("brew")
	if !ok {
		return nil, false
	}
	out, err := exec.CommandContext(ctx, p, "list", "--versions").Output()
	if err != nil {
		return nil, true // ran, has nothing usable to report
	}
	pkgs := parseBrewList(string(out))

	// `brew outdated` is read separately from `brew list` because it only
	// ever names formulas that ARE outdated — nothing here assumes silence
	// means current, applyLatest already treats it that way.
	outdated, _ := exec.CommandContext(ctx, p, "outdated", "--json=v2").Output()
	applyLatest(pkgs, parseBrewOutdated(string(outdated)))
	return pkgs, true
}

// parseBrewList parses `brew list --versions`: one line per formula,
// "<name> <version...>". A formula can list more than one installed version
// (e.g. after an upgrade that didn't clean up the old one) — only the first
// is used, which is also the one `brew outdated` treats as current.
func parseBrewList(output string) []pkg {
	var pkgs []pkg
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pkgs = append(pkgs, pkg{Manager: pmBrew, Name: fields[0], Version: fields[1]})
	}
	return pkgs
}

// parseBrewOutdated reads `brew outdated --json=v2`'s formulae array into a
// name -> current_version map ("current" meaning "what brew would install
// now", not what's on disk — the name Homebrew's own JSON uses).
func parseBrewOutdated(output string) map[string]string {
	var parsed struct {
		Formulae []struct {
			Name           string `json:"name"`
			CurrentVersion string `json:"current_version"`
		} `json:"formulae"`
	}
	if json.Unmarshal([]byte(output), &parsed) != nil {
		return nil
	}
	latest := map[string]string{}
	for _, f := range parsed.Formulae {
		latest[f.Name] = f.CurrentVersion
	}
	return latest
}

// ── backend: pnpm (global) ───────────────────────────────────────

func discoverPnpmGlobal(ctx context.Context) ([]pkg, bool) {
	p, ok := have("pnpm")
	if !ok {
		return nil, false
	}
	out, err := exec.CommandContext(ctx, p, "list", "-g", "--depth", "0", "--json").Output()
	if err != nil {
		// Usually the same PATH problem packagehealth.go's "pnpm global bin"
		// check already reports — nothing new to add here, just nothing to
		// list until that's fixed.
		return nil, true
	}
	pkgs := parsePnpmGlobalList(string(out))

	// `pnpm outdated`, like `npm outdated`, exits non-zero when it actually
	// finds something outdated — the interesting case, not the error case —
	// so its JSON is read regardless of the exit status.
	outdated, _ := exec.CommandContext(ctx, p, "outdated", "-g", "--format", "json").Output()
	applyLatest(pkgs, parseNpmStyleOutdated(string(outdated)))
	return pkgs, true
}

// parsePnpmGlobalList reads `pnpm list -g --depth 0 --json`: an array
// (pnpm's own "global project" wrapper) of objects each carrying a
// "dependencies" map of name -> {version}.
func parsePnpmGlobalList(output string) []pkg {
	var parsed []struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if json.Unmarshal([]byte(output), &parsed) != nil {
		return nil
	}
	var pkgs []pkg
	for _, project := range parsed {
		for name, d := range project.Dependencies {
			pkgs = append(pkgs, pkg{Manager: pmPnpm, Name: name, Version: d.Version})
		}
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
	return pkgs
}

// ── backend: npm (global) ────────────────────────────────────────

func discoverNpmGlobal(ctx context.Context) ([]pkg, bool) {
	p, ok := have("npm")
	if !ok {
		return nil, false
	}
	out, err := exec.CommandContext(ctx, p, "list", "-g", "--depth", "0", "--json").Output()
	if err != nil {
		return nil, true
	}
	pkgs := parseNpmGlobalList(string(out))

	outdated, _ := exec.CommandContext(ctx, p, "outdated", "-g", "--json").Output()
	applyLatest(pkgs, parseNpmStyleOutdated(string(outdated)))
	return pkgs, true
}

// parseNpmGlobalList reads `npm list -g --depth 0 --json`: a single object
// with a "dependencies" map of name -> {version}. npm always lists itself
// as one of its own global packages — that entry is dropped, since
// upgrading npm is a different, riskier action than upgrading something it
// merely manages, and offering it the same "u" as everything else would be
// misleading about what it does.
func parseNpmGlobalList(output string) []pkg {
	var parsed struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if json.Unmarshal([]byte(output), &parsed) != nil {
		return nil
	}
	var pkgs []pkg
	for name, d := range parsed.Dependencies {
		if name == "npm" {
			continue
		}
		pkgs = append(pkgs, pkg{Manager: pmNpm, Name: name, Version: d.Version})
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
	return pkgs
}

// parseNpmStyleOutdated parses the `{"<name>": {"current":..., "latest":...}}`
// shape both `npm outdated --json` and `pnpm outdated --format json` use —
// pnpm's JSON output was deliberately modeled on npm's, so one parser covers
// both call sites.
func parseNpmStyleOutdated(output string) map[string]string {
	var parsed map[string]struct {
		Latest string `json:"latest"`
	}
	if json.Unmarshal([]byte(output), &parsed) != nil {
		return nil
	}
	latest := map[string]string{}
	for name, e := range parsed {
		if e.Latest != "" {
			latest[name] = e.Latest
		}
	}
	return latest
}

// ── backend: uv tool ─────────────────────────────────────────────

type uvToolEntry struct {
	Name    string
	Version string
}

// parseUvToolList parses `uv tool list`:
//
//	kaggle v2.2.4
//	- kaggle
//	jupyterlab v4.5.0
//	- jupyter
//	- jupyter-lab
//
// One package line per tool (name, a space, "v" + version), followed by one
// "- <exposed command>" line per binary it puts on PATH. Only the package
// lines are parsed — skipping "-" lines matters because a tool can expose a
// command that happens to share a name with another package. Shared with
// packagehealth.go's jupyterlab check, so the two features' understanding of
// this output can't drift apart.
func parseUvToolList(output string) []uvToolEntry {
	var entries []uvToolEntry
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		version := ""
		if len(fields) >= 2 {
			version = strings.TrimPrefix(fields[1], "v")
		}
		entries = append(entries, uvToolEntry{Name: fields[0], Version: version})
	}
	return entries
}

// discoverUvTools has no "Latest" to offer: uv has no built-in outdated
// query, and guessing from PyPI would mean a network call per tool on every
// pane load. Left blank rather than faked, same as go below.
func discoverUvTools(ctx context.Context) ([]pkg, bool) {
	p, ok := have("uv")
	if !ok {
		return nil, false
	}
	out, err := exec.CommandContext(ctx, p, "tool", "list").Output()
	if err != nil {
		return nil, true
	}
	var pkgs []pkg
	for _, e := range parseUvToolList(string(out)) {
		pkgs = append(pkgs, pkg{Manager: pmUvTool, Name: e.Name, Version: e.Version})
	}
	return pkgs, true
}

// ── backend: pip (user site) ─────────────────────────────────────

// discoverPip tries pip3 first: on this setup "pip" is only a zsh alias
// (`alias pip='pip3'` in macos/zsh/.zshrc), not a real binary, so a
// non-interactive process seeing a bare PATH would never find it. `--user`
// scopes this to what a person installed by hand, not whatever a venv or
// system Python bundled — the same "was this actually a deliberate global
// install" question every other backend here is answering.
func discoverPip(ctx context.Context) ([]pkg, bool) {
	p, ok := have("pip3")
	if !ok {
		p, ok = have("pip")
	}
	if !ok {
		return nil, false
	}
	out, err := exec.CommandContext(ctx, p, "list", "--user", "--format=json").Output()
	if err != nil {
		return nil, true
	}
	pkgs := parsePipList(string(out))

	outdated, _ := exec.CommandContext(ctx, p, "list", "--user", "--outdated", "--format=json").Output()
	applyLatest(pkgs, parsePipOutdated(string(outdated)))
	return pkgs, true
}

func parsePipList(output string) []pkg {
	var parsed []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if json.Unmarshal([]byte(output), &parsed) != nil {
		return nil
	}
	pkgs := make([]pkg, 0, len(parsed))
	for _, p := range parsed {
		pkgs = append(pkgs, pkg{Manager: pmPip, Name: p.Name, Version: p.Version})
	}
	return pkgs
}

func parsePipOutdated(output string) map[string]string {
	var parsed []struct {
		Name          string `json:"name"`
		LatestVersion string `json:"latest_version"`
	}
	if json.Unmarshal([]byte(output), &parsed) != nil {
		return nil
	}
	latest := map[string]string{}
	for _, p := range parsed {
		if p.LatestVersion != "" {
			latest[p.Name] = p.LatestVersion
		}
	}
	return latest
}

// ── backend: go (installed binaries) ─────────────────────────────

// discoverGoBin lists $GOBIN (or $GOPATH/bin, or ~/go/bin) and asks each
// binary its own build info via `go version -m` — the module path and
// version a binary was built from is embedded at build time, so this needs
// no separate manifest the way `go install` itself keeps none.
//
// Latest is deliberately left blank: knowing it needs one
// `go list -m -versions` network round trip per binary, and that cost
// multiplies with every tool scanned here. No upgrade action is offered for
// the same reason packageAction skips pmGo — `go install <module>@latest`
// re-fetches into GOPATH rather than upgrading anything in place, which is
// a different action than every other row's "u".
func discoverGoBin(ctx context.Context) ([]pkg, bool) {
	goBin, ok := have("go")
	if !ok {
		return nil, false
	}
	dir := goBinDir(ctx, goBin)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	var pkgs []pkg
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out, err := exec.CommandContext(ctx, goBin, "version", "-m", filepath.Join(dir, e.Name())).Output()
		if err != nil {
			continue // not a Go binary, or built without embedded module info
		}
		if p, ok := parseGoVersionM(e.Name(), string(out)); ok {
			pkgs = append(pkgs, p)
		}
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
	return pkgs, true
}

func goBinDir(ctx context.Context, goBin string) string {
	if out, err := exec.CommandContext(ctx, goBin, "env", "GOBIN").Output(); err == nil {
		if dir := strings.TrimSpace(string(out)); dir != "" {
			return dir
		}
	}
	if out, err := exec.CommandContext(ctx, goBin, "env", "GOPATH").Output(); err == nil {
		if dir := strings.TrimSpace(string(out)); dir != "" {
			return filepath.Join(dir, "bin")
		}
	}
	return filepath.Join(os.Getenv("HOME"), "go", "bin")
}

// parseGoVersionM reads the "mod" line `go version -m` prints for a
// module-aware binary:
//
//	/path/to/dlv: go1.24.1
//		path	github.com/go-delve/delve/cmd/dlv
//		mod	github.com/go-delve/delve	v1.24.1	h1:...
//		dep	...
//
// The module a binary's main package belongs to is not always named after
// the binary (gopls's main module is golang.org/x/tools/gopls, not gopls) —
// so the row is named after the file on disk, and the mod line is only
// where the version comes from.
func parseGoVersionM(binName, output string) (pkg, bool) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "mod" {
			return pkg{Manager: pmGo, Name: binName, Version: fields[2]}, true
		}
	}
	return pkg{}, false
}

// ── backend: web installer scripts ──────────────────────────────

// installerCLIs lists tools bootstrap.sh installs via a vendor's own web
// install script (`curl -fsSL <url> | bash`) rather than through any package
// manager above — brew, pnpm, npm, uv tool, pip and go all have no way to
// see these, since none of them did the installing. install.sh is also how
// each one upgrades: re-running it just fetches whatever's current, the
// same self-updating shape `uv`/`pnpm`/`fnm`'s own installers already have.
var installerCLIs = []struct {
	bin, install string
}{
	{"claude", "https://claude.ai/install.sh"},
	{"opencode", "https://opencode.ai/install"},
}

func discoverInstallerCLIs(ctx context.Context) ([]pkg, bool) {
	var pkgs []pkg
	ran := false
	for _, c := range installerCLIs {
		p, ok := have(c.bin)
		if !ok {
			continue // not installed here — not every machine runs --ai
		}
		ran = true
		out, err := exec.CommandContext(ctx, p, "--version").Output()
		if err != nil {
			continue
		}
		if v := parseInstallerVersion(string(out)); v != "" {
			pkgs = append(pkgs, pkg{Manager: pmInstaller, Name: c.bin, Version: v})
		}
	}
	return pkgs, ran
}

// parseInstallerVersion takes just the first field: claude prints
// "2.1.226 (Claude Code)", opencode prints a bare "1.18.7" — both leave a
// clean version number as the first whitespace-separated token.
func parseInstallerVersion(out string) string {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// installerURLFor looks up the install command for a pmInstaller package by
// name — Manager alone doesn't say which one, since claude and opencode
// share it the same way multiple names share pmUvTool or pmGo.
func installerURLFor(name string) (string, bool) {
	for _, c := range installerCLIs {
		if c.bin == name {
			return c.install, true
		}
	}
	return "", false
}

// ── actions ───────────────────────────────────────────────────

// packageAction builds the actionSpec behind the "u" key, per manager.
// Returns false for anything with no safe single-package upgrade command —
// currently pmGo, see discoverGoBin's comment for why.
func packageAction(p pkg) (actionSpec, bool) {
	target := p.Latest
	if target == "" {
		target = "latest"
	}

	switch p.Manager {
	case pmBrew:
		return actionSpec{
			Title:   "Upgrade " + p.Name,
			Argv:    []string{"brew", "upgrade", p.Name},
			Confirm: fmt.Sprintf("Upgrade %s (%s → %s) via brew?", p.Name, p.Version, target),
			Timeout: 5 * time.Minute,
		}, true
	case pmPnpm:
		return actionSpec{
			Title:   "Upgrade " + p.Name,
			Argv:    []string{"pnpm", "add", "-g", p.Name + "@latest"},
			Confirm: fmt.Sprintf("Upgrade %s (%s → %s) via pnpm?", p.Name, p.Version, target),
			Timeout: 3 * time.Minute,
		}, true
	case pmNpm:
		return actionSpec{
			Title:   "Upgrade " + p.Name,
			Argv:    []string{"npm", "update", "-g", p.Name},
			Confirm: fmt.Sprintf("Upgrade %s (%s → %s) via npm?", p.Name, p.Version, target),
			Timeout: 3 * time.Minute,
		}, true
	case pmUvTool:
		return actionSpec{
			Title:   "Upgrade " + p.Name,
			Argv:    []string{"uv", "tool", "upgrade", p.Name},
			Confirm: fmt.Sprintf("Upgrade %s via uv tool?", p.Name),
			Timeout: 3 * time.Minute,
		}, true
	case pmPip:
		return actionSpec{
			Title:   "Upgrade " + p.Name,
			Argv:    []string{"pip3", "install", "--user", "--upgrade", p.Name},
			Confirm: fmt.Sprintf("Upgrade %s (%s → %s) via pip?", p.Name, p.Version, target),
			Timeout: 3 * time.Minute,
		}, true
	case pmInstaller:
		url, ok := installerURLFor(p.Name)
		if !ok {
			return actionSpec{}, false
		}
		// Argv runs through a shell rather than exec'd directly, unlike every
		// other case here — the install command IS a pipe (`curl | bash`),
		// same as bootstrap.sh runs it, so there's no argv form without one.
		return actionSpec{
			Title:   "Upgrade " + p.Name,
			Argv:    []string{"sh", "-c", "curl -fsSL " + url + " | bash"},
			Confirm: fmt.Sprintf("Upgrade %s (%s → %s) by re-running its install script?", p.Name, p.Version, target),
			Timeout: 5 * time.Minute,
		}, true
	default:
		return actionSpec{}, false
	}
}

// ── advisories: consolidation, not automation ──────────────────

// computeAdvisories flags packages sitting on a manager this setup has a
// stated preference against, while the preferred one is actually usable —
// pnpm over npm, uv over pip (docs/gotchas-setup.md's Jupyter entry draws
// the same line). Advisory only: moving a package between managers is an
// uninstall-then-reinstall, not a version bump, and something like codex's
// shell wrapper in macos/zsh/.zshrc cares which install path won — that
// decision stays with whoever runs the suggested command by hand, the same
// boundary the doc+doctor package checks already drew around auto-install.
func computeAdvisories(pkgs []pkg) []advisory {
	var advisories []advisory

	npmCount := 0
	pipCount := 0
	for _, p := range pkgs {
		switch p.Manager {
		case pmNpm:
			npmCount++
		case pmPip:
			pipCount++
		}
	}

	if npmCount > 0 {
		if r, ran := pnpmGlobalBinCheck(); ran && r.state == checkOK {
			advisories = append(advisories, advisory{Text: fmt.Sprintf(
				"%d package(s) on npm global — pnpm is set up and reachable here; `pnpm add -g <name>` moves them over.",
				npmCount)})
		}
	}

	if pipCount > 0 {
		if _, ok := have("uv"); ok {
			advisories = append(advisories, advisory{Text: fmt.Sprintf(
				"%d package(s) on pip --user — uv tool install is preferred here (see docs/gotchas-setup.md).",
				pipCount)})
		}
	}

	return advisories
}
