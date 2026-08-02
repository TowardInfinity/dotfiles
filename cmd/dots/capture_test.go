package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

// Typing into a filter must not trigger global keys. "q" is a letter people
// type; it is also quit.
func TestFilterCapturesGlobalKeys(t *testing.T) {
	// Services filter.
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 120, Height: 34})
	tm, _ = tm.Update(discoverServices()())
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}) // Services
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})

	for _, r := range "quit" {
		var cmd tea.Cmd
		tm, cmd = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if isQuit(cmd) {
			t.Fatalf("typing %q into the services filter quit the program", r)
		}
	}
	if got := tm.(model).man.svcFilter; got != "quit" {
		t.Errorf("services filter = %q, want %q", got, "quit")
	}
	if tm.(model).tab != tabManage {
		t.Error("typing into the services filter switched tabs")
	}

	// Docs filter, for comparison — this one was already guarded.
	m2 := newModel()
	var tm2 tea.Model = m2
	tm2, _ = tm2.Update(tea.WindowSizeMsg{Width: 120, Height: 34})
	tm2, _ = tm2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "q1" {
		var cmd tea.Cmd
		tm2, cmd = tm2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if isQuit(cmd) {
			t.Fatalf("typing %q into the docs filter quit the program", r)
		}
	}
	if got := tm2.(model).docs.filter; got != "q1" {
		t.Errorf("docs filter = %q, want %q", got, "q1")
	}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	return cmd() == tea.Quit()
}
