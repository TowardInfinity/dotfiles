package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tabID int

const (
	tabDocs tabID = iota
	tabDoctor
	tabManage
	numTabs
)

var tabNames = [numTabs]string{"Docs", "Doctor", "Manage"}

type model struct {
	tab    tabID
	w, h   int
	repo   string
	source string

	docs docsModel
	doc  doctorModel
	man  manageModel

	// Non-nil while an action overlay is up. It takes every key, so a long
	// install cannot be interrupted by a stray tab that switches panes
	// underneath it.
	act *actionModel

	err string
}

func newModel() model {
	repo := findRepo()
	docs, src := loadDocs(repo)
	return model{
		repo:   repo,
		source: src,
		docs:   newDocsModel(docs),
		doc:    newDoctorModel(repo),
		man:    newManageModel(repo),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.doc.Init(), m.man.Init())
}

// contentSize is the area inside the tab bar and status line. Every pane is
// handed the same numbers so nothing has to guess at the chrome.
func (m model) contentSize() (int, int) {
	h := m.h - 2 // tab bar (1 + its border) ... status line is added below
	h -= 2       // status line + its border
	if h < 3 {
		h = 3
	}
	w := m.w
	if w < 20 {
		w = 20
	}
	return w, h
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		w, h := m.contentSize()
		m.docs = m.docs.resize(w, h)
		m.doc = m.doc.resize(w, h)
		m.man = m.man.resize(w, h)
		if m.act != nil {
			a := m.act.resize(w, h)
			m.act = &a
		}
		return m, nil

	case runActionMsg:
		// One at a time. Overwriting a live action orphaned its process: it
		// kept running to completion with its output going nowhere and no way
		// to see or stop it, while the overlay showed the newer one.
		if m.act != nil {
			return m, nil
		}
		w, h := m.contentSize()
		a := newAction(msg.spec, w, h)
		if a.confirm {
			m.act = &a
			return m, nil
		}
		a2, cmd := a.start()
		m.act = &a2
		return m, cmd

	case tea.KeyMsg:
		// The overlay owns the keyboard while it is up.
		if m.act != nil {
			a, cmd, closed := m.act.update(msg)
			if closed {
				m.act = nil
				// Re-check after anything that may have changed the machine,
				// so the pane you return to is not showing a stale answer.
				// Doctor's checks cover installs; services/machines cover
				// the Manage actions that now run through this same overlay.
				// Dotfiles and Projects were missing here, so after `u` pulled
				// new commits the pane still showed the old sha, branch and
				// behind-count — the one place you would actually look to
				// confirm the update landed.
				return m, tea.Batch(
					m.doc.Init(),
					fetchDotfilesInfo(m.repo),
					discoverServices(),
					fetchProjectsInfo(),
					fetchMachinesInfo(),
				)
			}
			m.act = &a
			return m, cmd
		}

		// Let a pane consume the key first when it is capturing text, otherwise
		// typing "q" into the filter would quit the program.
		if m.tab == tabDocs && m.docs.filtering {
			var cmd tea.Cmd
			m.docs, cmd = m.docs.update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab", "]":
			m.tab = (m.tab + 1) % numTabs
			return m, nil
		case "shift+tab", "[":
			m.tab = (m.tab + numTabs - 1) % numTabs
			return m, nil
		case "1":
			m.tab = tabDocs
			return m, nil
		case "2":
			m.tab = tabDoctor
			return m, nil
		case "3":
			m.tab = tabManage
			return m, nil
		}
	}

	if m.act != nil {
		switch msg.(type) {
		case actLineMsg, actDoneMsg:
			a, cmd, closed := m.act.update(msg)
			if closed {
				m.act = nil
			} else {
				m.act = &a
			}
			return m, cmd
		}
	}

	// Broadcast to every pane, not just the visible one.
	//
	// Routing by active tab dropped any async result whose pane was in the
	// background — and Init() starts the doctor check while Docs is in front,
	// so its result was delivered to Docs and discarded. Doctor then sat on
	// "checking…" forever, which is exactly what it did.
	//
	// Panes ignore messages they do not recognise, so a broadcast is cheap and
	// removes a whole class of "this pane never loads" bug rather than fixing
	// one instance of it.
	var cmds []tea.Cmd
	var c tea.Cmd

	m.docs, c = m.docs.update(msg)
	cmds = append(cmds, c)
	m.doc, c = m.doc.update(msg)
	cmds = append(cmds, c)
	m.man, c = m.man.update(msg)
	cmds = append(cmds, c)

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.w == 0 {
		return "starting…"
	}

	var tabs []string
	for i := tabID(0); i < numTabs; i++ {
		label := fmt.Sprintf("%d %s", i+1, tabNames[i])
		if i == m.tab {
			tabs = append(tabs, styTabOn.Render(label))
		} else {
			tabs = append(tabs, styTab.Render(label))
		}
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	bar = styTabBar.Width(m.w).Render(bar)

	var body, help string
	switch m.tab {
	case tabDocs:
		body, help = m.docs.view(), m.docs.help()
	case tabDoctor:
		body, help = m.doc.view(), m.doc.help()
	case tabManage:
		body, help = m.man.view(), m.man.help()
	}

	// Hard clamp. Every pane is handed the same height, but an arithmetic slip
	// inside one of them silently steals a line from the tab bar rather than
	// erroring — so enforce it here as well as computing it correctly there.
	_, ch := m.contentSize()
	body = lipgloss.NewStyle().MaxHeight(ch).Render(body)

	// Truncate rather than let it wrap. Manage's help is the longest, and at 80
	// columns it wrapped to a second line — which made the status bar three
	// lines instead of two and pushed the tab bar off the top. A hint that is
	// cut short is a much smaller problem than losing the header.
	hint := help + "  ·  tab switch  ·  q quit"
	status := styStatus.Width(m.w).Render(
		styHint.Render(truncate(hint, m.w-2)),
	)

	if m.act != nil {
		body = m.act.view()
	}

	return lipgloss.JoinVertical(lipgloss.Left, bar, body, status)
}

// ── shared helpers ───────────────────────────────────────────

// pane wraps content to an exact box, so panes cannot push the layout around
// when their content is taller or wider than expected.
func pane(w, h int, s string) string {
	return lipgloss.NewStyle().Width(w).Height(h).MaxWidth(w).MaxHeight(h).Render(s)
}

func newViewport(w, h int) viewport.Model {
	v := viewport.New(w, h)
	v.YPosition = 0
	return v
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	if w <= 1 || len(r) <= 1 {
		return "…"
	}
	for len(r) > 0 && lipgloss.Width(string(r)+"…") > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

func padRight(s string, w int) string {
	d := w - lipgloss.Width(s)
	if d <= 0 {
		return s
	}
	return s + strings.Repeat(" ", d)
}
