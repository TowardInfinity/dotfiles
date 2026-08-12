package main

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

// Key bindings as data rather than a hand-concatenated hint string.
//
// The old status line was a string built by each pane and then TRUNCATED to
// stop it wrapping — because when it did wrap it made the status bar three
// lines instead of two and pushed the tab bar off the top of the screen. That
// truncation was a workaround for a layout bug caused by the same string.
// bubbles/help renders to a width by design, so the whole problem disappears.

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
		bind("wheel", "scroll"),
	}
}

func (m doctorModel) keys() []key.Binding {
	ks := []key.Binding{bind("r", "recheck")}
	// Only offer the install key when there is something to install; a key
	// listed but inert is worse than one that is absent.
	for _, c := range m.checks {
		if c.state == checkBad && !isConfigCheck(c.name) && !isPackageCheck(c.name) {
			ks = append(ks, bind("i", "install missing"))
			break
		}
	}
	if configRepairable(m.checks) {
		ks = append(ks, bind("c", "repair config"))
	}
	return ks
}

// manageModel's navigation contract, adhered to by every section below:
//
//   - left/right (h/l) is the OUTER axis. It switches the rail section and
//     does nothing else, in every section, always — including ones with no
//     row list to move a cursor through.
//   - up/down (j/k) is the INNER axis. It moves whatever the current section
//     has to move — a row cursor (Services/Packages/Projects/Machines) or a
//     scroll offset over static content (Overview/Dotfiles) — and never
//     switches the section. Where a section truly has nothing to move (an
//     empty list, content that already fits) it is inert rather than bound
//     to a second meaning.
//   - Filtering suspends both. While a filter box owns the keyboard
//     (svcFiltering/pkgFiltering), every key including h/j/k/l goes to the
//     filter — checked before outer-axis dispatch in manageModel.update()
//     and before inner-axis handling in each section's own update*Key.
//
// A key is listed in a section's footer only when it can currently act —
// same rule Doctor's `i` and Machines' `d` already followed before this.
func (m manageModel) keys() []key.Binding {
	nav := bind("h/l", "section")
	switch m.section {
	case secOverview:
		ks := []key.Binding{nav, bind("r", "refresh")}
		if len(m.overviewLines()) > m.bodyRows() {
			ks = append(ks, bind("j/k", "scroll"))
		}
		return ks
	case secDotfiles:
		ks := []key.Binding{nav,
			bind("u", "update"), bind("L", "relink"),
			bind("p", "plugins"), bind("t", "tpm"), bind("D", "deps"),
		}
		if len(m.dotfilesLines()) > m.bodyRows() {
			ks = append(ks, bind("j/k", "scroll"))
		}
		return ks
	case secServices:
		if m.svcFiltering {
			return []key.Binding{bind("enter", "keep"), bind("esc", "clear")}
		}
		return []key.Binding{nav,
			bind("j/k", "move"), bind("/", "filter"),
			bind("s", "start"), bind("x", "stop"), bind("R", "restart"),
			bind("a", "all/running"), bind("r", "rescan"),
		}
	case secPackages:
		if m.pkgFiltering {
			return []key.Binding{bind("enter", "keep"), bind("esc", "clear")}
		}
		ks := []key.Binding{nav, bind("j/k", "move"), bind("/", "filter"), bind("r", "rescan"), bind("s", "sort"), bind("m", "manager")}
		// Same "only advertise a key that can act" rule as below: `u` is
		// worth listing only once the cursor is actually on a row it can do
		// something with (pmGo rows have no upgrade action).
		if vis := m.visiblePackages(); len(vis) > 0 && m.pkgCursor < len(vis) {
			if _, ok := packageAction(vis[m.pkgCursor]); ok {
				ks = append(ks, bind("u", "upgrade"))
			}
		}
		return ks
	// Only advertise a key that can act. A listed key that does nothing is
	// worse than an absent one — the same rule Doctor's `i` already follows.
	case secProjects:
		ks := []key.Binding{nav}
		if len(m.projects) > 0 {
			ks = append(ks, bind("j/k", "move"), bind("enter", "tmux"))
		}
		return ks
	case secMachines:
		ks := []key.Binding{nav}
		if len(m.machines) > 0 {
			ks = append(ks, bind("j/k", "move"), bind("d", "remote doctor"))
		}
		return ks
	}
	return []key.Binding{nav}
}
