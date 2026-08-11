package main

// Deprecated compatibility model. runTUI uses shellModel; this three-tab
// implementation remains only so the pre-redesign interaction tests continue
// to exercise their historical contracts while the migration is rolled out.

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
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

	// Whether this checkout is ahead of the other machines. Read once at
	// startup and rendered as a status-bar badge; View runs per keystroke and
	// must not shell out to git.
	sync repoState

	// Non-nil while an action overlay is up. It takes every key, so a long
	// install cannot be interrupted by a stray tab that switches panes
	// underneath it.
	act *actionModel

	// One spinner for the whole app rather than one per pane. Each pane
	// keeping its own would start a separate tick loop, and with messages now
	// broadcast to every pane that means several overlapping animations
	// driving redraws. The root ticks once and hands the current frame down.
	sp spinner.Model
	hp help.Model

	err string
}

type repoStateMsg struct{ state repoState }

func newModel() model {
	repo := findRepo()
	docs, src := loadDocs(repo)
	sp := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(styPending),
	)
	return model{
		sp:     sp,
		hp:     newHelp(),
		repo:   repo,
		source: src,
		sync:   readRepoState(repo),
		docs:   newDocsModel(docs),
		doc:    newDoctorModel(repo),
		man:    newManageModel(repo),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.doc.Init(), m.man.Init(), m.sp.Tick)
}

