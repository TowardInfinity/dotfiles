package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// Shared chrome. Every pane was drawing its own header in its own way, which
// is why the app looked like three programs stapled together — and why a
// height bug in one of them was invisible in the others.
//
// The family is: an optional left rail, a content column with a two-line
// header and a rule, and an optional right rail. Docs uses all three, Manage
// uses a rail and content, Doctor uses content alone.

const (
	railWidth  = 24
	measureCap = 92
)

// paneHeader is always exactly two lines. A header that changes height shifts
// everything below it, and at the bottom of the stack that costs the tab bar
// its row — which is precisely how the Docs tab lost its header.
func paneHeader(group, title, summary string, measure int) string {
	head := styGroup.Render(strings.ToUpper(group))
	if title != "" {
		head += styMuted.Render(" › ") + styTitle.Render(title)
	}
	head = truncate(head, measure)

	second := " "
	if summary != "" {
		second = styMuted.Render(truncate(summary, measure))
	}
	return head + "\n" + second
}

func hrule(measure int) string {
	if measure < 1 {
		return ""
	}
	return styMuted.Render(strings.Repeat("─", measure))
}

// contentColumn assembles header + rule + body into a fixed box. Body is given
// h-3 lines; anything longer is clipped rather than allowed to push the layout.
func contentColumn(w, h int, header, body string) string {
	measure := w - 4
	if measure > measureCap {
		measure = measureCap
	}
	if measure < 10 {
		measure = 10
	}
	// Clip every body line to the measure rather than trusting callers to.
	//
	// A line longer than the column does not just look wrong — it WRAPS, which
	// adds a row, and one extra row is enough to push the tab bar off the top.
	// So an over-long service name in one section could remove the header from
	// the whole app. Truncating here makes that structurally impossible.
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		lines[i] = truncate(ln, measure)
	}
	body = strings.Join(lines, "\n")

	inner := lipgloss.JoinVertical(lipgloss.Left, header, hrule(measure), body)
	return lipgloss.NewStyle().
		Width(w).
		Height(h).
		MaxHeight(h).
		MaxWidth(w).
		Padding(0, 2).
		Render(inner)
}

// measureFor is the reading width a content column will actually use, so
// callers can wrap their own text to match.
func measureFor(w int) int {
	m := w - 4
	if m > measureCap {
		m = measureCap
	}
	if m < 10 {
		m = 10
	}
	return m
}

// railItem is one row of a left rail: either a group label or a selectable
// entry. Shared so Docs and Manage highlight the same way.
type railItem struct {
	label  string
	isHead bool
}

func renderRail(items []railItem, cursor, w, h int, top string) string {
	var b strings.Builder
	inner := w - 2

	if top != "" {
		b.WriteString(" " + padRight(truncate(top, inner), inner) + "\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString("\n")

	visible := h - 3
	if visible < 1 {
		visible = 1
	}
	start := 0
	if len(items) > visible {
		start = cursor - visible/2
		if start < 0 {
			start = 0
		}
		if start > len(items)-visible {
			start = len(items) - visible
		}
	}
	end := start + visible
	if end > len(items) {
		end = len(items)
	}

	for i := start; i < end; i++ {
		it := items[i]
		if it.isHead {
			b.WriteString(" " + styGroup.Render(truncate(strings.ToUpper(it.label), inner)) + "\n")
			continue
		}
		label := truncate(it.label, inner-3)
		if i == cursor {
			b.WriteString(styItemCursor.Render("▌") +
				styItemOn.Render(padRight("  "+label, w-1)) + "\n")
		} else {
			b.WriteString("   " + styItem.Render(label) + "\n")
		}
	}

	return stySidebar.Width(w).Height(h).Render(b.String())
}

// statRow is the key/value line used throughout Doctor and Manage. Keeping one
// implementation is what makes the two panes look related rather than merely
// adjacent.
func statRow(key, value string, keyW int) string {
	return styKey.Render(padRight(key, keyW)) + value
}

// stateDot encodes state in shape as well as colour, so it still reads on a
// terminal with a limited palette or for anyone who cannot separate the hues.
// checkDot renders a doctor row's marker for all four states. stateDot below
// only knows ok/not-ok, which would paint a warning the same red as a failure
// and undo the distinction the state exists to make.
func checkDot(s checkState) string {
	switch s {
	case checkPending:
		return styMuted.Render("○")
	case checkOK:
		return styOK.Render("●")
	case checkWarn:
		return styPending.Render("●")
	default:
		return styBad.Render("●")
	}
}

func stateDot(ok, known bool) string {
	switch {
	case !known:
		return styMuted.Render("○")
	case ok:
		return styOK.Render("●")
	default:
		return styBad.Render("●")
	}
}

// countSummary renders "12 / 13 present" with the number coloured by whether
// anything is outstanding.
func countSummary(have, total int, noun string) string {
	s := fmt.Sprintf("%d / %d %s", have, total, noun)
	if have == total {
		return styOK.Render(s)
	}
	return styPending.Render(s)
}

// dataTable renders rows as a real table with rules, rather than columns of
// hand-padded spaces. lipgloss/table measures each column itself, so a long
// path no longer shoves the next column sideways — which is what made the
// hand-rolled version drift out of alignment as content changed.
//
// selected is the row index to highlight, or -1 for none.
func dataTable(headers []string, rows [][]string, selected, maxW int) string {
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(cLine)).
		BorderTop(false).
		BorderLeft(false).
		BorderRight(false).
		BorderColumn(false).
		Headers(headers...).
		Rows(rows...).
		Width(maxW).
		StyleFunc(func(row, col int) lipgloss.Style {
			base := lipgloss.NewStyle().Padding(0, 1)
			switch {
			case row == table.HeaderRow:
				return base.Foreground(cFaint).Bold(true)
			case row == selected:
				return base.Foreground(cInk).Background(cSurface).Bold(true)
			default:
				return base.Foreground(cInkDim)
			}
		})
	return t.String()
}
