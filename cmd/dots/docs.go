package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// sidebar rows are a group label, a page, or a blank spacer between groups.
// Flattening the tree into one list keeps cursor movement trivial — j/k just
// skips the labels — and, just as importantly, keeps the scroll window
// honest: the spacer used to be an extra "\n" tacked on outside this list
// during rendering, so the row count viewNav windowed against undercounted
// the lines it was actually about to print. A window near the bottom of a
// list spanning several small groups would then render taller than the box,
// pushing the last rows off the bottom of the terminal with no scroll state
// that could reach them. Making the spacer a real row fixes that: one row,
// one rendered line, no exceptions.
type row struct {
	group   string
	doc     *doc
	isHead  bool
	isBlank bool
}

type docsModel struct {
	all  []doc
	rows []row
	cur  int

	vp          viewport.Model
	w, h        int
	sidebar     int
	outlineW    int
	showNav     bool
	showOutline bool

	filtering bool
	filter    string
	ti        textinput.Model

	rendered string
	lastDoc  string
}

func newDocsModel(all []doc) docsModel {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "filter"
	ti.PromptStyle = styFilter
	ti.TextStyle = styValue
	ti.PlaceholderStyle = styMuted
	ti.CharLimit = 40

	m := docsModel{all: all, sidebar: navWidth, showNav: true, ti: ti}
	m.rebuild()
	return m
}

