package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// sidebar rows are either a group label or a page. Flattening the tree into one
// list keeps cursor movement trivial — j/k just skips the labels.
type row struct {
	group  string
	doc    *doc
	isHead bool
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

	rendered string
	lastDoc  string
}

func newDocsModel(all []doc) docsModel {
	m := docsModel{all: all, sidebar: navWidth, showNav: true}
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

// snapToDoc moves off a group label onto a real page.
func (m *docsModel) snapToDoc(dir int) {
	for m.cur >= 0 && m.cur < len(m.rows) && m.rows[m.cur].isHead {
		m.cur += dir
	}
	if m.cur < 0 || m.cur >= len(m.rows) {
		for i := range m.rows {
			if !m.rows[i].isHead {
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
	for n >= 0 && n < len(m.rows) && m.rows[n].isHead {
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
	outlineWidth = 26
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

	m.vp = newViewport(m.contentWidth(), h-2)
	m.lastDoc = "" // force a re-render at the new width
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

func (m docsModel) update(msg tea.Msg) (docsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.filtering {
			switch msg.String() {
			case "esc":
				m.filtering = false
				m.filter = ""
				m.rebuild()
				return m, nil
			case "enter":
				m.filtering = false
				return m, nil
			case "backspace":
				if m.filter != "" {
					r := []rune(m.filter)
					m.filter = string(r[:len(r)-1])
					m.rebuild()
				}
				return m, nil
			default:
				if len(msg.Runes) > 0 {
					m.filter += string(msg.Runes)
					m.rebuild()
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "j", "down":
			m.move(+1)
			return m, nil
		case "k", "up":
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
			return m, nil
		case "esc":
			if m.filter != "" {
				m.filter = ""
				m.rebuild()
			}
			return m, nil
		case "d", "pgdown", " ":
			m.vp.HalfPageDown()
			return m, nil
		case "u", "pgup":
			m.vp.HalfPageUp()
			return m, nil
		}
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
		cue := styFilter.Render("/" + m.filter)
		if m.filtering {
			cue += styFilter.Render("▏")
		}
		b.WriteString(" " + padRight(truncate(cue, inner), inner) + "\n")
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
	}
	end := start + visible
	if end > len(m.rows) {
		end = len(m.rows)
	}

	for i := start; i < end; i++ {
		r := m.rows[i]
		if r.isHead {
			b.WriteString(" " + styGroup.Render(truncate(strings.ToUpper(r.group), inner)) + "\n")
			continue
		}
		label := truncate(r.doc.Title, inner-2)
		if i == m.cur {
			b.WriteString(styItemCursor.Render("▌") +
				styItemOn.Render(padRight(" "+label, navWidth-1)) + "\n")
		} else {
			b.WriteString("  " + styItem.Render(label) + "\n")
		}
	}

	return stySidebar.Width(navWidth).Height(m.h).Render(b.String())
}

func (m docsModel) viewContent() string {
	measure := m.contentWidth()

	// Breadcrumb rather than a bare title: on a 22-page reference, which group
	// a page belongs to is as useful as its name.
	var head string
	if d := m.current(); d != nil {
		head = styGroup.Render(strings.ToUpper(d.Group)) +
			styMuted.Render(" › ") +
			styTitle.Render(d.Title)
		if d.Summary != "" {
			head += "\n" + styMuted.Render(truncate(d.Summary, measure))
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
			b.WriteString(" " + styMuted.Render("·") + " " +
				styItem.Render(truncate(h, outlineWidth-5)) + "\n")
		}
	}

	// Scroll position, which the old layout gave no indication of at all.
	pct := 100
	if m.vp.Height > 0 {
		pct = int(m.vp.ScrollPercent() * 100)
	}
	b.WriteString("\n" + styMuted.Render(fmt.Sprintf(" %d%%", pct)))

	return lipgloss.NewStyle().
		Width(outlineWidth).
		Height(m.h).
		PaddingLeft(1).
		Render(b.String())
}

func (m docsModel) help() string {
	if m.filtering {
		return "type to filter  ·  enter keep  ·  esc clear"
	}
	return "j/k page  ·  / filter  ·  d/u scroll"
}
