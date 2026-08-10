package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── sections ──────────────────────────────────────────────────

type manageSection int

const (
	secOverview manageSection = iota
	secDotfiles
	secServices
	secPackages
	secProjects
	secMachines
	numSections
)

var sectionNames = [numSections]string{"Overview", "Dotfiles", "Services", "Packages", "Projects", "Machines"}

// ── data shapes ───────────────────────────────────────────────

type dotfilesInfo struct {
	sha    string
	branch string
	// dirtyKnown separates "checked, and it is clean" from "could not check".
	// Without it a failed `git status` fell through to the zero value and the
	// pane showed a confident green "clean" for a directory it had learned
	// nothing about.
	dirtyKnown bool
	dirty      bool
	behind     int // -1 = unknown (no upstream, or the git call failed)
}

type projectInfo struct {
	name       string
	path       string
	branch     string
	dirtyKnown bool
	dirty      bool
	ahead      int // -1 = unknown
	tmux       bool
}

type machineInfo struct {
	alias     string
	hostname  string
	checked   bool
	reachable bool
}

// ── messages ──────────────────────────────────────────────────

type dotfilesInfoMsg struct{ info dotfilesInfo }
type projectsInfoMsg struct{ projects []projectInfo }
type machinesInfoMsg struct{ machines []machineInfo }

// ── model ─────────────────────────────────────────────────────

type manageModel struct {
	repo    string
	w, h    int
	section manageSection

	ovInfo    overviewInfo
	ovLoading bool
	ovScroll  int

	dfInfo    dotfilesInfo
	dfLoading bool
	dfMsg     string
	dfScroll  int

	services     []service
	svcSources   []string
	svcLoading   bool
	svcMsg       string
	svcCursor    int
	svcFilter    string
	svcFiltering bool
	svcTI        textinput.Model
	// svcRunningOnly hides everything not currently running. A server can
	// carry 165 units of which two thirds are oneshots that fired at boot;
	// listing them all by default is honest but not useful.
	svcRunningOnly bool

	packages     []pkg
	advisories   []advisory
	pkgSources   []string
	pkgLoading   bool
	pkgMsg       string
	pkgCursor    int
	pkgFilter    string
	pkgFiltering bool
	pkgTI        textinput.Model
	pkgSortMode  pkgSortMode // zero value = pkgSortOutdated, today's order
	pkgMgrFilter pkgManager  // zero value = pkgManagerAll, every group shown

	projects     []projectInfo
	projLoading  bool
	projCursor   int
	projSelected int
	// projTmuxHint is the last outcome of "enter" on a project: either
	// confirmation that switch-client moved the current tmux client, or —
	// when we are not inside tmux and so cannot attach one ourselves — the
	// command the user should run in their own terminal.
	projTmuxHint string

	machines    []machineInfo
	machLoading bool
	machCursor  int
}

func newManageModel(repo string) manageModel {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "filter services"
	ti.PromptStyle = styFilter
	ti.TextStyle = styValue
	ti.PlaceholderStyle = styMuted
	ti.CharLimit = 40

	pkgTI := textinput.New()
	pkgTI.Prompt = "/"
	pkgTI.Placeholder = "filter packages"
	pkgTI.PromptStyle = styFilter
	pkgTI.TextStyle = styValue
	pkgTI.PlaceholderStyle = styMuted
	pkgTI.CharLimit = 40

	return manageModel{
		svcTI:        ti,
		pkgTI:        pkgTI,
		repo:         repo,
		ovLoading:    true,
		dfInfo:       dotfilesInfo{behind: -1},
		dfLoading:    true,
		svcLoading:   true,
		pkgLoading:   true,
		projLoading:  true,
		projSelected: -1,
		machLoading:  true,
	}
}

func (m manageModel) Init() tea.Cmd {
	return tea.Batch(
		fetchOverviewInfo(),
		fetchDotfilesInfo(m.repo),
		discoverServices(),
		discoverPackages(),
		fetchProjectsInfo(),
		fetchMachinesInfo(),
	)
}

func (m manageModel) resize(w, h int) manageModel {
	m.w, m.h = w, h
	// scrollWindow re-clamps at render either way, so a stale offset never
	// corrupts anything visible — but leaving it unclamped means a resize
	// from small to large can leave ovScroll/dfScroll positive after the
	// content no longer overflows, so the first j/k in the larger pane looks
	// like a no-op while it silently walks the offset back down to 0.
	if rows := m.bodyRows(); rows > 0 {
		if _, off := scrollWindow(m.overviewLines(), m.ovScroll, rows); off != m.ovScroll {
			m.ovScroll = off
		}
		if _, off := scrollWindow(m.dotfilesLines(), m.dfScroll, rows); off != m.dfScroll {
			m.dfScroll = off
		}
	}
	return m
}

// filtering reports whether a filter box currently owns the keyboard. Both
// the section-switch keys above and app.go's capturingInput() check this
// before anything else — a filter must see every key it can hold text or a
// cursor position for, including the ones that mean "switch section"
// everywhere else.
func (m manageModel) filtering() bool {
	return m.svcFiltering || m.pkgFiltering
}

// bodyRows is the number of lines a section's own content has to work with,
// matching contentColumn's documented h-3 body budget (layout.go). Sections
// whose content can outgrow the pane (Overview, Dotfiles) use it to decide
// how much of their scroll offset is actually visible. Services/Packages
// compute their own row budget already (advisories and the outdated block
// eat into theirs); this is not a replacement for that.
func (m manageModel) bodyRows() int {
	rows := m.h - 3
	if rows < 3 {
		rows = 3
	}
	return rows
}

// scrollWindow slices lines to whatever fits at rows starting from offset,
// clamping offset to a range that can never scroll past the end or before
// the start. Callers that mutate a scroll field should reuse the clamped
// offset returned here to keep the stored value in range too.
func scrollWindow(lines []string, offset, rows int) (window []string, clamped int) {
	max := len(lines) - rows
	if max < 0 {
		max = 0
	}
	if offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + rows
	if end > len(lines) {
		end = len(lines)
	}
	return lines[offset:end], offset
}