// capturingInput reports whether the visible pane is taking free text, in
// which case no key may be treated as a shortcut.
func (m model) capturingInput() bool {
	switch m.tab {
	case tabDocs:
		return m.docs.filtering
	case tabManage:
		return m.man.filtering()
	}
	return false
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
		m.hp.SetWidth(m.w - 2)
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
		a := newAction(msg.plan, w, h)
		if a.confirm {
			m.act = &a
			return m, nil
		}
		a2, cmd := a.start()
		m.act = &a2
		return m, cmd

	case actionPlanErrorMsg:
		m.err = msg.err.Error()
		return m, nil

	case repoStateMsg:
		m.sync = msg.state
		return m, nil

	case tea.KeyPressMsg:
		// The overlay owns the keyboard while it is up.
		if m.act != nil {
			a, cmd, closed := m.act.update(msg)
			if closed {
				result := a.result
				m.act = nil
				return m, m.refreshAffected(result.Affects)
			}
			m.act = &a
			return m, cmd
		}
		m.err = ""

		// A pane capturing text owns every key, including the global ones.
		//
		// This was special-cased for the Docs filter only, so typing "q" into
		// the Services filter quit the program and "1"/"2"/"3" switched tabs
		// mid-word. Asking the pane whether it is capturing covers both, and
		// covers the next input field without anyone remembering to.
		if m.capturingInput() {
			var cmd tea.Cmd
			switch m.tab {
			case tabDocs:
				m.docs, cmd = m.docs.update(msg)
			case tabDoctor:
				m.doc, cmd = m.doc.update(msg)
			case tabManage:
				m.man, cmd = m.man.update(msg)
			}
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

		// Keys go to the VISIBLE pane only.
		//
		// The broadcast below exists so async results reach a pane that is not
		// in front. Letting key presses take the same path meant every pane saw
		// every key: `u` in Docs is "scroll up", but Manage binds `u` to
		// "update dotfiles", so reading the docs started a git pull. `i`, `s`,
		// `x` and `D` leaked the same way. Async data does not care which tab
		// is showing; input very much does.
		var kc tea.Cmd
		switch m.tab {
		case tabDocs:
			m.docs, kc = m.docs.update(msg)
		case tabDoctor:
			m.doc, kc = m.doc.update(msg)
		case tabManage:
			m.man, kc = m.man.update(msg)
		}
		return m, kc
	}

	// Mouse is input, so it goes where keys go: the visible pane, not every
	// pane. Broadcasting it would have three panes reacting to one wheel notch.
	if _, ok := msg.(tea.MouseMsg); ok {
		if m.act != nil {
			return m, nil // the overlay owns input while it is up
		}
		var c tea.Cmd
		switch m.tab {
		case tabDocs:
			m.docs, c = m.docs.update(msg)
		case tabDoctor:
			m.doc, c = m.doc.update(msg)
		case tabManage:
			m.man, c = m.man.update(msg)
		}
		return m, c
	}

	if _, ok := msg.(spinner.TickMsg); ok {
		var c tea.Cmd
		m.sp, c = m.sp.Update(msg)
		return m, c
	}

	if m.act != nil {
		switch msg.(type) {
		case actLineMsg, actDoneMsg, actStreamClosedMsg:
			a, cmd, closed := m.act.update(msg)
			if closed {
				m.act = nil
			} else {
				m.act = &a
			}
			return m, cmd
		}
	}

	// Broadcast to every pane, not just the visible one. Data only — key
	// presses are handled above and never reach here.
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

// refreshAffected is the first consumer of operation metadata: an action that
// changes one service no longer triggers package, project, machine and repo
// probes. Overview is a summary, so any successful mutation refreshes it too.
func (m model) refreshAffected(resources []ops.ResourceID) tea.Cmd {
	if len(resources) == 0 {
		return nil
	}
	seen := make(map[ops.ResourceID]bool, len(resources))
	cmds := []tea.Cmd{fetchOverviewInfo()}
	doctorAdded := false
	for _, resource := range resources {
		if seen[resource] {
			continue
		}
		seen[resource] = true
		switch resource {
		case resourceDoctor, resourceConfig:
			if !doctorAdded {
				cmds = append(cmds, m.doc.Init())
				doctorAdded = true
			}
		case resourceRepo:
			cmds = append(cmds, fetchDotfilesInfo(m.repo), fetchRepoState(m.repo))
		case resourceServices:
			cmds = append(cmds, discoverServices())
		case resourcePackages:
			cmds = append(cmds, discoverPackages())
		case resourceMachines:
			cmds = append(cmds, fetchMachinesInfo())
		}
	}
	return tea.Batch(cmds...)
}

func fetchRepoState(repo string) tea.Cmd {
	return func() tea.Msg { return repoStateMsg{state: readRepoState(repo)} }
}

func (m model) View() tea.View {
	if m.w == 0 {
		return tea.NewView("starting…")
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

	var body string
	var ks []key.Binding
	switch m.tab {
	case tabDocs:
		body, ks = m.docs.view(), m.docs.keys()
	case tabDoctor:
		body, ks = m.doc.view(m.sp.View()), m.doc.keys()
	case tabManage:
		body, ks = m.man.view(m.sp.View()), m.man.keys()
	}

	// Hard clamp. Every pane is handed the same height, but an arithmetic slip
	// inside one of them silently steals a line from the tab bar rather than
	// erroring — so enforce it here as well as computing it correctly there.
	_, ch := m.contentSize()
	body = lipgloss.NewStyle().MaxHeight(ch).Render(body)

	// help renders to its assigned width and elides what does not fit, so the
	// status bar is always exactly one line. No truncation hack, and no way for
	// a long key list to steal the tab bar's row.
	//
	// Width is set here rather than on resize: View is the only place the width
	// is certainly current, and a stale zero here means no eliding at all —
	// which put Manage's key list on two lines and cost the tab bar its row
	// again, the very thing help was brought in to prevent.
	// A right-aligned badge when this checkout is ahead of the other machines.
	// It is computed once at startup, not per frame: it shells out to git, and
	// View runs on every keystroke.
	//
	// Its width comes out of the help's budget *before* help renders, so the
	// elision below still has the whole story and the bar cannot reach two
	// lines — the failure this row has already had twice. Below 40 columns the
	// badge is dropped entirely rather than squeezing the key hints to nothing.
	badge := ""
	if m.sync.needsSync() && m.w >= 40 {
		badge = styPending.Render("● " + m.sync.summary() + " — dots publish")
	}

	hintW := m.w - 2 - lipgloss.Width(badge)

	hp := m.hp
	hp.SetWidth(hintW)
	hint := hp.ShortHelpView(append(append([]key.Binding{}, ks...), globalKeys...))
	if m.err != "" {
		hint = styBad.Render("error: " + m.err)
	}
	if badge != "" {
		hint = padRight(truncate(hint, hintW), hintW) + badge
	}

	// Backstop, and not a redundant one. bubbles' shouldAddItem only stops
	// early if the ellipsis itself still fits: when it does not, the branch
	// falls through and the item is appended anyway. So at narrow widths the
	// "eliding" help can still overrun, wrap to two lines, and take the tab
	// bar's row with it. Clip it.
	status := styStatus.Width(m.w).Render(truncate(hint, m.w-2))

	if m.act != nil {
		body = m.act.view(m.sp.View())
	}

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, bar, body, status))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// ── shared helpers ───────────────────────────────────────────

// pane wraps content to an exact box, so panes cannot push the layout around
// when their content is taller or wider than expected.
func pane(w, h int, s string) string {
	return lipgloss.NewStyle().Width(w).Height(h).MaxWidth(w).MaxHeight(h).Render(s)
}

func newViewport(w, h int) viewport.Model {
	v := viewport.New(viewport.WithWidth(w), viewport.WithHeight(h))
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
