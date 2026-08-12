package main

import (
	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
)

// Tokyo Night. Not chosen for this program — lifted from the configs it
// documents, so Ghostty, tmux, Neovim and this app all agree. The blue/green
// split is load-bearing rather than decorative: it is exactly what the two
// tmux status bars use to say which machine you are looking at, and the same
// meaning carries through every OS tag and machine row here.
var (
	cBg = lipgloss.Color("#1a1b26")
	// Selection needs to survive real terminal contrast and screenshots. The
	// old surface (#1f2130) was only a few RGB steps above the background, so
	// list cursors looked absent unless the terminal was viewed at high zoom.
	cSurface = lipgloss.Color("#26304a")
	cLine    = lipgloss.Color("#292e42")
	cInk     = lipgloss.Color("#c0caf5")
	cInkDim  = lipgloss.Color("#9aa5ce")
	cFaint   = lipgloss.Color("#6b74a0")

	cMac    = lipgloss.Color("#7aa2f7") // macOS, and the primary accent
	cLinux  = lipgloss.Color("#9ece6a") // Linux, and "good"
	cWarn   = lipgloss.Color("#e0af68")
	cHot    = lipgloss.Color("#f7768e")
	cViolet = lipgloss.Color("#bb9af7")
	cCyan   = lipgloss.Color("#7dcfff")
)

var (
	// Tabs. The active one is a solid block rather than an underline: at a
	// glance you are looking for a filled shape, not a 1px rule.
	styTab = lipgloss.NewStyle().
		Padding(0, 2).
		Foreground(cFaint)

	styTabOn = lipgloss.NewStyle().
			Padding(0, 2).
			Foreground(cBg).
			Background(cMac).
			Bold(true)

	styTabBar = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(cLine)

	// Sidebar
	styGroup = lipgloss.NewStyle().
			Foreground(cFaint).
			Bold(true)

	// Colour and weight only — no padding. These styles are applied to strings
	// the caller has already measured and padded to a rail width, so carrying
	// PaddingLeft here silently added 2-3 columns on top and pushed the line
	// past the rail, where it wrapped: the selected row grew a blank
	// highlighted second line, and outline entries broke mid-word.
	// A style that changes geometry behind the caller's back is the problem.
	styItem = lipgloss.NewStyle().
		Foreground(cInkDim)

	styItemOn = lipgloss.NewStyle().
			Foreground(cInk).
			Background(cSurface).
			Bold(true)

	styItemCursor = lipgloss.NewStyle().Foreground(cMac).Bold(true)

	// Panes
	stySidebar = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderRight(true).
			BorderForeground(cLine)

	styPane = lipgloss.NewStyle().Padding(0, 2)

	styTitle = lipgloss.NewStyle().Foreground(cMac).Bold(true)

	styHint = lipgloss.NewStyle().Foreground(cFaint)

	styStatus = lipgloss.NewStyle().
			Foreground(cFaint).
			BorderStyle(lipgloss.NormalBorder()).
			BorderTop(true).
			BorderForeground(cLine).
			Padding(0, 1)

	styFilter = lipgloss.NewStyle().Foreground(cCyan)

	// Semantic state — deliberately separate from the accent, so "good" never
	// reads as "selected" and a warning never reads as a heading.
	styOK      = lipgloss.NewStyle().Foreground(cLinux)
	styBad     = lipgloss.NewStyle().Foreground(cHot)
	styPending = lipgloss.NewStyle().Foreground(cWarn)
	styMuted   = lipgloss.NewStyle().Foreground(cFaint)
	styKey     = lipgloss.NewStyle().Foreground(cViolet)
	styValue   = lipgloss.NewStyle().Foreground(cInk)

	// OS tags carry the same colour meaning as the tmux status bars.
	styOSMac   = lipgloss.NewStyle().Foreground(cMac)
	styOSLinux = lipgloss.NewStyle().Foreground(cLinux)
	styOSBoth  = lipgloss.NewStyle().Foreground(cFaint)
)

// glamourStyle renders markdown in the same palette as everything else.
// Glamour ships light/dark defaults that are perfectly fine and completely
// unrelated to Tokyo Night, which is exactly the mismatch that made the old
// output look borrowed.
func glamourStyle() ansi.StyleConfig {
	s := func(v string) *string { return &v }
	b := func(v bool) *bool { return &v }
	u := func(v uint) *uint { return &v }

	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: s("#c0caf5")},
			Margin:         u(0),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: s("#bb9af7"), Italic: b(true)},
			Indent:         u(1),
			IndentToken:    s("│ "),
		},
		Paragraph: ansi.StyleBlock{},
		List: ansi.StyleList{
			StyleBlock:  ansi.StyleBlock{IndentToken: s("  ")},
			LevelIndent: 2,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Bold: b(true), Color: s("#7aa2f7")},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "", Suffix: "",
				Color: s("#7aa2f7"), Bold: b(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Prefix: "", Color: s("#e0af68"), Bold: b(true)},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Prefix: "", Color: s("#9ece6a"), Bold: b(true)},
		},
		Text:           ansi.StylePrimitive{},
		Strong:         ansi.StylePrimitive{Bold: b(true), Color: s("#c0caf5")},
		Emph:           ansi.StylePrimitive{Italic: b(true)},
		HorizontalRule: ansi.StylePrimitive{Color: s("#292e42"), Format: "\n─────\n"},
		Item:           ansi.StylePrimitive{BlockPrefix: "• "},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: s("#7dcfff")},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{Color: s("#9ece6a")},
				Margin:         u(2),
			},
		},
		Table: ansi.StyleTable{
			StyleBlock:      ansi.StyleBlock{},
			CenterSeparator: s("┼"),
			ColumnSeparator: s("│"),
			RowSeparator:    s("─"),
		},
		Link:     ansi.StylePrimitive{Color: s("#7dcfff"), Underline: b(true)},
		LinkText: ansi.StylePrimitive{Color: s("#7dcfff")},
	}
}

func newRenderer(width int) (*glamour.TermRenderer, error) {
	if width < 20 {
		width = 20
	}
	return glamour.NewTermRenderer(
		glamour.WithStyles(glamourStyle()),
		glamour.WithWordWrap(width),
	)
}