// ── update ────────────────────────────────────────────────────

func (m manageModel) update(msg tea.Msg) (manageModel, tea.Cmd) {
	switch msg := msg.(type) {
	case overviewInfoMsg:
		m.ovInfo = msg.info
		m.ovLoading = false
		return m, nil

	case dotfilesInfoMsg:
		m.dfInfo = msg.info
		m.dfLoading = false
		return m, nil

	case servicesFoundMsg:
		m.services = msg.services
		m.svcSources = msg.sources
		m.svcLoading = false
		if msg.err != "" {
			m.svcMsg = msg.err
		}
		// Clamp against the VISIBLE list, not the total. With a filter active,
		// a rescan could leave the cursor past the end of what is shown — the
		// highlight vanished and s/x/R became silent no-ops on rows plainly on
		// screen, because the key handler guards on the visible length.
		if n := len(m.visibleServices()); m.svcCursor >= n {
			m.svcCursor = n - 1
		}
		if m.svcCursor < 0 {
			m.svcCursor = 0
		}
		// Discovery says what is loaded; probing says whether it answers.
		return m, probeServices(m.services)

	case servicesProbedMsg:
		for i := range m.services {
			if m.services[i].Port == 0 {
				continue
			}
			m.services[i].Probed = true
			m.services[i].Healthy = msg.ports[m.services[i].Port]
		}
		return m, nil

	case packagesFoundMsg:
		m.packages = msg.packages
		m.advisories = msg.advisories
		m.pkgSources = msg.sources
		m.pkgLoading = false
		// Same clamp-against-visible reasoning as servicesFoundMsg: a rescan
		// with an active filter must not strand the cursor past what's shown.
		if n := len(m.visiblePackages()); m.pkgCursor >= n {
			m.pkgCursor = n - 1
		}
		if m.pkgCursor < 0 {
			m.pkgCursor = 0
		}
		return m, nil

	case projectsInfoMsg:
		m.projects = msg.projects
		m.projLoading = false
		if m.projCursor >= len(m.projects) {
			m.projCursor = len(m.projects) - 1
		}
		if m.projCursor < 0 {
			m.projCursor = 0
		}
		return m, nil

	case projTmuxMsg:
		m.projTmuxHint = msg.hint
		return m, nil

	case machinesInfoMsg:
		m.machines = msg.machines
		m.machLoading = false
		if m.machCursor >= len(m.machines) {
			m.machCursor = len(m.machines) - 1
		}
		if m.machCursor < 0 {
			m.machCursor = 0
		}
		return m, nil

	case tea.KeyMsg:
		// See the navigation contract above manageModel.keys() in keys.go:
		// left/right is the only thing that ever switches the rail, up/down is
		// the only thing that ever moves within a section, and neither fires
		// while a filter box owns the keyboard. This used to also switch
		// section on j/k/up/down in the two sections with no row list
		// (Overview, Dotfiles) — up/down now scrolls those instead (see
		// updateOverviewKey/updateDotfilesKey), so the split is gone and every
		// section behaves the same way.
		if !m.filtering() {
			switch msg.String() {
			case "l", "right":
				m.section = (m.section + 1) % numSections
				return m, nil
			case "h", "left":
				m.section = (m.section + numSections - 1) % numSections
				return m, nil
			}
		}

		switch m.section {
		case secOverview:
			return m.updateOverviewKey(msg)
		case secDotfiles:
			return m.updateDotfilesKey(msg)
		case secServices:
			return m.updateServicesKey(msg)
		case secPackages:
			return m.updatePackagesKey(msg)
		case secProjects:
			return m.updateProjectsKey(msg)
		case secMachines:
			return m.updateMachinesKey(msg)
		}
	}
	return m, nil
}

// updateOverviewKey is deliberately narrow: this section only ever reports
// on the machine, it never acts on it, so refresh and scroll are all it
// handles.
func (m manageModel) updateOverviewKey(msg tea.KeyMsg) (manageModel, tea.Cmd) {
	switch msg.String() {
	case "r":
		m.ovLoading = true
		return m, fetchOverviewInfo()
	case "j", "down":
		_, m.ovScroll = scrollWindow(m.overviewLines(), m.ovScroll+1, m.bodyRows())
		return m, nil
	case "k", "up":
		_, m.ovScroll = scrollWindow(m.overviewLines(), m.ovScroll-1, m.bodyRows())
		return m, nil
	}
	return m, nil
}

func (m manageModel) updateDotfilesKey(msg tea.KeyMsg) (manageModel, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		_, m.dfScroll = scrollWindow(m.dotfilesLines(), m.dfScroll+1, m.bodyRows())
		return m, nil
	case "k", "up":
		_, m.dfScroll = scrollWindow(m.dotfilesLines(), m.dfScroll-1, m.bodyRows())
		return m, nil
	case "u":
		if m.repo == "" {
			m.dfMsg = "no checkout to update"
			return m, nil
		}
		return m, requestAction(syncInboundRequest{Repo: m.repo})
	case "L":
		if m.repo == "" {
			m.dfMsg = "no checkout to relink"
			return m, nil
		}
		return m, requestAction(applyRequest{Repo: m.repo})
	case "p":
		return m, requestAction(nvimRestoreRequest{})
	case "t":
		if m.repo == "" {
			m.dfMsg = "no repo found"
			return m, nil
		}
		return m, requestAction(tpmRepairRequest{Repo: m.repo})
	case "D":
		if m.repo == "" {
			m.dfMsg = "no repo found"
			return m, nil
		}
		return m, requestAction(depsRequest{Repo: m.repo})
	}
	return m, nil
}

