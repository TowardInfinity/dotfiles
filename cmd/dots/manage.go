package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── sections ──────────────────────────────────────────────────

type manageSection int

const (
	secDotfiles manageSection = iota
	secServices
	secProjects
	secMachines
	numSections
)

var sectionNames = [numSections]string{"Dotfiles", "Services", "Projects", "Machines"}

// ── data shapes ───────────────────────────────────────────────

type dotfilesInfo struct {
	sha    string
	branch string
	dirty  bool
	behind int // -1 = unknown (no upstream, or the git call failed)
}

type serviceStatus struct {
	name    string
	present bool
	running bool
	detail  string
}

type servicesInfo struct {
	openWebUI serviceStatus
	ollama    serviceStatus
}

type projectInfo struct {
	name   string
	path   string
	branch string
	dirty  bool
	ahead  int // -1 = unknown
	tmux   bool
}

type machineInfo struct {
	alias     string
	hostname  string
	checked   bool
	reachable bool
}

// ── messages ──────────────────────────────────────────────────

type dotfilesInfoMsg struct{ info dotfilesInfo }
type dotfilesActionDoneMsg struct {
	ok  bool
	msg string
}
type servicesInfoMsg struct{ info servicesInfo }
type projectsInfoMsg struct{ projects []projectInfo }
type machinesInfoMsg struct{ machines []machineInfo }

// ── model ─────────────────────────────────────────────────────

