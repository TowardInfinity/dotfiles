package main

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/TowardInfinity/dotfiles/internal/dots/ai"
)

func TestAIUsageCutoffsSpanFiveHoursInHourlySteps(t *testing.T) {
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	cutoffs := aiUsageCutoffs(now)
	if got, want := len(cutoffs), 6; got != want {
		t.Fatalf("cutoff count = %d, want %d", got, want)
	}
	for i, cutoff := range cutoffs {
		want := now.Add(-5*time.Hour + time.Duration(i)*time.Hour)
		if !cutoff.Equal(want) {
			t.Fatalf("cutoff %d = %s, want %s", i, cutoff, want)
		}
	}
}

func TestAIUsageTrendConvertsCumulativeReportsToHourlyTokens(t *testing.T) {
	reports := []ai.UsageReport{
		{Claude: ai.TokenUsage{Input: 100}},
		{Claude: ai.TokenUsage{Input: 80}},
		{Claude: ai.TokenUsage{Input: 50}},
		{Claude: ai.TokenUsage{Input: 20}},
		{Claude: ai.TokenUsage{Input: 0}},
		{Claude: ai.TokenUsage{Input: 10}}, // malformed/reset ordering clamps to zero
	}
	got := aiUsageTrend(reports)
	want := []int64{20, 30, 30, 20, 0}
	if len(got) != len(want) {
		t.Fatalf("trend = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("trend[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestShellAIRouteRendersLocalActivityAtNarrowAndNormalWidths(t *testing.T) {
	for _, size := range [][2]int{{60, 18}, {100, 30}} {
		t.Run("render", func(t *testing.T) {
			m := shellAt(t, size[0], size[1])
			m.route = routeAI
			m.aiSessions = []ai.SessionRef{{Tool: "claude", ID: "session", Updated: time.Now()}}
			m.aiUsage = ai.UsageReport{
				Claude:                          ai.TokenUsage{Input: 120},
				Codex:                           ai.TokenUsage{Input: 30},
				GrokSessionsWithoutTimestamps:   2,
				CursorSessionsWithoutTimestamps: 1,
			}
			m.aiTrend = []int64{0, 5, 10, 2, 0}
			view := stripANSI(m.View().Content)
			for _, want := range []string{"LOCAL ACTIVITY", "claude", "codex", "grok", "cursor", "hourly local tokens"} {
				if !strings.Contains(view, want) {
					t.Fatalf("AI view at %dx%d missing %q:\n%s", size[0], size[1], want, view)
				}
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > size[0] {
					t.Fatalf("AI line overflows at %dx%d: %q", size[0], size[1], line)
				}
			}
		})
	}
}
