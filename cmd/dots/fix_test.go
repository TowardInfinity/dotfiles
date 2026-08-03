package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

// A oneshot unit at active/exited is not running.
func TestSystemdExitedIsNotRunning(t *testing.T) {
	out := `apparmor.service        loaded active   exited  Load AppArmor profiles
cron.service            loaded active   running Regular background program
apport.service          loaded inactive dead    crash reports
broken.service          loaded failed   failed  Something broke
`
	svcs := parseSystemdUnits(out, false)
	got := map[string]service{}
	for _, s := range svcs {
		got[s.ID] = s
	}
	if got["apparmor.service"].Running {
		t.Error("active/exited reported as running")
	}
	if !got["cron.service"].Running {
		t.Error("active/running not reported as running")
	}
	if got["apport.service"].Running {
		t.Error("inactive/dead reported as running")
	}
	if d := got["broken.service"].Detail; d != "failed" {
		t.Errorf("failed unit detail = %q, want %q", d, "failed")
	}
}

// The tmux hint must stay complete — it exists to be retyped.
func TestTmuxHintNotTruncated(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 118, Height: 30})
	tm, _ = tm.Update(fetchProjectsInfo()())
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}) // Dotfiles
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}) // Services
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}) // Projects

	var cmd tea.Cmd
	tm, cmd = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			tm, _ = tm.Update(msg)
		}
	}
	hint := tm.(model).man.projTmuxHint
	if hint == "" {
		t.Skip("no projects discovered here")
	}
	t.Logf("hint: %s", hint)
	if strings.Contains(hint, "…") {
		t.Errorf("hint was truncated: %s", hint)
	}
	// Check the hint's own lines, not the whole screen — an ellipsis elsewhere
	// (a "… N more" row, a clipped column) is unrelated.
	var hintLines []string
	for _, ln := range strings.Split(tm.View(), "\n") {
		if strings.Contains(ln, "tmux new-session") || strings.Contains(ln, "Codes/") {
			hintLines = append(hintLines, ln)
		}
	}
	for _, ln := range hintLines {
		if strings.Contains(ln, "…") {
			t.Errorf("rendered hint line is truncated: %q", strings.TrimSpace(ln))
		}
	}
	if len(hintLines) == 0 {
		t.Error("hint did not render at all")
	}
}
