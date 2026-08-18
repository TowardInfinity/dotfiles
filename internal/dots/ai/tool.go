// Package ai contains the thin, interactive launch surface for the AI CLIs
// configured by this repository.
//
// It deliberately knows only how each CLI spells “resume”. Session discovery
// and cross-tool selection belong to the later console phase; a direct launch
// must remain useful without an index, capture pass, or local model.
package ai

import "time"

// Tool describes one supported CLI. The differences are argv shape, not
// behaviour, so a small static table is clearer than five implementations of
// an interface with one method each.
type Tool struct {
	// Name matches memory.Session.Tool when the console phase later enriches
	// these launches with local session data.
	Name string
	// Alias is the dots subcommand.
	Alias string
	// Binary is deliberately a command name, never a machine-specific path.
	// The CLI edge resolves it on PATH immediately before syscall.Exec.
	Binary string
}

var tools = []Tool{
	{Name: "claude-code", Alias: "claude", Binary: "claude"},
	{Name: "codex", Alias: "codex", Binary: "codex"},
	{Name: "grok", Alias: "grok", Binary: "grok"},
	{Name: "cursor", Alias: "cursor", Binary: "cursor-agent"},
}

// SessionRef is the minimum local record needed to resume or display a
// session. It intentionally does not carry transcript text or summaries:
// choosing a tool must remain independent of the memory index and Ollama.
type SessionRef struct {
	Tool    string
	ID      string
	Updated time.Time
}

func toolForAlias(alias string) (Tool, bool) {
	for _, tool := range tools {
		if tool.Alias == alias {
			return tool, true
		}
	}
	return Tool{}, false
}