func (m manageModel) updateServicesKey(msg tea.KeyMsg) (manageModel, tea.Cmd) {
	if m.svcFiltering {
		switch msg.String() {
		case "esc":
			m.svcFiltering = false
			m.svcTI.Blur()
			m.svcTI.SetValue("")
			m.svcFilter = ""
			m.svcCursor = 0
			return m, nil
		case "enter":
			m.svcFiltering = false
			m.svcTI.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.svcTI, cmd = m.svcTI.Update(msg)
		if v := m.svcTI.Value(); v != m.svcFilter {
			m.svcFilter = v
			m.svcCursor = 0
		}
		return m, cmd
	}

	vis := m.visibleServices()

	switch msg.String() {
	case "j", "down":
		if m.svcCursor < len(vis)-1 {
			m.svcCursor++
		}
		return m, nil
	case "k", "up":
		if m.svcCursor > 0 {
			m.svcCursor--
		}
		return m, nil
	case "/":
		m.svcFiltering = true
		m.svcTI.Focus()
		return m, textinput.Blink
	case "a":
		m.svcRunningOnly = !m.svcRunningOnly
		m.svcCursor = 0
		return m, nil
	case "r":
		m.svcLoading = true
		m.svcMsg = ""
		return m, discoverServices()
	}

	verb := ""
	switch msg.String() {
	case "s":
		verb = "start"
	case "x":
		verb = "stop"
	case "R":
		verb = "restart"
	default:
		return m, nil
	}

	if m.svcCursor >= len(vis) {
		return m, nil
	}
	_, ok := serviceAction(vis[m.svcCursor], verb)
	if !ok {
		// Saying why is better than a key that appears to do nothing.
		m.svcMsg = verb + " is not available for " + svcSourceName(vis[m.svcCursor].Source) +
			" units of this kind"
		return m, nil
	}
	m.svcMsg = ""
	return m, requestAction(serviceRequest{Service: vis[m.svcCursor], Verb: verb})
}

func (m manageModel) updatePackagesKey(msg tea.KeyMsg) (manageModel, tea.Cmd) {
	if m.pkgFiltering {
		switch msg.String() {
		case "esc":
			m.pkgFiltering = false
			m.pkgTI.Blur()
			m.pkgTI.SetValue("")
			m.pkgFilter = ""
			m.pkgCursor = 0
			return m, nil
		case "enter":
			m.pkgFiltering = false
			m.pkgTI.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.pkgTI, cmd = m.pkgTI.Update(msg)
		if v := m.pkgTI.Value(); v != m.pkgFilter {
			m.pkgFilter = v
			m.pkgCursor = 0
		}
		return m, cmd
	}

	vis := m.visiblePackages()

	switch msg.String() {
	case "j", "down":
		if m.pkgCursor < len(vis)-1 {
			m.pkgCursor++
		}
		return m, nil
	case "k", "up":
		if m.pkgCursor > 0 {
			m.pkgCursor--
		}
		return m, nil
	case "/":
		m.pkgFiltering = true
		m.pkgTI.Focus()
		return m, textinput.Blink
	case "r":
		m.pkgLoading = true
		m.pkgMsg = ""
		return m, discoverPackages()
	case "s":
		if m.pkgSortMode == pkgSortOutdated {
			m.pkgSortMode = pkgSortName
		} else {
			m.pkgSortMode = pkgSortOutdated
		}
		m.pkgCursor = 0
		return m, nil
	case "m":
		// Cycles All → Pnpm → Npm → Uv Tool → Pip → Go → Installer → Brew →
		// All, the same order the table already groups by — so one press
		// always lands on whichever group currently follows the one you're
		// looking at.
		m.pkgMgrFilter = (m.pkgMgrFilter + 1) % numPkgManagers
		m.pkgCursor = 0
		return m, nil
	case "u":
		if m.pkgCursor >= len(vis) {
			return m, nil
		}
		p := vis[m.pkgCursor]
		_, ok := packageAction(p)
		if !ok {
			// Saying why is better than a key that appears to do nothing —
			// same rule Services' s/x/R already follows.
			m.pkgMsg = "no upgrade action for " + p.Manager.String() + " packages"
			return m, nil
		}
		m.pkgMsg = ""
		return m, requestAction(packageRequest{Package: p})
	}
	return m, nil
}

func (m manageModel) updateProjectsKey(msg tea.KeyMsg) (manageModel, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.projCursor < len(m.projects)-1 {
			m.projCursor++
		}
	case "k", "up":
		if m.projCursor > 0 {
			m.projCursor--
		}
	case "enter":
		if m.projCursor < len(m.projects) {
			m.projSelected = m.projCursor
			return m, m.openProjectTmux(m.projects[m.projCursor])
		}
	}
	return m, nil
}

// openProjectTmux is deliberately NOT run through the action runner: attaching
// to a tmux session needs to own the terminal, and exec.CommandContext there
// gives a command no controlling tty. It also never suspends this program to
// hand the terminal over — that's a bigger change than this pane owns.
//
// When we are already inside tmux, switch-client can retarget the attached
// client without any of that, so it runs for real. Otherwise there is
// nothing this process can safely do: record what the user would run and
// show it as a hint line.
type projTmuxMsg struct{ hint string }

// openProjectTmux does not go through the action runner: attaching to a tmux
// session needs to own the terminal, which an overlay cannot give it.
//
// It is still async. Running exec directly inside Update() blocked the whole
// UI thread for the duration of the call — every other exec in this file is a
// tea.Cmd for exactly that reason, and tmux stalling is not hypothetical on a
// box with a wedged session.
func (m manageModel) openProjectTmux(p projectInfo) tea.Cmd {
	// Abbreviate $HOME. The point of this line is that you can read it and
	// retype it; a path that runs off the edge of the pane serves neither.
	shown := p.path
	if home := os.Getenv("HOME"); home != "" && strings.HasPrefix(shown, home) {
		shown = "~" + strings.TrimPrefix(shown, home)
	}
	cmdLine := fmt.Sprintf("tmux new-session -A -s %s -c %s", p.name, shown)

	if os.Getenv("TMUX") == "" {
		return func() tea.Msg {
			return projTmuxMsg{hint: "run in a terminal: " + cmdLine}
		}
	}

	name, path := p.name, p.path
	_ = path
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "tmux", "switch-client", "-t", name).Run(); err != nil {
			return projTmuxMsg{hint: "no session named " + name + " yet — run: " + cmdLine}
		}
		return projTmuxMsg{hint: "switched to " + name}
	}
}

