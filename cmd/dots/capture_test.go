package main

import (
	tea "charm.land/bubbletea/v2"
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
	tm, _ = tm.Update(tea.KeyPressMsg{Code: '3', Text: string('3')})
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'l', Text: string('l')}) // Dotfiles
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'l', Text: string('l')}) // Services
	tm, _ = tm.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})

	for _, r := range "quit" {
		var cmd tea.Cmd
		tm, cmd = tm.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
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
	tm2, _ = tm2.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})
	for _, r := range "q1" {
		var cmd tea.Cmd
		tm2, cmd = tm2.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
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

// "h"/"l" (and the arrow keys of the same name) are Manage's section-switch
// keys everywhere else — but while a filter box is focused they must be
// typed, or move the cursor within it, not treated as navigation. This was a
// real bug: the top-level nav switch in manageModel.update() ran before
// updateServicesKey/updatePackagesKey's own filtering guards ever saw the
// key, so typing "lazygit" into either filter silently hopped sections
// partway through the word.
func TestFilterCapturesSectionKeys(t *testing.T) {
	// Services: two "l" presses from Overview lands here (Dotfiles, then
	// Services), matching capture_test.go's existing navigation above.
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 120, Height: 34})
	tm, _ = tm.Update(discoverServices()())
	tm, _ = tm.Update(tea.KeyPressMsg{Code: '3', Text: string('3')})
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'l', Text: string('l')}) // Dotfiles
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'l', Text: string('l')}) // Services
	tm, _ = tm.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})

	for _, r := range "lazygit" {
		tm, _ = tm.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := tm.(model).man.section; got != secServices {
		t.Errorf("typing into the services filter changed section to %v", got)
	}
	if got := tm.(model).man.svcFilter; got != "lazygit" {
		t.Errorf("services filter = %q, want %q", got, "lazygit")
	}

	// Arrow-type left/right move the textinput's cursor, not the value — so
	// only assert the section held, not that the text changed.
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := tm.(model).man.section; got != secServices {
		t.Errorf("arrow keys in the services filter changed section to %v", got)
	}
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

	// Packages: one more "l" from Services.
	tm, _ = tm.Update(tea.KeyPressMsg{Code: 'l', Text: string('l')}) // Packages
	tm, _ = tm.Update(tea.KeyPressMsg{Code: '/', Text: string('/')})

	for _, r := range "lazygit" {
		tm, _ = tm.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := tm.(model).man.section; got != secPackages {
		t.Errorf("typing into the packages filter changed section to %v", got)
	}
	if got := tm.(model).man.pkgFilter; got != "lazygit" {
		t.Errorf("packages filter = %q, want %q", got, "lazygit")
	}

	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	tm, _ = tm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := tm.(model).man.section; got != secPackages {
		t.Errorf("arrow keys in the packages filter changed section to %v", got)
	}
}
