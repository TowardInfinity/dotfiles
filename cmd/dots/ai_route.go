package main

import (
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/TowardInfinity/dotfiles/internal/dots/ai"
	"github.com/TowardInfinity/dotfiles/internal/dots/memory"
)

type aiSessionsMsg struct {
	project  memory.ProjectKey
	sessions []ai.SessionRef
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