func (m manageModel) updateMachinesKey(msg tea.KeyMsg) (manageModel, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.machCursor < len(m.machines)-1 {
			m.machCursor++
		}
	case "k", "up":
		if m.machCursor > 0 {
			m.machCursor--
		}
	case "d":
		if m.machCursor < len(m.machines) {
			alias := m.machines[m.machCursor].alias
			return m, requestAction(remoteDoctorRequest{Alias: alias})
		}
	}
	return m, nil
}

// ── commands: dotfiles ────────────────────────────────────────

func runGit(ctx context.Context, repo string, args ...string) (string, error) {
	full := append([]string{"-C", repo}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).CombinedOutput()
	return string(out), err
}

func fetchDotfilesInfo(repo string) tea.Cmd {
	return func() tea.Msg {
		info := dotfilesInfo{behind: -1}
		if repo == "" {
			return dotfilesInfoMsg{info: info}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if out, err := runGit(ctx, repo, "rev-parse", "--short", "HEAD"); err == nil {
			info.sha = strings.TrimSpace(out)
		}
		if out, err := runGit(ctx, repo, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			info.branch = strings.TrimSpace(out)
		}
		if out, err := runGit(ctx, repo, "status", "--porcelain"); err == nil {
			info.dirtyKnown = true
			info.dirty = strings.TrimSpace(out) != ""
		}
		// @{u} needs upstream tracking, which is NOT set by `git push origin
		// main` — only by `push -u` or a fresh clone. A repo whose remote was
		// re-added by hand therefore has none, and this silently reported
		// "behind unknown" forever. Fall back to origin/<branch>, which needs
		// no tracking at all.
		countBehind := func(ref string) (int, bool) {
			out, err := runGit(ctx, repo, "rev-list", "--count", "HEAD.."+ref)
			if err != nil {
				return 0, false
			}
			n, cerr := strconv.Atoi(strings.TrimSpace(out))
			return n, cerr == nil
		}
		if n, ok := countBehind("@{u}"); ok {
			info.behind = n
		} else if info.branch != "" {
			if n, ok := countBehind("origin/" + info.branch); ok {
				info.behind = n
			}
		}
		return dotfilesInfoMsg{info: info}
	}
}

func firstLine(out string, err error) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return err.Error()
	}
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	return out
}

// ── commands: services ────────────────────────────────────────

// ── commands: projects ────────────────────────────────────────

func fetchProjectsInfo() tea.Cmd {
	return func() tea.Msg {
		home := os.Getenv("HOME")
		roots := []string{
			filepath.Join(home, "Codes", "Projects"),
			filepath.Join(home, "Codes", "Hobby"),
			filepath.Join(home, "Codes", "Learning"),
		}

		var projects []projectInfo
		for _, root := range roots {
			entries, err := os.ReadDir(root)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				p := filepath.Join(root, e.Name())
				if _, err := os.Stat(filepath.Join(p, ".git")); err != nil {
					continue
				}
				projects = append(projects, projectInfo{name: e.Name(), path: p, ahead: -1})
			}
		}

		sessions := tmuxSessions()
		for i := range projects {
			p := &projects[i]
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if out, err := runGit(ctx, p.path, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
				p.branch = strings.TrimSpace(out)
			}
			if out, err := runGit(ctx, p.path, "status", "--porcelain"); err == nil {
				p.dirtyKnown = true
				p.dirty = strings.TrimSpace(out) != ""
			}
			if out, err := runGit(ctx, p.path, "rev-list", "--count", "@{u}..HEAD"); err == nil {
				if n, cerr := strconv.Atoi(strings.TrimSpace(out)); cerr == nil {
					p.ahead = n
				}
			}
			cancel()
			p.tmux = sessions[p.name]
		}

		sort.Slice(projects, func(i, j int) bool { return projects[i].name < projects[j].name })
		return projectsInfoMsg{projects: projects}
	}
}

func tmuxSessions() map[string]bool {
	out := map[string]bool{}
	if _, ok := have("tmux"); !ok {
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", "#{session_name}").CombinedOutput()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line != "" {
			out[line] = true
		}
	}
	return out
}

// ── commands: machines ────────────────────────────────────────

func fetchMachinesInfo() tea.Cmd {
	return func() tea.Msg {
		hosts := parseSSHConfig()
		infos := make([]machineInfo, len(hosts))
		var wg sync.WaitGroup
		for i, h := range hosts {
			infos[i] = machineInfo{alias: h.alias, hostname: h.hostname}
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "ssh",
					"-o", "BatchMode=yes", "-o", "ConnectTimeout=4",
					infos[i].alias, "echo ok")
				out, err := cmd.CombinedOutput()
				infos[i].checked = true
				infos[i].reachable = err == nil && strings.TrimSpace(string(out)) == "ok"
			}(i)
		}
		wg.Wait()
		return machinesInfoMsg{machines: infos}
	}
}

// ── view ──────────────────────────────────────────────────────

