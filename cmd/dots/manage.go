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
	secProjects
	secMachines
	numSections
)

var sectionNames = [numSections]string{"Overview", "Dotfiles", "Services", "Projects", "Machines"}

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

	dfInfo    dotfilesInfo
	dfLoading bool
	dfMsg     string

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

	return manageModel{
		svcTI:        ti,
		repo:         repo,
		ovLoading:    true,
		dfInfo:       dotfilesInfo{behind: -1},
		dfLoading:    true,
		svcLoading:   true,
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
		fetchProjectsInfo(),
		fetchMachinesInfo(),
	)
}

func (m manageModel) resize(w, h int) manageModel {
	m.w, m.h = w, h
	return m
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
		// The rail is drawn vertically, so up/down would be the natural way to
		// move it — and that is what Docs does. Here it cannot be
		// unconditional: three of the five sections put a list in the body and
		// up/down has to drive that. So up/down moves the rail only in the two
		// sections with no list, which leaves all four arrows doing something
		// everywhere without ever changing what a key already meant. The
		// non-rail cases deliberately fall out of the switch rather than
		// returning, so the section dispatch below still sees the key.
		railUpDown := m.section == secOverview || m.section == secDotfiles
		switch msg.String() {
		case "l", "right":
			m.section = (m.section + 1) % numSections
			return m, nil
		case "h", "left":
			m.section = (m.section + numSections - 1) % numSections
			return m, nil
		case "j", "down":
			if railUpDown {
				m.section = (m.section + 1) % numSections
				return m, nil
			}
		case "k", "up":
			if railUpDown {
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
		case secProjects:
			return m.updateProjectsKey(msg)
		case secMachines:
			return m.updateMachinesKey(msg)
		}
	}
	return m, nil
}

// updateOverviewKey is deliberately narrow: this section only ever reports
// on the machine, it never acts on it, so "r" (refresh) is the only key it
// handles.
func (m manageModel) updateOverviewKey(msg tea.KeyMsg) (manageModel, tea.Cmd) {
	switch msg.String() {
	case "r":
		m.ovLoading = true
		return m, fetchOverviewInfo()
	}
	return m, nil
}

