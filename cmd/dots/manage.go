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
type serviceActionDoneMsg struct {
	ok  bool
	msg string
}
type projectsInfoMsg struct{ projects []projectInfo }
type machinesInfoMsg struct{ machines []machineInfo }
type machineDoctorMsg struct {
	alias  string
	output string
	err    error
}

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
	svcBusy    bool
	svcMsg     string

	projects     []projectInfo
	projLoading  bool
	projCursor   int
	projSelected int

	machines       []machineInfo
	machLoading    bool
	machCursor     int
	machDoctorHost string
	machDoctorOut  string
	machDoctorBusy bool
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

	case serviceActionDoneMsg:
		m.svcBusy = false
		m.svcMsg = msg.msg
		return m, fetchServicesInfo()

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

	case machineDoctorMsg:
		m.machDoctorBusy = false
		m.machDoctorHost = msg.alias
		if msg.err != nil {
			out := msg.output
			if out != "" {
				out += "\n"
			}
			m.machDoctorOut = out + "ssh failed: " + msg.err.Error()
		} else {
			m.machDoctorOut = msg.output
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
		m.dfBusy = true
		m.dfMsg = "pulling and relinking…"
		return m, runDotfilesUpdate(m.repo)
	case "L":
		m.dfBusy = true
		m.dfMsg = "relinking…"
		return m, runRelinkOnly(m.repo)
	}
	return m, nil
}

func (m manageModel) updateServicesKey(msg tea.KeyMsg) (manageModel, tea.Cmd) {
	if m.svcBusy {
		return m, nil
	}
	switch msg.String() {
	case "s":
		m.svcBusy = true
		m.svcMsg = "starting…"
		return m, runServiceAction("load")
	case "x":
		m.svcBusy = true
		m.svcMsg = "stopping…"
		return m, runServiceAction("unload")
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
		}
	}
	return m, nil
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
		if !m.machDoctorBusy && m.machCursor < len(m.machines) {
			alias := m.machines[m.machCursor].alias
			m.machDoctorBusy = true
			m.machDoctorHost = alias
			m.machDoctorOut = ""
			return m, runRemoteDoctor(alias)
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

// runDotfilesUpdate ports bin/dots' update(): git pull --ff-only, then
// install.sh to relink. --ff-only is the only state-changing git call this
// program ever makes, and only on this explicit keypress.
func runDotfilesUpdate(repo string) tea.Cmd {
	return func() tea.Msg {
		if repo == "" {
			return dotfilesActionDoneMsg{ok: false, msg: "no repo found"}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if out, err := runGit(ctx, repo, "pull", "--ff-only"); err != nil {
			return dotfilesActionDoneMsg{ok: false, msg: "pull failed: " + firstLine(out, err)}
		}
		return relink(repo, "updated and relinked")
	}
}

func runRelinkOnly(repo string) tea.Cmd {
	return func() tea.Msg {
		if repo == "" {
			return dotfilesActionDoneMsg{ok: false, msg: "no repo found"}
		}
		return relink(repo, "relinked")
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

// runServiceAction starts or stops open-webui via launchctl. macOS only —
// Linux reports "not managed here" rather than pretending to control it.
func runServiceAction(action string) tea.Cmd {
	return func() tea.Msg {
		if runtime.GOOS != "darwin" {
			return serviceActionDoneMsg{ok: false, msg: "not managed here"}
		}
		plist := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", "com.towardinfinity.open-webui.plist")
		if _, err := os.Stat(plist); err != nil {
			return serviceActionDoneMsg{ok: false, msg: "plist not found: " + plist}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "launchctl", action, plist).CombinedOutput()
		if err != nil {
			return serviceActionDoneMsg{ok: false, msg: firstLine(string(out), err)}
		}
		verb := "started"
		if action == "unload" {
			verb = "stopped"
		}
		return serviceActionDoneMsg{ok: true, msg: "open-webui " + verb}
	}
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

func runRemoteDoctor(alias string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ssh",
			"-o", "BatchMode=yes", "-o", "ConnectTimeout=4",
			alias, "dots doctor").CombinedOutput()
		return machineDoctorMsg{alias: alias, output: tailLines(string(out), 12), err: err}
	}
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
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
	if m.svcBusy {
		b.WriteString(styPending.Render(m.svcMsg))
	} else if m.svcMsg != "" {
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

	if m.machDoctorBusy {
		b.WriteString("\n" + styPending.Render("running dots doctor on "+m.machDoctorHost+"…") + "\n")
	} else if m.machDoctorHost != "" {
		b.WriteString("\n" + styMuted.Render(m.machDoctorHost+":") + "\n")
		for _, line := range strings.Split(m.machDoctorOut, "\n") {
			b.WriteString("  " + truncate(line, m.w-6) + "\n")
		}
	}
	return b.String()
}

func (m manageModel) help() string {
	const nav = "h/l section"
	switch m.section {
	case secDotfiles:
		return nav + "  ·  u update  ·  L relink"
	case secServices:
		return nav + "  ·  s start  ·  x stop"
	case secProjects:
		return nav + "  ·  j/k move  ·  enter select"
	case secMachines:
		return nav + "  ·  j/k move  ·  d remote doctor"
	}
	return nav
}