func (m manageModel) view(spin string) string {
	// A left rail rather than horizontal segments, so all three tabs share one
	// shape: rail, then a content column with a two-line header and a rule.
	// The sections were a row of pills here and a sidebar in Docs, which is a
	// large part of why the app read as three separate programs.
	showRail := m.w >= 76

	items := make([]railItem, 0, numSections+1)
	items = append(items, railItem{label: "Manage", isHead: true})
	for i := manageSection(0); i < numSections; i++ {
		items = append(items, railItem{label: sectionNames[i]})
	}

	body, summary := m.sectionBody(spin)
	contentW := m.w
	if showRail {
		contentW = m.w - railWidth - 1 // -1: the rail border sits outside Width
	}

	// Packages' manager filter gets its own breadcrumb segment rather than
	// living only in the summary line — "Packages › Brew" reads as the inner
	// section it behaves like, the same idea as the outer Manage rail one
	// level up, just without a second rail's width to spend on it.
	title := sectionNames[m.section]
	if m.section == secPackages && m.pkgMgrFilter != pkgManagerAll {
		title += " › " + m.pkgMgrFilter.managerTitle()
	}

	content := contentColumn(contentW, m.h,
		paneHeader("Manage", title, summary, measureFor(contentW)),
		body)

	if !showRail {
		return content
	}
	rail := renderRail(items, int(m.section)+1, railWidth, m.h, "")
	return lipgloss.JoinHorizontal(lipgloss.Top, rail, content)
}

// sectionBody returns the section's content and a one-line summary for the
// header, so every section states its own state in the same place.
func (m manageModel) sectionBody(spin string) (string, string) {
	switch m.section {
	case secOverview:
		return m.viewOverview(spin), m.overviewSummary()
	case secDotfiles:
		return m.viewDotfiles(), m.dotfilesSummary()
	case secServices:
		return m.viewServices(spin), m.servicesSummary()
	case secPackages:
		return m.viewPackages(spin), m.packagesSummary()
	case secProjects:
		return m.viewProjects(), fmt.Sprintf("%d repositories under ~/Codes", len(m.projects))
	case secMachines:
		return m.viewMachines(), fmt.Sprintf("%d hosts in ~/.ssh/config", len(m.machines))
	}
	return "", ""
}

func (m manageModel) overviewSummary() string {
	if m.ovLoading {
		return "gathering machine info…"
	}
	host := m.ovInfo.host
	if host == "" {
		host = "this machine"
	}
	return fmt.Sprintf("%s  ·  %s  ·  %s", host, m.ovInfo.osName, m.ovInfo.arch)
}

func (m manageModel) dotfilesSummary() string {
	if m.dfLoading {
		return "reading the checkout…"
	}
	if m.repo == "" {
		return "no checkout found"
	}
	if m.dfInfo.behind > 0 {
		return fmt.Sprintf("%d commits behind origin", m.dfInfo.behind)
	}
	return "up to date with origin"
}

func (m manageModel) servicesSummary() string {
	if m.svcLoading {
		return "discovering services…"
	}
	if len(m.services) == 0 {
		return "no services discovered on this machine"
	}
	run := 0
	for _, s := range m.services {
		if s.Running {
			run++
		}
	}
	src := strings.Join(m.svcSources, ", ")
	if src == "" {
		src = "none"
	}
	scope := "all"
	if m.svcRunningOnly {
		scope = "running only"
	}
	return fmt.Sprintf("%d of %d running  ·  %s  ·  via %s", run, len(m.services), scope, src)
}

func (m manageModel) packagesSummary() string {
	if m.pkgLoading {
		return "checking package managers…"
	}
	if len(m.packages) == 0 {
		return "no package managers found on this machine"
	}
	outdated := 0
	for _, p := range m.packages {
		if p.Outdated() {
			outdated++
		}
	}
	src := strings.Join(m.pkgSources, ", ")
	if src == "" {
		src = "none"
	}
	// Counts stay whole-machine even when a manager filter narrows the table
	// beneath them — same call as the Outdated block: this line answers "what
	// does the machine look like," not "what's currently on screen."
	s := fmt.Sprintf("%d packages, %d outdated  ·  via %s  ·  sorted by %s",
		len(m.packages), outdated, src, m.pkgSortMode)
	if m.pkgMgrFilter != pkgManagerAll {
		s += "  ·  showing " + m.pkgMgrFilter.String() + " only"
	}
	return s
}

// overviewLines is the content Overview shows, one stat per line. Split out
// from viewOverview so both the view and the scroll-key handler can measure
// and window it without shelling out or duplicating the formatting.
func (m manageModel) overviewLines() []string {
	measure := measureFor(m.w - railWidth - 1)
	const keyW = 10
	valW := measure - keyW
	if valW < 4 {
		valW = 4
	}

	info := m.ovInfo
	var lines []string

	host := info.host
	if host == "" {
		host = "unknown"
	}
	lines = append(lines, statRow("host", styValue.Render(truncate(host, valW)), keyW))

	osName := info.osName
	if osName == "" {
		osName = runtime.GOOS
	}
	lines = append(lines, statRow("os", styValue.Render(truncate(osName, valW)), keyW))

	lines = append(lines, statRow("arch", styValue.Render(info.arch), keyW))

	profile := "full"
	if info.profileLight {
		profile = "light"
	}
	lines = append(lines, statRow("profile", styValue.Render(profile), keyW))

	dots := info.version
	if dots == "" {
		dots = "dev"
	}
	if info.distSource != "" {
		dots += " (" + info.distSource + ")"
	}
	lines = append(lines, statRow("dots", styValue.Render(truncate(dots, valW)), keyW))

	lines = append(lines, statRow("repo", m.overviewRepoLine(valW), keyW))

	lines = append(lines, statRow("tools", countSummary(info.toolsHave, info.toolsTotal, "present"), keyW))

	lines = append(lines, statRow("services", m.overviewServicesLine(), keyW))

	if info.memKnown {
		mem := fmt.Sprintf("%s free / %s", formatBytes(info.memFreeBytes), formatBytes(info.memTotalBytes))
		lines = append(lines, statRow("memory", styValue.Render(truncate(mem, valW)), keyW))
	} else {
		lines = append(lines, statRow("memory", styMuted.Render("unknown"), keyW))
	}

	if info.diskKnown {
		disk := fmt.Sprintf("%s free / %s", formatBytes(info.diskFreeBytes), formatBytes(info.diskTotalBytes))
		lines = append(lines, statRow("disk", styValue.Render(truncate(disk, valW)), keyW))
	} else {
		lines = append(lines, statRow("disk", styMuted.Render("unknown"), keyW))
	}

	if info.uptimeKnown {
		lines = append(lines, statRow("uptime", styValue.Render(formatDuration(info.uptime)), keyW))
	} else {
		lines = append(lines, statRow("uptime", styMuted.Render("unknown"), keyW))
	}

	return lines
}

