package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
		tm, _ = tm.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
	}
	if tm.(model).docs.vp.AtBottom() {
		t.Fatal("the longest page fits in 14 rows? the test setup is wrong")
	}

	before := tm.(model).docs.yOffset()
	for i := 0; i < 4; i++ {
		tm, _ = tm.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}
	after := tm.(model).docs.yOffset()
	if after <= before {
		t.Errorf("wheel down did not scroll: offset %d -> %d", before, after)
	}

	for i := 0; i < 10; i++ {
		tm, _ = tm.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	}
	if got := tm.(model).docs.yOffset(); got != 0 {
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
		tm, _ = tm.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
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
		msg  tea.KeyPressMsg
	}{
		{"d", tea.KeyPressMsg{Code: 'd', Text: string('d')}},
		{"space", tea.KeyPressMsg{Code: tea.KeySpace}},
		{"pgdown", tea.KeyPressMsg{Code: tea.KeyPgDown}},
		{"ctrl+f", tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}},
	} {
		m := newModel()
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: 150, Height: 14})
		for i := 0; i < 30; i++ {
			if d := tm.(model).docs.current(); d != nil && d.Name == "update" {
				break
			}
			tm, _ = tm.Update(tea.KeyPressMsg{Code: 'j', Text: string('j')})
		}
		if n := tm.(model).docs.vp.TotalLineCount(); n == 0 {
			t.Fatalf("%s: viewport has no content — rendering is not reaching the model", k.name)
		}
		tm, _ = tm.Update(k.msg)
		if got := tm.(model).docs.yOffset(); got == 0 {
			t.Errorf("%s did not scroll (YOffset still 0)", k.name)
		}
	}
}
