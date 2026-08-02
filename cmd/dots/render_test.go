package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Renders every tab at several sizes and asserts nothing panics and nothing
// exceeds the terminal width. Width overflow is the failure mode that wrecks a
// TUI layout, and it only shows up at sizes you did not try by hand.
func TestTabsRenderWithinWidth(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 32}, {200, 50}, {60, 18}}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		m := newModel()
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: h})

		for tab := 0; tab < int(numTabs); tab++ {
			tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune('1' + tab)}})
			out := tm.View()
			if strings.TrimSpace(out) == "" {
				t.Fatalf("tab %d empty at %dx%d", tab, w, h)
			}
			for i, line := range strings.Split(out, "\n") {
				if got := lipgloss.Width(line); got > w {
					t.Errorf("tab %d line %d overflows at %dx%d: %d > %d",
						tab, i, w, h, got, w)
				}
			}
		}
	}
}

// Docs navigation must never land the cursor on a group label or out of range.
func TestDocsNavigation(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 120, Height: 32})

	for i := 0; i < 60; i++ {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		mm := tm.(model)
		if d := mm.docs.current(); d == nil {
			t.Fatalf("cursor landed on no document after %d downs", i+1)
		}
	}
	for i := 0; i < 60; i++ {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		mm := tm.(model)
		if d := mm.docs.current(); d == nil {
			t.Fatalf("cursor landed on no document after %d ups", i+1)
		}
	}
}

// A filter that matches nothing must render, not crash.
func TestDocsFilterNoMatches(t *testing.T) {
	m := newModel()
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "zzzznope" {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if strings.TrimSpace(tm.View()) == "" {
		t.Fatal("empty view for a filter with no matches")
	}
}

// Every page must carry front-matter and have it stripped from the body.
func TestDocsHaveFrontMatter(t *testing.T) {
	docs, _ := loadDocs(findRepo())
	if len(docs) < 20 {
		t.Fatalf("expected the full set of pages, got %d", len(docs))
	}
	for _, d := range docs {
		if d.Group == "" || d.Title == d.Name && d.Order == 999 {
			t.Errorf("%s: missing or unparsed front-matter", d.Name)
		}
		if strings.HasPrefix(strings.TrimSpace(d.Body), "---") {
			t.Errorf("%s: front-matter leaked into the body", d.Name)
		}
		if _, ok := groupOrder[d.Group]; !ok {
			t.Errorf("%s: unknown group %q", d.Name, d.Group)
		}
	}
}