// viewOverview is read-only: every field either comes straight from
// overviewInfo, or — for repo/tools/services — from state another section
// already fetched, so this never re-shells git and never re-scans services.
func (m manageModel) viewOverview(spin string) string {
	if m.ovLoading {
		return "\n  " + spin + styPending.Render(" gathering machine info")
	}

	lines := m.overviewLines()
	rows := m.bodyRows()
	// If everything doesn't fit, the hint telling you so needs a row of its
	// own — appending it after a full-width window pushes the body one line
	// past the pane's budget, and contentColumn's MaxHeight silently drops
	// whichever line lands last. That line is always the hint, so it would
	// never actually render. Reserving the row up front instead means the
	// window is one line shorter exactly when a hint is going to sit below it.
	if len(lines) > rows {
		rows--
	}
	win, off := scrollWindow(lines, m.ovScroll, rows)
	body := strings.Join(win, "\n")
	if hidden := len(lines) - off - len(win); hidden > 0 {
		body += "\n" + styMuted.Render(fmt.Sprintf("↓ %d more, j/k to scroll", hidden))
	}
	return body
}

// overviewRepoLine reuses m.repo/m.dfInfo — the same state Dotfiles renders
// — rather than shelling out to git again.
func (m manageModel) overviewRepoLine(w int) string {
	if m.repo == "" {
		return styMuted.Render("not found")
	}
	path := styValue.Render(truncate(m.repo, w))
	if m.dfLoading {
		return path + "  " + styMuted.Render("checking…")
	}

	bits := []string{path}
	if m.dfInfo.sha != "" {
		bits = append(bits, styValue.Render(m.dfInfo.sha))
	}
	if m.dfInfo.branch != "" {
		bits = append(bits, styValue.Render(m.dfInfo.branch))
	}
	switch {
	case !m.dfInfo.dirtyKnown:
		bits = append(bits, styMuted.Render("status unknown"))
	case m.dfInfo.dirty:
		bits = append(bits, styBad.Render("dirty"))
	default:
		bits = append(bits, styOK.Render("clean"))
	}
	switch {
	case m.dfInfo.behind > 0:
		bits = append(bits, styPending.Render(fmt.Sprintf("%d behind", m.dfInfo.behind)))
	case m.dfInfo.behind == 0:
		bits = append(bits, styOK.Render("up to date"))
	}
	return truncate(strings.Join(bits, styMuted.Render("  ·  ")), w)
}

// overviewServicesLine reuses m.services — already discovered for the
// Services section — rather than scanning launchd/systemd/docker again.
func (m manageModel) overviewServicesLine() string {
	if m.svcLoading && len(m.services) == 0 {
		return styMuted.Render("discovering…")
	}
	if len(m.services) == 0 {
		return styMuted.Render("none discovered")
	}
	run := 0
	for _, s := range m.services {
		if s.Running {
			run++
		}
	}
	return countSummary(run, len(m.services), "running")
}

// dotfilesLines is the content Dotfiles shows, split out from viewDotfiles
// for the same reason overviewLines is: the scroll-key handler needs to
// measure it without duplicating the formatting.
func (m manageModel) dotfilesLines() []string {
	var lines []string

	repo := m.repo
	if repo == "" {
		repo = styMuted.Render("not found")
	} else {
		repo = styValue.Render(truncate(repo, m.w-14))
	}
	lines = append(lines, styKey.Render("repo    ")+repo)

	branch := m.dfInfo.branch
	if branch == "" {
		lines = append(lines, styKey.Render("branch  ")+styMuted.Render("unknown"))
	} else {
		lines = append(lines, styKey.Render("branch  ")+styValue.Render(branch))
	}

	sha := m.dfInfo.sha
	if sha == "" {
		lines = append(lines, styKey.Render("sha     ")+styMuted.Render("unknown"))
	} else {
		lines = append(lines, styKey.Render("sha     ")+styValue.Render(sha))
	}

	if !m.dfInfo.dirtyKnown {
		lines = append(lines, styKey.Render("status  ")+styMuted.Render("unknown"))
	} else if m.dfInfo.dirty {
		lines = append(lines, styKey.Render("status  ")+styBad.Render("dirty"))
	} else {
		lines = append(lines, styKey.Render("status  ")+styOK.Render("clean"))
	}

	switch {
	case m.dfInfo.behind < 0:
		lines = append(lines, styKey.Render("behind  ")+styMuted.Render("unknown"))
	case m.dfInfo.behind == 0:
		lines = append(lines, styKey.Render("behind  ")+styOK.Render("up to date"))
	default:
		lines = append(lines, styKey.Render("behind  ")+styPending.Render(fmt.Sprintf("%d commit(s)", m.dfInfo.behind)))
	}

	if m.dfMsg != "" {
		lines = append(lines, "", styMuted.Render(m.dfMsg))
	}

	return lines
}

func (m manageModel) viewDotfiles() string {
	if m.dfLoading {
		return styPending.Render("checking…")
	}

	lines := m.dotfilesLines()
	rows := m.bodyRows()
	// See viewOverview: reserve a row for the hint before windowing, or the
	// hint is always the line MaxHeight clips off and never actually shows.
	if len(lines) > rows {
		rows--
	}
	win, off := scrollWindow(lines, m.dfScroll, rows)
	body := strings.Join(win, "\n")
	if hidden := len(lines) - off - len(win); hidden > 0 {
		body += "\n" + styMuted.Render(fmt.Sprintf("↓ %d more, j/k to scroll", hidden))
	}
	return body
}

