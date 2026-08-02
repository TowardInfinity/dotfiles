package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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
	checks  []checkResult
	loading bool
}

func newDoctorModel() doctorModel {
	return doctorModel{
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
		if msg.String() == "r" {
			m.loading = true
			m.checks = pendingChecks()
			return m, runDoctorChecks
		}
	}
	return m, nil
}

func (m doctorModel) view() string {
	osName := "Linux"
	if runtime.GOOS == "darwin" {
		osName = "macOS"
	}

	var b strings.Builder
	b.WriteString(styTitle.Render("Doctor") + styMuted.Render("  ("+osName+")") + "\n\n")

	if m.loading {
		b.WriteString(styPending.Render("checking…") + "\n")
		return pane(m.w, m.h, styPane.Render(b.String()))
	}

	ok := 0
	nameW := 10
	for _, c := range m.checks {
		if len(c.name) > nameW {
			nameW = len(c.name)
		}
	}
	for _, c := range m.checks {
		var mark string
		switch c.state {
		case checkOK:
			mark = styOK.Render("✓")
			ok++
		case checkBad:
			mark = styBad.Render("✗")
		default:
			mark = styPending.Render("…")
		}
		line := mark + "  " + padRight(c.name, nameW)
		if c.path != "" {
			room := m.w - nameW - 8
			line += "  " + styMuted.Render(truncate(c.path, room))
		}
		b.WriteString(truncate(line, m.w-4) + "\n")
	}

	b.WriteString("\n")
	summary := fmt.Sprintf("%d / %d present", ok, len(m.checks))
	if ok == len(m.checks) {
		b.WriteString(styOK.Render(summary))
	} else {
		b.WriteString(styPending.Render(summary))
	}
	b.WriteString("\n")

	return pane(m.w, m.h, styPane.Render(b.String()))
}

func (m doctorModel) help() string {
	if m.loading {
		return "checking…"
	}
	return "r re-run"
}