func (m manageModel) updateDotfilesKey(msg tea.KeyMsg) (manageModel, tea.Cmd) {
	switch msg.String() {
	case "u":
		if m.repo == "" {
			m.dfMsg = "no checkout to update"
			return m, nil
		}
		// Both steps in one action, so the overlay shows the whole update
		// rather than half of it. This is the only place a shell string is
		// used, and deliberately: the pull has to gate the relink, and there
		// is nothing to relink if the pull failed. The path is a resolved
		// local directory, not input, and it is quoted regardless.
		spec := actionSpec{
			Title: "Update dotfiles",
			// Naming the remote and branch explicitly rather than relying on
			// `git pull --ff-only` alone: without upstream tracking that fails
			// with a wall of git's own help text, which is exactly what it did
			// on a repo whose remote had been re-added by hand.
			Argv: []string{"sh", "-c",
				"cd " + shellQuote(m.repo) +
					` && git pull --ff-only origin "$(git symbolic-ref --short HEAD)"` +
					" && ./install.sh"},
			Confirm: "Pull the latest dotfiles and relink configs?",
			Timeout: 10 * time.Minute,
		}
		return m, func() tea.Msg { return runActionMsg{spec: spec} }
	case "L":
		if m.repo == "" {
			m.dfMsg = "no checkout to relink"
			return m, nil
		}
		spec := actionSpec{
			Title:   "Relink configs",
			Argv:    []string{filepath.Join(m.repo, "install.sh")},
			Confirm: "Re-run install.sh and relink every config?",
		}
		return m, func() tea.Msg { return runActionMsg{spec: spec} }
	case "p":
		spec := actionSpec{
			Title:   "Restore nvim plugins",
			Argv:    []string{"nvim", "--headless", "+Lazy! restore", "+qa"},
			Confirm: "Restore nvim plugins to match lazy-lock.json?",
			Timeout: 20 * time.Minute,
		}
		return m, func() tea.Msg { return runActionMsg{spec: spec} }
	case "t":
		if m.repo == "" {
			m.dfMsg = "no repo found"
			return m, nil
		}
		spec := actionSpec{
			Title:   "Install/repair TPM",
			Argv:    []string{filepath.Join(m.repo, "install.sh")},
			Dir:     m.repo,
			Confirm: "Run install.sh to install/repair TPM (clones it when absent)?",
			Timeout: 10 * time.Minute,
		}
		return m, func() tea.Msg { return runActionMsg{spec: spec} }
	case "D":
		if m.repo == "" {
			m.dfMsg = "no repo found"
			return m, nil
		}
		spec := actionSpec{
			Title:   "Install missing deps",
			Argv:    []string{"sh", filepath.Join(m.repo, "bootstrap.sh"), "--deps"},
			Dir:     m.repo,
			Confirm: "Install missing dependencies via bootstrap.sh --deps?",
			Timeout: 20 * time.Minute,
		}
		return m, func() tea.Msg { return runActionMsg{spec: spec} }
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
	spec, ok := serviceAction(vis[m.svcCursor], verb)
	if !ok {
		// Saying why is better than a key that appears to do nothing.
		m.svcMsg = verb + " is not available for " + svcSourceName(vis[m.svcCursor].Source) +
			" units of this kind"
		return m, nil
	}
	m.svcMsg = ""
	return m, func() tea.Msg { return runActionMsg{spec: spec} }
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
			spec := actionSpec{
				Title: "Remote doctor: " + alias,
				Argv: []string{"ssh",
					"-o", "BatchMode=yes", "-o", "ConnectTimeout=4",
					alias, remoteDots("doctor")},
				// Read-only, but it still opens a connection to another
				// machine. Every other key in this app asks first, and a
				// keystroke that reaches out over the network without asking
				// is a surprise even when it changes nothing.
				Confirm: "Run doctor on " + alias + " over SSH?",
				Timeout: 20 * time.Second,
			}
			return m, func() tea.Msg { return runActionMsg{spec: spec} }
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

type sshHost struct {
	alias    string
	hostname string
}

// parseSSHConfig reads ~/.ssh/config Host entries, excluding wildcard
// patterns and anything without a HostName — those aren't reachable targets,
// they're templates.
func parseSSHConfig() []sshHost {
	var hosts []sshHost
	var cur *sshHost
	var skip bool

	flush := func() {
		if cur != nil && !skip && cur.hostname != "" {
			hosts = append(hosts, *cur)
		}
		cur = nil
		skip = false
	}

	// Shared with sshHosts() in sync.go via sshConfigLines, so the two cannot
	// disagree about which machines exist — they already differed on which
	// wildcard characters disqualify an alias.
	for _, line := range sshConfigLines() {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "host":
			flush()
			aliases := fields[1:]
			for _, a := range aliases {
				if strings.ContainsAny(a, "*?") {
					skip = true
				}
			}
			if !skip {
				cur = &sshHost{alias: aliases[0]}
			}
		case "hostname":
			if cur != nil {
				cur.hostname = fields[1]
			}
		}
	}
	flush()
	return hosts
}

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

	content := contentColumn(contentW, m.h,
		paneHeader("Manage", sectionNames[m.section], summary, measureFor(contentW)),
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

// viewOverview is read-only: every field either comes straight from
// overviewInfo, or — for repo/tools/services — from state another section
// already fetched, so this never re-shells git and never re-scans services.
func (m manageModel) viewOverview(spin string) string {
	if m.ovLoading {
		return "\n  " + spin + styPending.Render(" gathering machine info")
	}

	measure := measureFor(m.w - railWidth - 1)
	const keyW = 10
	valW := measure - keyW
	if valW < 4 {
		valW = 4
	}

	info := m.ovInfo
	var b strings.Builder

	host := info.host
	if host == "" {
		host = "unknown"
	}
	b.WriteString(statRow("host", styValue.Render(truncate(host, valW)), keyW) + "\n")

	osName := info.osName
	if osName == "" {
		osName = runtime.GOOS
	}
	b.WriteString(statRow("os", styValue.Render(truncate(osName, valW)), keyW) + "\n")

	b.WriteString(statRow("arch", styValue.Render(info.arch), keyW) + "\n")

	profile := "full"
	if info.profileLight {
		profile = "light"
	}
	b.WriteString(statRow("profile", styValue.Render(profile), keyW) + "\n")

	dots := info.version
	if dots == "" {
		dots = "dev"
	}
	if info.distSource != "" {
		dots += " (" + info.distSource + ")"
	}
	b.WriteString(statRow("dots", styValue.Render(truncate(dots, valW)), keyW) + "\n")

	b.WriteString(statRow("repo", m.overviewRepoLine(valW), keyW) + "\n")

	b.WriteString(statRow("tools", countSummary(info.toolsHave, info.toolsTotal, "present"), keyW) + "\n")

	b.WriteString(statRow("services", m.overviewServicesLine(), keyW) + "\n")

	if info.memKnown {
		mem := fmt.Sprintf("%s free / %s", formatBytes(info.memFreeBytes), formatBytes(info.memTotalBytes))
		b.WriteString(statRow("memory", styValue.Render(truncate(mem, valW)), keyW) + "\n")
	} else {
		b.WriteString(statRow("memory", styMuted.Render("unknown"), keyW) + "\n")
	}

	if info.diskKnown {
		disk := fmt.Sprintf("%s free / %s", formatBytes(info.diskFreeBytes), formatBytes(info.diskTotalBytes))
		b.WriteString(statRow("disk", styValue.Render(truncate(disk, valW)), keyW) + "\n")
	} else {
		b.WriteString(statRow("disk", styMuted.Render("unknown"), keyW) + "\n")
	}

	if info.uptimeKnown {
		b.WriteString(statRow("uptime", styValue.Render(formatDuration(info.uptime)), keyW) + "\n")
	} else {
		b.WriteString(statRow("uptime", styMuted.Render("unknown"), keyW) + "\n")
	}

	return b.String()
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

func (m manageModel) viewDotfiles() string {
	if m.dfLoading {
		return styPending.Render("checking…")
	}
	var b strings.Builder

	repo := m.repo
	if repo == "" {
		repo = styMuted.Render("not found")
	} else {
		repo = styValue.Render(truncate(repo, m.w-14))
	}
	b.WriteString(styKey.Render("repo    ") + repo + "\n")

	branch := m.dfInfo.branch
	if branch == "" {
		b.WriteString(styKey.Render("branch  ") + styMuted.Render("unknown") + "\n")
	} else {
		b.WriteString(styKey.Render("branch  ") + styValue.Render(branch) + "\n")
	}

	sha := m.dfInfo.sha
	if sha == "" {
		b.WriteString(styKey.Render("sha     ") + styMuted.Render("unknown") + "\n")
	} else {
		b.WriteString(styKey.Render("sha     ") + styValue.Render(sha) + "\n")
	}

	if !m.dfInfo.dirtyKnown {
		b.WriteString(styKey.Render("status  ") + styMuted.Render("unknown") + "\n")
	} else if m.dfInfo.dirty {
		b.WriteString(styKey.Render("status  ") + styBad.Render("dirty") + "\n")
	} else {
		b.WriteString(styKey.Render("status  ") + styOK.Render("clean") + "\n")
	}

	switch {
	case m.dfInfo.behind < 0:
		b.WriteString(styKey.Render("behind  ") + styMuted.Render("unknown") + "\n")
	case m.dfInfo.behind == 0:
		b.WriteString(styKey.Render("behind  ") + styOK.Render("up to date") + "\n")
	default:
		b.WriteString(styKey.Render("behind  ") + styPending.Render(fmt.Sprintf("%d commit(s)", m.dfInfo.behind)) + "\n")
	}

	b.WriteString("\n")
	if m.dfMsg != "" {
		b.WriteString(styMuted.Render(m.dfMsg))
	}
	return b.String()
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

// shellQuote wraps a value in single quotes for `sh -c`, escaping any single
// quote inside it. Used for exactly one command; everything else passes argv
// directly and never goes near a shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
