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

	vp      viewport.Model
	w, h    int
	sidebar int

	filtering bool
	filter    string

	rendered string
	lastDoc  string
}

func newDocsModel(all []doc) docsModel {
	m := docsModel{all: all, sidebar: 22}
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

func (m docsModel) resize(w, h int) docsModel {
	m.w, m.h = w, h
	m.sidebar = w / 4
	if m.sidebar < 16 {
		m.sidebar = 16
	}
	if m.sidebar > 30 {
		m.sidebar = 30
	}
	m.vp = newViewport(m.contentWidth(), h)
	m.lastDoc = "" // force a re-render at the new width
	return m
}

func (m docsModel) contentWidth() int {
	w := m.w - m.sidebar - 5
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

	// ── sidebar ──
	var b strings.Builder
	inner := m.sidebar - 1

	if m.filtering || m.filter != "" {
		cue := styFilter.Render("/" + m.filter)
		if m.filtering {
			cue += styFilter.Render("▋")
		}
		b.WriteString(padRight(truncate(cue, inner), inner) + "\n")
	} else {
		b.WriteString(styMuted.Render(padRight(truncate(fmt.Sprintf("%d pages", len(m.all)), inner), inner)) + "\n")
	}

	// Keep the cursor in view when the list is taller than the pane.
	start := 0
	visible := m.h - 2
	if visible < 1 {
		visible = 1
	}
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
			b.WriteString(styGroup.Render(truncate(strings.ToUpper(r.group), inner)) + "\n")
			continue
		}
		label := truncate(r.doc.Title, inner-3)
		if i == m.cur {
			b.WriteString(styItemCursor.Render("▍") + styItemOn.Render(padRight(label, inner-2)) + "\n")
		} else {
			b.WriteString(styItem.Render(label) + "\n")
		}
	}

	side := stySidebar.Width(m.sidebar).Height(m.h).Render(b.String())

	// ── content ──
	var head string
	if d := m.current(); d != nil {
		head = styTitle.Render(d.Title)
		if d.Summary != "" {
			head += styMuted.Render("  " + d.Summary)
		}
	}
	content := lipgloss.JoinVertical(lipgloss.Left,
		truncate(head, m.contentWidth()),
		m.vp.View(),
	)
	// -1 for the sidebar's right border, which sits outside its Width().
	// Without it every docs line is exactly one column too wide.
	content = styPane.Width(m.w - m.sidebar - 1).Height(m.h).Render(content)

	return lipgloss.JoinHorizontal(lipgloss.Top, side, content)
}

func (m docsModel) help() string {
	if m.filtering {
		return "type to filter  ·  enter keep  ·  esc clear"
	}
	return "j/k page  ·  / filter  ·  d/u scroll"
}