func (m manageModel) viewServices(spin string) string {
	measure := measureFor(m.w - railWidth - 1)

	if m.svcLoading {
		return "\n  " + spin + styPending.Render(" discovering")
	}
	if len(m.services) == 0 {
		return "\n  " + styMuted.Render(
			"Nothing found. Looked for launchd agents, systemd units and docker containers.")
	}

	var head string
	if m.svcFilter != "" || m.svcFiltering {
		m.svcTI.Width = measure - 4
		head = "  " + truncate(m.svcTI.View(), measure) + "\n"
	}

	vis := m.visibleServices()
	if len(vis) == 0 {
		return head + "  " + styMuted.Render("nothing matches that filter")
	}

	// Only as many rows as fit, cursor kept in view: a Mac can carry dozens of
	// agents and the pane must not try to draw them all.
	rows := m.h - 9
	if rows < 3 {
		rows = 3
	}
	start := 0
	if len(vis) > rows {
		start = m.svcCursor - rows/2
		if start < 0 {
			start = 0
		}
		if start > len(vis)-rows {
			start = len(vis) - rows
		}
	}
	end := start + rows
	if end > len(vis) {
		end = len(vis)
	}

	data := make([][]string, 0, end-start)
	for i := start; i < end; i++ {
		s := vis[i]
		// Running-but-not-answering gets its own mark. Folding it into
		// "running" hides the failure that actually matters.
		dot := stateDot(s.Running, true)
		state := "stopped"
		if s.Running {
			state = "running"
			if s.Probed && !s.Healthy {
				dot = styPending.Render("●")
				state = "not answering"
			}
		}
		port := ""
		if s.Port != 0 {
			port = ":" + strconv.Itoa(s.Port)
		}
		data = append(data, []string{
			dot + " " + s.Name,
			svcSourceName(s.Source),
			state,
			port,
			s.Detail,
		})
	}

	body := head + dataTable(
		[]string{"SERVICE", "VIA", "STATE", "PORT", "DETAIL"},
		data, m.svcCursor-start, measure)

	if len(vis) > rows {
		body += "\n  " + styMuted.Render(fmt.Sprintf("… %d more, / to filter", len(vis)-rows))
	}
	if m.svcMsg != "" {
		body += "\n  " + styMuted.Render(truncate(m.svcMsg, measure))
	}
	return body
}

func svcSourceName(s svcSource) string {
	switch s {
	case srcLaunchd:
		return "launchd"
	case srcSystemd:
		return "systemd"
	case srcDocker:
		return "docker"
	}
	return "?"
}

