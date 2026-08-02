package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// checkState is where a single tool check currently stands.
type checkState int

const (
	checkPending checkState = iota
	checkOK
	checkBad
)

// checkResult is one row of the doctor view: a tool (or directory) name, its
// state, and — when found — the resolved path, shown dimmed next to it.
type checkResult struct {
	name  string
	state checkState
	path  string
}

// doctorMsg carries a finished check pass back into the update loop.
type doctorMsg struct {
	results []checkResult
}

type doctorModel struct {
	w, h    int
	repo    string
	checks  []checkResult
	loading bool
	note    string // one-line status for the last "i" press, e.g. "no repo found"
}

func newDoctorModel(repo string) doctorModel {
	return doctorModel{
		repo:    repo,
		checks:  pendingChecks(),
		loading: true,
	}
}

// checkNames mirrors bin/dots' doctor(): the tools the CONFIGS call, not the
// --deps package list — those are different lists, and conflating them is
// exactly the bug that list was written to avoid.
func checkNames() []string {
	need := []string{"zsh", "git", "nvim", "tmux"}
	if runtime.GOOS == "darwin" {
		need = append(need, "bat", "eza", "fzf", "zoxide", "lazygit", "fnm", "uv")
	} else {
		need = append(need, "glow", "go", "uv", "pnpm", "fnm")
	}
	need = append(need, "oh-my-zsh", "tpm")
	return need
}

func pendingChecks() []checkResult {
	names := checkNames()
	out := make([]checkResult, len(names))
	for i, n := range names {
		out[i] = checkResult{name: n, state: checkPending}
	}
	return out
}

func (m doctorModel) Init() tea.Cmd {
	return runDoctorChecks
}

// runDoctorChecks is the tea.Cmd: it does all the stat/PATH work off the
// update loop and reports back as a single message so the UI never blocks.
func runDoctorChecks() tea.Msg {
	names := checkNames()
	out := make([]checkResult, len(names))
	for i, n := range names {
		out[i] = evalCheck(n)
	}
	return doctorMsg{results: out}
}

func evalCheck(name string) checkResult {
	home := os.Getenv("HOME")
	switch name {
	case "oh-my-zsh":
		dir := filepath.Join(home, ".oh-my-zsh")
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return checkResult{name: name, state: checkOK, path: dir}
		}
		return checkResult{name: name, state: checkBad}
	case "tpm":
		dir := filepath.Join(home, ".config", "tmux", "plugins", "tpm", ".git")
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return checkResult{name: name, state: checkOK, path: filepath.Dir(dir)}
		}
		return checkResult{name: name, state: checkBad}
	default:
		if p, ok := have(name); ok {
			return checkResult{name: name, state: checkOK, path: p}
		}
		return checkResult{name: name, state: checkBad}
	}
}

// have mirrors bin/dots' have() (itself mirroring bootstrap.sh): PATH first,
// then these directories directly, because a non-interactive process — this
// one — has not sourced .zshrc, so tools installed by these configs can be
// missing from PATH even though they are very much present. Getting this
// wrong reports installed tools as missing.
func have(bin string) (string, bool) {
	if p, err := exec.LookPath(bin); err == nil {
		return p, true
	}
	home := os.Getenv("HOME")
	dirs := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, ".local", "share", "pnpm"),
		filepath.Join(home, ".local", "share", "fnm"),
		"/usr/local/bin",
		"/usr/local/go/bin",
		"/opt/homebrew/bin",
	}
	for _, d := range dirs {
		p := filepath.Join(d, bin)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

func (m doctorModel) resize(w, h int) doctorModel {
	m.w, m.h = w, h
	return m
}

func (m doctorModel) update(msg tea.Msg) (doctorModel, tea.Cmd) {
	switch msg := msg.(type) {
	case doctorMsg:
		m.checks = msg.results
		m.loading = false
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			m.loading = true
			m.checks = pendingChecks()
			m.note = ""
			return m, runDoctorChecks
		case "i":
			if m.loading {
				return m, nil
			}
			spec, note, ok := m.buildInstall()
			m.note = note
			if !ok {
				return m, nil
			}
			return m, func() tea.Msg { return runActionMsg{spec: spec} }
		}
	}
	return m, nil
}

// brewFormulas maps a check's binary name to the brew formula name, for the
// handful where they differ (bootstrap.sh's install_macos_pkgs is the
// authoritative list). Anything absent from this map uses its own name.
var brewFormulas = map[string]string{
	"nvim": "neovim",
	"rg":   "ripgrep",
}

func brewFormula(check string) string {
	if f, ok := brewFormulas[check]; ok {
		return f
	}
	return check
}

