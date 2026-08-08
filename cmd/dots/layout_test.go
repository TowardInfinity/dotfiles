package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"strings"
	"testing"
)

func TestLayoutBreakpoints(t *testing.T) {
	for _, tc := range []struct {
		w            int
		nav, outline bool
	}{
		{200, true, true}, {140, true, true}, {110, true, false}, {70, false, false},
	} {
		m := newModel()
		var tm tea.Model = m
		tm, _ = tm.Update(tea.WindowSizeMsg{Width: tc.w, Height: 40})
		d := tm.(model).docs
		if d.showNav != tc.nav || d.showOutline != tc.outline {
			t.Errorf("w=%d nav=%v outline=%v, want nav=%v outline=%v",
				tc.w, d.showNav, d.showOutline, tc.nav, tc.outline)
		}
		if got := d.contentWidth(); got > maxMeasure {
			t.Errorf("w=%d measure %d exceeds cap %d", tc.w, got, maxMeasure)
		}
		v := tm.(model).View()
		for _, line := range strings.Split(v, "\n") {
			if lipgloss.Width(line) > tc.w {
				t.Errorf("w=%d overflow: %d", tc.w, lipgloss.Width(line))
				break
			}
		}
		if tc.outline && !strings.Contains(v, "ON THIS PAGE") {
			t.Errorf("w=%d: outline rail missing", tc.w)
		}
		if !tc.outline && strings.Contains(v, "ON THIS PAGE") {
			t.Errorf("w=%d: outline rail shown when it should not be", tc.w)
		}
	}
}

// The scroll hint on Overview/Dotfiles is appended after windowing to
// bodyRows() lines, so if that window isn't shrunk by one first, the hint
// itself is always the line contentColumn's MaxHeight clips off — it would
// never actually render. Pins the fix at the exact heights where Overview's
// 11 lines first overflow.
func TestOverviewScrollHintSurvivesClipping(t *testing.T) {
	for _, h := range []int{13, 12, 10, 8} {
		m := manageModel{w: 100, h: h, section: secOverview}
		m.ovInfo = overviewInfo{host: "mac", osName: "darwin", arch: "arm64", uptimeKnown: true}
		out := m.view("")
		if !strings.Contains(out, "more, j/k to scroll") {
			t.Errorf("h=%d: scroll hint missing from rendered output; got clipped", h)
		}
	}
}

func TestNoDuplicateTitle(t *testing.T) {
	docs, _ := loadDocs(findRepo())
	for _, d := range docs {
		if strings.HasPrefix(strings.TrimSpace(d.Body), "# ") {
			t.Errorf("%s: body still starts with an H1 — it will render twice", d.Name)
		}
	}
}
