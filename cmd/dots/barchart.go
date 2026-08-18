package main

import (
	"fmt"
	"strings"

	"github.com/TowardInfinity/dotfiles/internal/dots/ai"
)

type localActivityBar struct {
	label string
	value int64
}

// proportionalBarWidth returns a display width relative to the largest value
// in this rendered set. It is intentionally not a percentage of a service
// limit: the only scale here is the other observed local activity rows.
func proportionalBarWidth(value, largest int64, width int) int {
	if value <= 0 || largest <= 0 || width <= 0 {
		return 0
	}
	if value >= largest {
		return width
	}
	// Round up so non-zero local activity is visible even beside a much larger
	// row, while never exceeding the available display width.
	n := int((value*int64(width) + largest - 1) / largest)
	if n > width {
		return width
	}
	return n
}

func localActivitySparkline(values []int64) string {
	if len(values) == 0 {
		return ""
	}
	var largest int64
	for _, value := range values {
		if value > largest {
			largest = value
		}
	}
	if largest == 0 {
		return strings.Repeat("░", len(values))
	}
	const blocks = "▁▂▃▄▅▆▇█"
	var b strings.Builder
	for _, value := range values {
		if value <= 0 {
			b.WriteString("░")
			continue
		}
		index := int((value*7 + largest - 1) / largest)
		b.WriteRune([]rune(blocks)[index])
	}
	return b.String()
}

func localActivityPanel(report ai.UsageReport, trend []int64, measure int) string {
	bars := []localActivityBar{
		{label: "claude", value: report.Claude.Tokens()},
		{label: "codex", value: report.Codex.Tokens()},
	}
	var largest int64
	for _, bar := range bars {
		if bar.value > largest {
			largest = bar.value
		}
	}
	barWidth := max(4, measure-27)
	lines := []string{
		styGroup.Render("LOCAL ACTIVITY · LAST 5 HOURS"),
		styMuted.Render("bar length compares observed local tokens in this pane only · not a quota scale"),
	}
	for _, bar := range bars {
		n := proportionalBarWidth(bar.value, largest, barWidth)
		fill := strings.Repeat("█", n)
		empty := strings.Repeat("░", barWidth-n)
		lines = append(lines, fmt.Sprintf("%-7s %s%s  %d tokens", bar.label, fill, empty, bar.value))
	}
	lines = append(lines,
		fmt.Sprintf("grok    %d recent session(s) · no per-message timestamps", report.GrokSessionsWithoutTimestamps),
		fmt.Sprintf("cursor  %d recent session(s) · no per-message timestamps", report.CursorSessionsWithoutTimestamps),
		styMuted.Render("hourly local tokens: "+localActivitySparkline(trend)),
	)
	return strings.Join(lines, "\n")
}
