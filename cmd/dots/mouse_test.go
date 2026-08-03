package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The wheel must scroll the docs content. It did nothing before because mouse
// reporting was never requested, so no MouseMsg ever arrived.
func TestWheelScrollsDocs(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 150, Height: 14})

	// Land on a page long enough to scroll.
	for i := 0; i < 30; i++ {
		if d := tm.(model).docs.current(); d != nil && d.Name == "update" {
			break
		}
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	if tm.(model).docs.vp.AtBottom() {
		t.Fatal("the longest page fits in 14 rows? the test setup is wrong")
	}

	before := tm.(model).docs.vp.YOffset
	for i := 0; i < 4; i++ {
		tm, _ = tm.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	}
	after := tm.(model).docs.vp.YOffset
	if after <= before {
		t.Errorf("wheel down did not scroll: offset %d -> %d", before, after)
	}

	for i := 0; i < 10; i++ {
		tm, _ = tm.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	}
	if got := tm.(model).docs.vp.YOffset; got != 0 {
		t.Errorf("wheel up did not return to the top: offset %d", got)
	}
}

// A page with more below must say so, not just show a percentage.
func TestMoreIndicator(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 150, Height: 14})
	for i := 0; i < 30; i++ {
		if d := tm.(model).docs.current(); d != nil && d.Name == "update" {
			break
		}
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	if tm.(model).docs.vp.AtBottom() {
		t.Fatal("the longest page fits in 14 rows? the test setup is wrong")
	}
	if !strings.Contains(tm.(model).docs.viewOutline(), "more") {
		t.Error(`no "more" indicator on a page that scrolls`)
	}
}

// Every documented scroll key must actually move the viewport. All of these
// were silently inert because the model's viewport had no content.
func TestScrollKeysWork(t *testing.T) {
	for _, k := range []struct {
		name string
		msg  tea.KeyMsg
	}{
		{"d", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}},
		{"space", tea.KeyMsg{Type: tea.KeySpace}},
		{"pgdown", tea.KeyMsg{Type: tea.KeyPgDown}},
		{"ctrl+f", tea.KeyMsg{Type: tea.KeyCtrlF}},
	} {
		m := newModel()
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: 150, Height: 14})
		for i := 0; i < 30; i++ {
			if d := tm.(model).docs.current(); d != nil && d.Name == "update" {
				break
			}
			tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		}
		if n := tm.(model).docs.vp.TotalLineCount(); n == 0 {
			t.Fatalf("%s: viewport has no content — rendering is not reaching the model", k.name)
		}
		tm, _ = tm.Update(k.msg)
		if got := tm.(model).docs.vp.YOffset; got == 0 {
			t.Errorf("%s did not scroll (YOffset still 0)", k.name)
		}
	}
}
