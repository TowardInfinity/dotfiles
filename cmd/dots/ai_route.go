package main

import (
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/TowardInfinity/dotfiles/internal/dots/ai"
	"github.com/TowardInfinity/dotfiles/internal/dots/memory"
)

type aiSessionsMsg struct {
	project  memory.ProjectKey
	sessions []ai.SessionRef
}

type aiUsageMsg struct {
	project memory.ProjectKey
	report  ai.UsageReport
	trend   []int64
}

// fetchAISessions is a read-only route provider. It deliberately reads the
// independent session metadata, not memory.LoadIndex, so the route works even
// while capture owns the index lock or Ollama is unavailable.
func fetchAISessions(repo string) tea.Cmd {
	return func() tea.Msg {
		dir := repo
		if dir == "" {
			dir, _ = os.Getwd()
		}
		project, _, _ := memory.ResolveProject(dir)
		return aiSessionsMsg{project: project, sessions: ai.RecentSessions(project)}
	}
}

// aiUsageCutoffs returns six one-hour boundaries for the five-hour local
// activity view. UsageForWindows scans each transcript once for all of these
// cutoffs, so the sparkline does not multiply filesystem work.
func aiUsageCutoffs(now time.Time) []time.Time {
	const points = 6
	cutoffs := make([]time.Time, points)
	for i := range cutoffs {
		cutoffs[i] = now.Add(-5*time.Hour + time.Duration(i)*time.Hour)
	}
	return cutoffs
}

// aiUsageTrend turns cumulative-after-cutoff reports into five hourly local
// token buckets. A negative difference is treated as zero because malformed
// or reset local snapshots must not draw a negative bar.
func aiUsageTrend(reports []ai.UsageReport) []int64 {
	if len(reports) < 2 {
		return nil
	}
	totals := make([]int64, len(reports))
	for i, report := range reports {
		totals[i] = report.Claude.Tokens() + report.Codex.Tokens()
	}
	trend := make([]int64, len(totals)-1)
	for i := range trend {
		trend[i] = totals[i] - totals[i+1]
		if trend[i] < 0 {
			trend[i] = 0
		}
	}
	return trend
}

// fetchAIUsage reads raw local transcript activity for the AI pane. It is
// deliberately local only: none of the returned counts represents allowance,
// quota, reset time, or web activity on the same account.
func fetchAIUsage(repo string) tea.Cmd {
	return func() tea.Msg {
		dir := repo
		if dir == "" {
			dir, _ = os.Getwd()
		}
		project, _, _ := memory.ResolveProject(dir)
		reports := ai.UsageForWindows(project, aiUsageCutoffs(time.Now()))
		return aiUsageMsg{project: project, report: reports[0], trend: aiUsageTrend(reports)}
	}
}