// rebuild recomputes the visible tree. A filter matches against title, summary
// and full body, so searching for "clipboard" finds the page that mentions it
// even when the word is nowhere in its title.
func (m *docsModel) rebuild() {
	q := strings.ToLower(strings.TrimSpace(m.filter))
	m.rows = m.rows[:0]

	var lastGroup string
	for i := range m.all {
		d := &m.all[i]
		if q != "" {
			hay := strings.ToLower(d.Title + " " + d.Summary + " " + d.Name + " " + d.Body)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		if d.Group != lastGroup {
			if lastGroup != "" {
				m.rows = append(m.rows, row{isBlank: true})
			}
			m.rows = append(m.rows, row{group: d.Group, isHead: true})
			lastGroup = d.Group
		}
		m.rows = append(m.rows, row{doc: d})
	}

	if m.cur >= len(m.rows) {
		m.cur = len(m.rows) - 1
	}
	if m.cur < 0 {
		m.cur = 0
	}
	m.snapToDoc(+1)
}

// snapToDoc moves off a group label or spacer onto a real page.
func (m *docsModel) snapToDoc(dir int) {
	for m.cur >= 0 && m.cur < len(m.rows) && (m.rows[m.cur].isHead || m.rows[m.cur].isBlank) {
		m.cur += dir
	}
	if m.cur < 0 || m.cur >= len(m.rows) {
		for i := range m.rows {
			if !m.rows[i].isHead && !m.rows[i].isBlank {
				m.cur = i
				return
			}
		}
	}
}

func (m *docsModel) move(delta int) {
	if len(m.rows) == 0 {
		return
	}
	n := m.cur + delta
	for n >= 0 && n < len(m.rows) && (m.rows[n].isHead || m.rows[n].isBlank) {
		n += delta
	}
	if n < 0 || n >= len(m.rows) {
		return
	}
	m.cur = n
}

func (m docsModel) current() *doc {
	if m.cur >= 0 && m.cur < len(m.rows) {
		return m.rows[m.cur].doc
	}
	return nil
}

// Layout, widest first:
//
//	nav │ content │ outline     >= 132 cols
//	nav │ content               >= 76
//	content                     narrower still
//
// The content column is capped at maxMeasure regardless of how wide the
// terminal is. Prose set across 200 columns is genuinely hard to read — the
// eye loses the line on the way back — and it was the main reason the old
// full-width layout felt off even though nothing was technically wrong.
const (
	navWidth     = 24
	outlineWidth = 32
	maxMeasure   = 92
)

func (m docsModel) resize(w, h int) docsModel {
	m.w, m.h = w, h

	m.showNav = w >= 76
	m.showOutline = w >= 132

	m.sidebar = 0
	if m.showNav {
		m.sidebar = navWidth
	}
	m.outlineW = 0
	if m.showOutline {
		m.outlineW = outlineWidth
	}

	// head is 2 lines (breadcrumb + summary) and the rule is 1, so the
	// viewport gets the rest. Giving it h-2 made the column h+1 tall, and a
	// pane one line too tall pushes the tab bar off the top of the screen —
	// which is why the Docs tab appeared to have no header at all.
	m.vp = newViewport(m.contentWidth(), h-3)
	m.lastDoc = "" // force a re-render at the new width
	m.ensureRendered()
	return m
}

// contentWidth is the reading measure: what is left after the rails, capped.
func (m docsModel) contentWidth() int {
	w := m.w - m.sidebar - m.outlineW - 6
	if m.showNav {
		w-- // the nav's right border, drawn outside its Width
	}
	if w > maxMeasure {
		w = maxMeasure
	}
	if w < 20 {
		w = 20
	}
	return w
}

// update renders after handling the message, so the viewport in the MODEL holds
// the page rather than one built inside view() and thrown away.
//
// ensureRendered used to be called only from view(), which has a value
// receiver — so it filled a copy's viewport and the copy was discarded. The
// model's viewport therefore had zero lines, and a viewport with no content
// cannot scroll: d, u, space, PgDn and the wheel all left YOffset at 0. The
// percentage in the outline still looked plausible because it was computed on
// that same temporary copy.
func (m docsModel) update(msg tea.Msg) (docsModel, tea.Cmd) {
	m2, cmd := m.updateInner(msg)
	m2.ensureRendered()
	return m2, cmd
}

func (m docsModel) updateInner(msg tea.Msg) (docsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.filtering {
			switch msg.String() {
			case "esc":
				m.filtering = false
				m.ti.Blur()
				m.ti.SetValue("")
				m.filter = ""
				m.rebuild()
				return m, nil
			case "enter":
				m.filtering = false
				m.ti.Blur()
				return m, nil
			}
			// textinput handles word-delete, home/end and cursor movement,
			// none of which the hand-rolled rune append did.
			var cmd tea.Cmd
			m.ti, cmd = m.ti.Update(msg)
			if v := m.ti.Value(); v != m.filter {
				m.filter = v
				m.rebuild()
			}
			return m, cmd
		}

		switch msg.String() {
		// left/right move the sidebar too. There is no horizontal axis in this
		// pane — the body is wrapped to the reading measure, so nothing ever
		// scrolls sideways — and leaving two of the four arrows dead here while
		// Manage uses them for its rail is the inconsistency worth removing.
		case "j", "down", "l", "right":
			m.move(+1)
			return m, nil
		case "k", "up", "h", "left":
			m.move(-1)
			return m, nil
		case "g", "home":
			m.cur = 0
			m.snapToDoc(+1)
			return m, nil
		case "G", "end":
			m.cur = len(m.rows) - 1
			m.snapToDoc(-1)
			return m, nil
		case "/":
			m.filtering = true
			m.ti.Focus()
			return m, textinput.Blink
		case "esc":
			if m.filter != "" {
				m.filter = ""
				m.ti.SetValue("")
				m.rebuild()
			}
			return m, nil
		case "d", "ctrl+d", "pgdown", " ":
			m.vp.HalfPageDown()
			return m, nil
		case "u", "ctrl+u", "pgup":
			m.vp.HalfPageUp()
			return m, nil
		case "ctrl+f":
			m.vp.ViewDown()
			return m, nil
		case "ctrl+b":
			m.vp.ViewUp()
			return m, nil
		}

	case tea.MouseMsg:
		// Three lines per notch is what most terminals send and what reads as
		// "a scroll" rather than a jump.
		switch msg.Button {
		case tea.MouseButtonWheelDown:
			m.vp.LineDown(3)
			return m, nil
		case tea.MouseButtonWheelUp:
			m.vp.LineUp(3)
			return m, nil
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m *docsModel) ensureRendered() {
	d := m.current()
	if d == nil {
		m.rendered = styMuted.Render("  no page matches that filter")
		m.vp.SetContent(m.rendered)
		return
	}
	if d.Name == m.lastDoc {
		return
	}
	r, err := newRenderer(m.contentWidth())
	if err != nil {
		m.rendered = d.Body
	} else if out, err := r.Render(d.Body); err == nil {
		m.rendered = out
	} else {
		m.rendered = d.Body
	}
	m.lastDoc = d.Name
	m.vp.SetContent(m.rendered)
	m.vp.GotoTop()
}

func (m docsModel) view() string {
	m.ensureRendered()

	cols := []string{}
	if m.showNav {
		cols = append(cols, m.viewNav())
	}
	cols = append(cols, m.viewContent())
	if m.showOutline {
		cols = append(cols, m.viewOutline())
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

func (m docsModel) viewNav() string {
	var b strings.Builder
	inner := navWidth - 2

	// The filter lives at the top of the nav so it reads as "narrowing this
	// list", which is what it does.
	if m.filtering || m.filter != "" {
		m.ti.Width = inner - 2
		b.WriteString(" " + truncate(m.ti.View(), inner) + "\n")
	} else {
		b.WriteString(" " + styMuted.Render(padRight(
			truncate(fmt.Sprintf("%d pages", len(m.all)), inner), inner)) + "\n")
	}
	b.WriteString("\n")

	visible := m.h - 3
	if visible < 1 {
		visible = 1
	}
	start := 0
	if len(m.rows) > visible {
		start = m.cur - visible/2
		if start < 0 {
			start = 0
		}
		if start > len(m.rows)-visible {
			start = len(m.rows) - visible
		}
		// A spacer landing on the box's own top row just wastes a line —
		// skip it forward onto the head it was separating groups from.
		if m.rows[start].isBlank {
			start++
		}
	}
	end := start + visible
	if end > len(m.rows) {
		end = len(m.rows)
	}

	for i := start; i < end; i++ {
		r := m.rows[i]
		if r.isBlank {
			// Groups sit one column in, pages three. They were both at two,
			// which is how the whole rail ended up reading as one flat list.
			b.WriteString("\n")
			continue
		}
		if r.isHead {
			b.WriteString(" " + styGroup.Render(truncate(strings.ToUpper(r.group), inner)) + "\n")
			continue
		}
		label := truncate(r.doc.Title, inner-3)
		if i == m.cur {
			b.WriteString(styItemCursor.Render("▌") +
				styItemOn.Render(padRight("  "+label, navWidth-1)) + "\n")
		} else {
			b.WriteString("   " + styItem.Render(label) + "\n")
		}
	}

	return stySidebar.Width(navWidth).Height(m.h).Render(b.String())
}

func (m docsModel) viewContent() string {
	measure := m.contentWidth()

	// Breadcrumb rather than a bare title: on a 22-page reference, which group
	// a page belongs to is as useful as its name.
	// Always two lines, so the column height is the same on every page. A
	// header that changes height by one shifts everything below it and, at the
	// bottom of the stack, silently costs you the tab bar.
	head := "\n"
	if d := m.current(); d != nil {
		head = styGroup.Render(strings.ToUpper(d.Group)) +
			styMuted.Render(" › ") +
			styTitle.Render(d.Title) + "\n"
		if d.Summary != "" {
			head += styMuted.Render(truncate(d.Summary, measure))
		}
	}

	rule := styMuted.Render(strings.Repeat("─", measure))

	body := lipgloss.JoinVertical(lipgloss.Left,
		head,
		rule,
		m.vp.View(),
	)

	// Centre the measure in whatever space is left, so a very wide terminal
	// gets margins instead of a 200-column line.
	//
	// The -1 is the nav's right border: lipgloss draws borders OUTSIDE the
	// declared Width, so the rail actually occupies navWidth+1 columns. This
	// is the second time that has cost an off-by-one, hence spelling it out.
	avail := m.w - m.sidebar - m.outlineW
	if m.showNav {
		avail--
	}
	if avail < 20 {
		avail = 20
	}
	return lipgloss.NewStyle().
		Width(avail).
		Height(m.h).
		Padding(0, 2).
		Render(body)
}

// viewOutline is the page's own H2 headings. On a wide terminal the right
// third was simply empty; showing the shape of the page is the most useful
// thing that space can hold, and it costs nothing to compute.
func (m docsModel) viewOutline() string {
	d := m.current()
	var b strings.Builder

	b.WriteString(styGroup.Render("ON THIS PAGE") + "\n\n")
	if d != nil {
		heads := outline(d.Body)
		if len(heads) == 0 {
			b.WriteString(styMuted.Render(" —") + "\n")
		}
		for _, h := range heads {
			// Strip inline-code ticks: "Why `Q` does not drop you" reads as
			// markup in a rail, and the backticks eat width the heading needs.
			h = strings.ReplaceAll(h, "`", "")

			// Wrap rather than truncate. An outline exists so you can see the
			// shape of a page without scrolling it — "The servers are arm64…"
			// tells you nothing the heading was for. Continuation lines are
			// indented under the bullet so the entries stay distinguishable.
			lines := wrapPlain(h, outlineWidth-4)
			for j, ln := range lines {
				if j == 0 {
					b.WriteString(" " + styMuted.Render("·") + " " + styItem.Render(ln) + "\n")
				} else {
					b.WriteString("   " + styItem.Render(ln) + "\n")
				}
			}
		}
	}

	// Scroll position. A bare percentage is easy to miss — say plainly when
	// there is more below, since "is that the whole page?" is the question.
	pct := 100
	if m.vp.Height > 0 {
		pct = int(m.vp.ScrollPercent() * 100)
	}
	b.WriteString("\n" + styMuted.Render(fmt.Sprintf(" %d%%", pct)))
	if !m.vp.AtBottom() {
		b.WriteString("  " + styPending.Render("▼ more"))
	}

	return lipgloss.NewStyle().
		Width(outlineWidth).
		Height(m.h).
		PaddingLeft(1).
		Render(b.String())
}