type manageModel struct {
	repo    string
	w, h    int
	section manageSection

	dfInfo    dotfilesInfo
	dfLoading bool
	dfBusy    bool
	dfMsg     string

	svcInfo    servicesInfo
	svcLoading bool
	svcMsg     string

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
	return manageModel{
		repo:         repo,
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
		fetchDotfilesInfo(m.repo),
		fetchServicesInfo(),
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
	case dotfilesInfoMsg:
		m.dfInfo = msg.info
		m.dfLoading = false
		return m, nil

	case dotfilesActionDoneMsg:
		m.dfBusy = false
		m.dfMsg = msg.msg
		if msg.ok {
			return m, fetchDotfilesInfo(m.repo)
		}
		return m, nil

	case servicesInfoMsg:
		m.svcInfo = msg.info
		m.svcLoading = false
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
		switch msg.String() {
		case "l", "right":
			m.section = (m.section + 1) % numSections
			return m, nil
		case "h", "left":
			m.section = (m.section + numSections - 1) % numSections
			return m, nil
		}

		switch m.section {
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

func (m manageModel) updateDotfilesKey(msg tea.KeyMsg) (manageModel, tea.Cmd) {
	if m.dfBusy {
		return m, nil
	}
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
			Title:   "Update dotfiles",
			Argv:    []string{"sh", "-c", "cd " + shellQuote(m.repo) + " && git pull --ff-only && ./install.sh"},
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
	var action string
	switch msg.String() {
	case "s":
		action = "load"
	case "x":
		action = "unload"
	default:
		return m, nil
	}
	spec, note, ok := serviceActionSpec(action)
	if !ok {
		m.svcMsg = note
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
			m = m.openProjectTmux(m.projects[m.projCursor])
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
func (m manageModel) openProjectTmux(p projectInfo) manageModel {
	cmdLine := fmt.Sprintf("tmux new-session -A -s %s -c %s", p.name, p.path)
	if os.Getenv("TMUX") == "" {
		m.projTmuxHint = "run in a terminal: " + cmdLine
		return m
	}
	if err := exec.Command("tmux", "switch-client", "-t", p.name).Run(); err != nil {
		m.projTmuxHint = "no session named " + p.name + " yet — run: " + cmdLine
	} else {
		m.projTmuxHint = "switched to " + p.name
	}
	return m
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
					alias, "dots", "doctor"},
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
			info.dirty = strings.TrimSpace(out) != ""
		}
		if out, err := runGit(ctx, repo, "rev-list", "--count", "HEAD..@{u}"); err == nil {
			if n, cerr := strconv.Atoi(strings.TrimSpace(out)); cerr == nil {
				info.behind = n
			}
		}
		return dotfilesInfoMsg{info: info}
	}
}

func relink(repo, okMsg string) tea.Msg {
	installSh := filepath.Join(repo, "install.sh")
	if _, err := os.Stat(installSh); err != nil {
		return dotfilesActionDoneMsg{ok: false, msg: "install.sh not found"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, installSh)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		return dotfilesActionDoneMsg{ok: false, msg: "install.sh failed: " + firstLine(string(out), err)}
	}
	return dotfilesActionDoneMsg{ok: true, msg: okMsg}
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

func fetchServicesInfo() tea.Cmd {
	return func() tea.Msg {
		var info servicesInfo

		info.openWebUI = serviceStatus{name: "open-webui"}
		conn, err := net.DialTimeout("tcp", "127.0.0.1:11435", 2*time.Second)
		if err == nil {
			conn.Close()
			info.openWebUI.present = true
			info.openWebUI.running = true
			info.openWebUI.detail = "listening on :11435"
		} else {
			info.openWebUI.detail = "not listening on :11435"
		}

		info.ollama = serviceStatus{name: "ollama"}
		if p, ok := have("ollama"); ok {
			info.ollama.present = true
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			out, err := exec.CommandContext(ctx, p, "ps").CombinedOutput()
			cancel()
			if err != nil {
				info.ollama.detail = "not running"
			} else {
				n := 0
				for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					if strings.TrimSpace(line) != "" {
						n++
					}
				}
				if n > 0 {
					n-- // header row
				}
				if n <= 0 {
					info.ollama.detail = "running, no models loaded"
				} else {
					info.ollama.running = true
					info.ollama.detail = fmt.Sprintf("running, %d model(s) loaded", n)
				}
			}
		} else {
			info.ollama.detail = "not installed"
		}

		return servicesInfoMsg{info: info}
	}
}

// serviceActionSpec builds the launchctl load/unload action for open-webui,
// run through the action runner so its output and any error are visible.
// macOS only — Linux (or a missing plist) reports why instead of running
// anything.
func serviceActionSpec(action string) (spec actionSpec, note string, ok bool) {
	if runtime.GOOS != "darwin" {
		return actionSpec{}, "not managed here", false
	}
	plist := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", "com.towardinfinity.open-webui.plist")
	if _, err := os.Stat(plist); err != nil {
		return actionSpec{}, "plist not found: " + plist, false
	}
	verb := "Start"
	if action == "unload" {
		verb = "Stop"
	}
	return actionSpec{
		Title:   "launchctl " + action + " open-webui",
		Argv:    []string{"launchctl", action, plist},
		Confirm: verb + " open-webui via launchctl?",
		Timeout: 30 * time.Second,
	}, "", true
}

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
	path := filepath.Join(os.Getenv("HOME"), ".ssh", "config")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

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

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
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

func (m manageModel) view() string {
	var segs []string
	for i := manageSection(0); i < numSections; i++ {
		if i == m.section {
			segs = append(segs, styTabOn.Render(sectionNames[i]))
		} else {
			segs = append(segs, styTab.Render(sectionNames[i]))
		}
	}
	header := truncate(lipgloss.JoinHorizontal(lipgloss.Top, segs...), m.w-4)

	var body string
	switch m.section {
	case secDotfiles:
		body = m.viewDotfiles()
	case secServices:
		body = m.viewServices()
	case secProjects:
		body = m.viewProjects()
	case secMachines:
		body = m.viewMachines()
	}

	content := header + "\n\n" + body
	return pane(m.w, m.h, styPane.Render(content))
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

	if m.dfInfo.dirty {
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
	if m.dfBusy {
		b.WriteString(styPending.Render(m.dfMsg))
	} else if m.dfMsg != "" {
		b.WriteString(styMuted.Render(m.dfMsg))
	}
	return b.String()
}

func (m manageModel) viewServices() string {
	if m.svcLoading {
		return styPending.Render("checking…")
	}
	var b strings.Builder

	writeSvc := func(s serviceStatus) {
		dot := styMuted.Render("○")
		if s.running {
			dot = styOK.Render("●")
		}
		b.WriteString(dot + "  " + padRight(s.name, 12) + styMuted.Render(truncate(s.detail, m.w-20)) + "\n")
	}
	writeSvc(m.svcInfo.openWebUI)
	writeSvc(m.svcInfo.ollama)

	b.WriteString("\n")
	if runtime.GOOS != "darwin" {
		b.WriteString(styMuted.Render("start/stop: not managed here") + "\n")
	}
	if m.svcMsg != "" {
		b.WriteString(styMuted.Render(m.svcMsg))
	}
	return b.String()
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
		if p.dirty {
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
		b.WriteString("\n" + styMuted.Render(truncate(m.projTmuxHint, m.w-4)) + "\n")
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

func (m manageModel) help() string {
	const nav = "h/l section"
	switch m.section {
	case secDotfiles:
		return nav + "  ·  u update  ·  L relink  ·  p plugins  ·  t tpm  ·  D deps"
	case secServices:
		return nav + "  ·  s start  ·  x stop"
	case secProjects:
		return nav + "  ·  j/k move  ·  enter tmux"
	case secMachines:
		return nav + "  ·  j/k move  ·  d remote doctor"
	}
	return nav
}

// shellQuote wraps a value in single quotes for `sh -c`, escaping any single
// quote inside it. Used for exactly one command; everything else passes argv
// directly and never goes near a shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