// visibleServices applies the search box. Discovery is deliberately broad, so
// filtering is what makes a list of dozens usable.
func (m manageModel) visibleServices() []service {
	q := strings.ToLower(m.svcFilter)
	out := make([]service, 0, len(m.services))
	for _, s := range m.services {
		if m.svcRunningOnly && !s.Running {
			continue
		}
		if q != "" && !strings.Contains(
			strings.ToLower(s.Name+" "+s.ID+" "+svcSourceName(s.Source)), q) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// viewPackages groups rows by manager the way Doctor's checks are grouped —
// a label in the first column only when the manager changes, not a flat list
// like Services — since "which manager is this" is the first thing worth
// knowing about a package row.
// pkgOutdatedOverviewCap bounds the Outdated summary block so a machine with
// dozens of stale packages doesn't push the manager-grouped table below off
// screen — the same reasoning viewServices/viewPackages already apply to
// their own row lists.
const pkgOutdatedOverviewCap = 5

// outdatedOverviewBlock renders the "what's outdated, across every manager"
// summary shown above the manager-grouped table. Built from m.packages
// (unfiltered) rather than the visible/filtered slice — it's a whole-machine
// summary, not scoped to whatever the filter box happens to show. Returns ""
// with height 0 when nothing is outdated, so a fully up-to-date machine
// doesn't carry an empty header around.
func (m manageModel) outdatedOverviewBlock(measure int) (string, int) {
	shown, more := outdatedOverview(m.packages, pkgOutdatedOverviewCap)
	if len(shown) == 0 {
		return "", 0
	}
	total := len(shown) + more

	var b strings.Builder
	b.WriteString("  " + styGroup.Render(fmt.Sprintf("OUTDATED (%d)", total)) + "\n")
	height := 1
	for _, p := range shown {
		line := fmt.Sprintf("  %s %s  %s %s %s",
			styMuted.Render(strings.ToUpper(p.Manager.String())),
			styValue.Render(p.Name), p.Version,
			styMuted.Render("→"), styPending.Render(p.Latest))
		b.WriteString(truncate(line, measure) + "\n")
		height++
	}
	if more > 0 {
		b.WriteString("  " + styMuted.Render(fmt.Sprintf("… %d more outdated", more)) + "\n")
		height++
	}
	return b.String(), height
}

func (m manageModel) viewPackages(spin string) string {
	measure := measureFor(m.w - railWidth - 1)

	if m.pkgLoading {
		return "\n  " + spin + styPending.Render(" checking package managers")
	}
	if len(m.packages) == 0 {
		return "\n  " + styMuted.Render(
			"Nothing found. Looked for brew, pnpm, npm, uv tool, pip, go-installed binaries, and claude/opencode.")
	}

	var head string
	if m.pkgFilter != "" || m.pkgFiltering {
		m.pkgTI.Width = measure - 4
		head = "  " + truncate(m.pkgTI.View(), measure) + "\n"
	}

	vis := m.visiblePackages()
	if len(vis) == 0 {
		return head + "  " + styMuted.Render("nothing matches that filter")
	}

	outBlock, outHeight := m.outdatedOverviewBlock(measure)

	var adv strings.Builder
	advHeight := 0
	if len(m.advisories) > 0 {
		for _, a := range m.advisories {
			adv.WriteString("  " + styPending.Render("! ") + styMuted.Render(truncate(a.Text, measure)) + "\n")
		}
		// The blank-line separator below the advisories block is part of
		// its footprint too, same as the pre-redesign math counted.
		advHeight = len(m.advisories) + 1
	}

	// Both fixed blocks above the table are computed and subtracted
	// together, then floored once — subtracting and re-flooring them one at
	// a time could still leave rows too tall for what's actually left once
	// both blocks are present, at the smallest sizes this app supports.
	rows := m.h - 9 - outHeight - advHeight
	if rows < 3 {
		rows = 3
	}
	start := 0
	if len(vis) > rows {
		start = m.pkgCursor - rows/2
		if start < 0 {
			start = 0
		}
		if start > len(vis)-rows {
			start = len(vis) - rows
		}
	}
	end := start + rows
	if end > len(vis) {
		end = len(vis)
	}

	data := make([][]string, 0, end-start)
	lastManager := ""
	for i := start; i < end; i++ {
		p := vis[i]
		label := ""
		mgr := p.Manager.String()
		if mgr != lastManager {
			label = strings.ToUpper(mgr)
			lastManager = mgr
		}
		mark, latest := "", p.Latest
		switch {
		case p.Outdated():
			mark = styPending.Render("↑")
		case latest == "":
			latest = styMuted.Render("—")
		}
		data = append(data, []string{label, p.Name, p.Version, mark + " " + latest})
	}

	body := head + outBlock + adv.String() + dataTable(
		[]string{"", "PACKAGE", "VERSION", "LATEST"},
		data, m.pkgCursor-start, measure)

	if len(vis) > rows {
		body += "\n  " + styMuted.Render(fmt.Sprintf("… %d more, / to filter", len(vis)-rows))
	}
	if m.pkgMsg != "" {
		body += "\n  " + styMuted.Render(truncate(m.pkgMsg, measure))
	}
	return body
}

// visiblePackages applies the manager filter, then the search box, then the
// active sort mode. m.packages is already grouped by manager (discoverPackages
// sorts it that way) and both filters only remove entries, so the grouping
// survives unchanged; sortPackagesFor re-sorts within each group without
// touching that outer order.
func (m manageModel) visiblePackages() []pkg {
	q := strings.ToLower(m.pkgFilter)
	out := make([]pkg, 0, len(m.packages))
	for _, p := range m.packages {
		if m.pkgMgrFilter != pkgManagerAll && p.Manager != m.pkgMgrFilter {
			continue
		}
		if q != "" && !strings.Contains(
			strings.ToLower(p.Name+" "+p.Manager.String()), q) {
			continue
		}
		out = append(out, p)
	}
	sortPackagesFor(out, m.pkgSortMode)
	return out
}

func (m manageModel) viewProjects() string {
	if m.projLoading {
		return styPending.Render("scanning…")
	}
	if len(m.projects) == 0 {
		return styMuted.Render("no projects found under ~/Codes/{Projects,Hobby,Learning}")
	}

	var b strings.Builder
	for i, p := range m.projects {
		cursor := "  "
		if i == m.projCursor {
			cursor = styItemCursor.Render("▍ ")
		}

		name := padRight(truncate(p.name, 22), 22)
		branch := padRight(truncate(p.branch, 16), 16)

		status := styOK.Render("clean")
		if !p.dirtyKnown {
			status = styMuted.Render("unknown")
		} else if p.dirty {
			status = styBad.Render("dirty")
		}

		var ahead string
		switch {
		case p.ahead < 0:
			ahead = styMuted.Render("?")
		case p.ahead == 0:
			ahead = styMuted.Render("-")
		default:
			ahead = styPending.Render(fmt.Sprintf("+%d", p.ahead))
		}

		tmux := styMuted.Render("-")
		if p.tmux {
			tmux = styOK.Render("tmux")
		}

		line := cursor + name + " " + branch + " " + status + "  " + ahead + "  " + tmux
		if i == m.projSelected {
			line = styItemOn.Render(truncate(line, m.w-4))
		} else {
			line = truncate(line, m.w-4)
		}
		b.WriteString(line + "\n")
	}
	if m.projTmuxHint != "" {
		// Wrap rather than truncate. This hint exists to be read and retyped,
		// so cutting the end off the path defeats the whole point of showing
		// it — the tail is the part you need.
		measure := measureFor(m.w - railWidth - 1)
		for _, ln := range wrapPlain(m.projTmuxHint, measure) {
			b.WriteString("\n" + styMuted.Render(ln))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m manageModel) viewMachines() string {
	if m.machLoading {
		return styPending.Render("checking hosts…")
	}
	if len(m.machines) == 0 {
		return styMuted.Render("no usable Host entries in ~/.ssh/config")
	}

	var b strings.Builder
	for i, mh := range m.machines {
		cursor := "  "
		if i == m.machCursor {
			cursor = styItemCursor.Render("▍ ")
		}

		dot := styMuted.Render("○")
		label := styMuted.Render("checking…")
		if mh.checked {
			if mh.reachable {
				dot = styOK.Render("●")
				label = styOK.Render("reachable")
			} else {
				dot = styBad.Render("●")
				label = styBad.Render("unreachable")
			}
		}

		name := padRight(truncate(mh.alias, 18), 18)
		line := cursor + dot + " " + name + " " + label
		b.WriteString(truncate(line, m.w-4) + "\n")
	}
	return b.String()
}

// wrapPlain breaks a string on spaces to fit a width, without hyphenating or
// dropping anything. Used where the text must stay complete — a command you
// are meant to copy cannot be elided.
func wrapPlain(s string, w int) []string {
	if w < 8 {
		w = 8
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var out []string
	line := words[0]
	for _, word := range words[1:] {
		if len(line)+1+len(word) <= w {
			line += " " + word
			continue
		}
		out = append(out, line)
		line = word
	}
	return append(out, line)
}
