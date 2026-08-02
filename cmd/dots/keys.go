package main

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// Key bindings as data rather than a hand-concatenated hint string.
//
// The old status line was a string built by each pane and then TRUNCATED to
// stop it wrapping — because when it did wrap it made the status bar three
// lines instead of two and pushed the tab bar off the top of the screen. That
// truncation was a workaround for a layout bug caused by the same string.
// bubbles/help renders to a width by design, so the whole problem disappears.

type keyMap struct {
	bindings []key.Binding
}

func (k keyMap) ShortHelp() []key.Binding { return k.bindings }
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.bindings}
}

func bind(keys, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys), key.WithHelp(keys, desc))
}

// Global keys, appended to every pane's own set.
var globalKeys = []key.Binding{
	bind("tab", "switch"),
	bind("q", "quit"),
}

func newHelp() help.Model {
	h := help.New()
	h.ShowAll = false
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(cViolet)
	h.Styles.ShortDesc = lipgloss.NewStyle().Foreground(cFaint)
	h.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(cLine)
	h.Styles.Ellipsis = lipgloss.NewStyle().Foreground(cFaint)
	return h
}

// ── per-pane bindings ────────────────────────────────────────

func (m docsModel) keys() []key.Binding {
	if m.filtering {
		return []key.Binding{
			bind("enter", "keep"),
			bind("esc", "clear"),
		}
	}
	return []key.Binding{
		bind("j/k", "page"),
		bind("/", "filter"),
		bind("d/u", "scroll"),
	}
}

func (m doctorModel) keys() []key.Binding {
	ks := []key.Binding{bind("r", "recheck")}
	// Only offer the install key when there is something to install; a key
	// listed but inert is worse than one that is absent.
	for _, c := range m.checks {
		if c.state == checkBad {
			ks = append(ks, bind("i", "install missing"))
			break
		}
	}
	return ks
}

func (m manageModel) keys() []key.Binding {
	nav := bind("h/l", "section")
	switch m.section {
	case secDotfiles:
		return []key.Binding{nav,
			bind("u", "update"), bind("L", "relink"),
			bind("p", "plugins"), bind("t", "tpm"), bind("D", "deps"),
		}
	case secServices:
		if m.svcFiltering {
			return []key.Binding{bind("enter", "keep"), bind("esc", "clear")}
		}
		return []key.Binding{nav,
			bind("j/k", "move"), bind("/", "filter"),
			bind("s", "start"), bind("x", "stop"), bind("R", "restart"),
			bind("r", "rescan"),
		}
	case secProjects:
		return []key.Binding{nav, bind("j/k", "move"), bind("enter", "tmux")}
	case secMachines:
		return []key.Binding{nav, bind("j/k", "move"), bind("d", "remote doctor")}
	}
	return []key.Binding{nav}
}