// buildInstall assembles the single action behind the "i" key: install
// everything currently missing. ok is false when there is nothing to do, or
// nothing this pane knows how to do — note then explains why (empty when
// there is simply nothing missing).
func (m doctorModel) buildInstall() (spec actionSpec, note string, ok bool) {
	var missing []string
	for _, c := range m.checks {
		if c.state == checkBad {
			missing = append(missing, c.name)
		}
	}
	if len(missing) == 0 {
		return actionSpec{}, "", false
	}

	// oh-my-zsh and tpm are directories the repo's install.sh creates, not
	// packages a package manager knows about — keep them out of the
	// brew/apt list entirely.
	var dirs, pkgs []string
	for _, n := range missing {
		if n == "oh-my-zsh" || n == "tpm" {
			dirs = append(dirs, n)
		} else {
			pkgs = append(pkgs, n)
		}
	}

	if runtime.GOOS == "darwin" {
		if len(pkgs) == 0 {
			// Only the directories are missing: install.sh is what sets
			// those up, brew has nothing to install.
			if m.repo == "" {
				return actionSpec{}, "no repo found — can't run install.sh for " + strings.Join(dirs, ", "), false
			}
			installSh := filepath.Join(m.repo, "install.sh")
			return actionSpec{
				Title:   "Set up " + strings.Join(dirs, ", "),
				Argv:    []string{installSh},
				Dir:     m.repo,
				Confirm: "Run install.sh to set up " + strings.Join(dirs, ", ") + "?",
				Timeout: 10 * time.Minute,
			}, "", true
		}

		var formulas []string
		seen := map[string]bool{}
		for _, n := range pkgs {
			f := brewFormula(n)
			if !seen[f] {
				seen[f] = true
				formulas = append(formulas, f)
			}
		}
		argv := append([]string{"brew", "install"}, formulas...)
		confirm := fmt.Sprintf("Install %d package(s) with brew: %s?", len(formulas), strings.Join(formulas, " "))
		if len(dirs) > 0 {
			confirm += " (" + strings.Join(dirs, ", ") + " still need install.sh — Manage > Dotfiles)"
		}
		return actionSpec{
			Title:   "Install missing tools",
			Argv:    argv,
			Confirm: confirm,
			Timeout: 15 * time.Minute,
		}, "", true
	}

	// Linux: several of these (glow, go, uv, pnpm, fnm) are not apt packages
	// at all here — they come from tarballs/scripts inside bootstrap.sh, and
	// that same run also links the configs (creating tpm) and always
	// installs oh-my-zsh. Re-running it is simpler and more correct than
	// reimplementing that split here.
	if m.repo == "" {
		return actionSpec{}, "no repo found — can't run bootstrap.sh --deps", false
	}
	installer := filepath.Join(m.repo, "bootstrap.sh")
	if _, err := os.Stat(installer); err != nil {
		return actionSpec{}, "bootstrap.sh not found in repo", false
	}
	return actionSpec{
		Title:   "Install missing dependencies",
		Argv:    []string{"sh", installer, "--deps"},
		Dir:     m.repo,
		Confirm: fmt.Sprintf("Install missing (%s) via bootstrap.sh --deps?", strings.Join(missing, ", ")),
		Timeout: 20 * time.Minute,
	}, "", true
}

// checkGroup labels a run of related checks, so the list reads as three short
// sections rather than one undifferentiated column of thirteen.
func checkGroup(name string) string {
	switch name {
	case "zsh", "git", "nvim", "tmux":
		return "Core"
	case "oh-my-zsh", "tpm":
		return "Frameworks"
	default:
		return "Tools"
	}
}

func (m doctorModel) view(spin string) string {
	osName := "Linux"
	if runtime.GOOS == "darwin" {
		osName = "macOS"
	}
	measure := measureFor(m.w)

	if m.loading {
		return contentColumn(m.w, m.h,
			paneHeader("Doctor", osName, "checking what the configs call…", measure),
			"\n  "+spin+styPending.Render(" checking"))
	}

	ok := 0
	for _, c := range m.checks {
		if c.state == checkOK {
			ok++
		}
	}

	// A real table rather than padded columns. The resolved paths vary wildly
	// in length, and hand-padding drifted out of alignment the moment one of
	// them changed; lipgloss measures the columns itself.
	rows := make([][]string, 0, len(m.checks))
	lastGroup := ""
	for _, c := range m.checks {
		g := checkGroup(c.name)
		label := ""
		if g != lastGroup {
			label = strings.ToUpper(g)
			lastGroup = g
		}
		where := c.path
		if where == "" && c.state == checkBad {
			where = "not found"
		}
		rows = append(rows, []string{
			label,
			stateDot(c.state == checkOK, c.state != checkPending) + " " + c.name,
			where,
		})
	}

	body := dataTable([]string{"", "TOOL", "WHERE"}, rows, -1, measure) + "\n"

	if ok == len(m.checks) {
		body += " " + styOK.Render("Everything the configs call is installed.")
	} else {
		body += " " + styPending.Render("i") + styMuted.Render(" installs the missing ones")
	}
	if m.note != "" {
		body += "\n " + styMuted.Render(truncate(m.note, measure))
	}

	return contentColumn(m.w, m.h,
		paneHeader("Doctor", osName, countSummary(ok, len(m.checks), "present"), measure),
		body)
}

func (m doctorModel) help() string {
	if m.loading {
		return "checking…"
	}
	return "r re-run  ·  i install missing"
}
