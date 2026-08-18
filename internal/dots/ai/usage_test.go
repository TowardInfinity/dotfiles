package ai

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/TowardInfinity/dotfiles/internal/dots/memory"
)

func TestUsageSinceCountsClaudeDirectlyAndCodexAsDeltas(t *testing.T) {
	home := t.TempDir()
	project := memory.ProjectKey("github.com/TowardInfinity/almanac")
	installLookupSeams(t, home, project)
	since := time.Date(2026, time.August, 18, 10, 0, 0, 0, time.UTC)

	writeLookupFile(t, filepath.Join(home, ".claude", "projects", "x", "claude.jsonl"), `
{"cwd":"/work/almanac","timestamp":"2026-08-18T09:00:00Z","message":{"usage":{"input_tokens":1,"output_tokens":2}}}
{"timestamp":"2026-08-18T11:00:00Z","message":{"usage":{"input_tokens":10,"cache_creation_input_tokens":20,"cache_read_input_tokens":30,"output_tokens":40,"output_tokens_details":{"thinking_tokens":5}}}}
`, since)
	// The first Codex snapshot is before the window. The 50-token delta is
	// attributed to the later snapshot, not counted as the 150-token total.
	writeLookupFile(t, filepath.Join(home, ".codex", "sessions", "2026", "08", "18", "rollout-codex.jsonl"), `
{"type":"session_meta","payload":{"id":"codex","cwd":"/work/almanac"}}
{"type":"event_msg","timestamp":"2026-08-18T09:00:00Z","payload":{"info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"cache_write_input_tokens":1,"output_tokens":4,"reasoning_output_tokens":2}}}}
{"type":"event_msg","timestamp":"2026-08-18T11:00:00Z","payload":{"info":{"total_token_usage":{"input_tokens":150,"cached_input_tokens":30,"cache_write_input_tokens":6,"output_tokens":14,"reasoning_output_tokens":7}}}}
`, since)
	writeLookupFile(t, filepath.Join(home, ".grok", "sessions", "x", "grok", "summary.json"),
		`{"info":{"id":"grok","cwd":"/work/almanac"},"updated_at":"2026-08-18T11:00:00Z","num_chat_messages":7}`+"\n", since)
	writeLookupFile(t, filepath.Join(home, ".cursor", "chats", "x", "cursor", "meta.json"),
		`{"cwd":"/work/almanac","updatedAtMs":1787050800000}`+"\n", since)
	writeLookupFile(t, filepath.Join(home, ".cursor", "chats", "x", "cursor", "prompt_history.json"), `["one","two","three"]`, since)

	report := UsageSince(project, since)
	if got, want := report.Claude, (TokenUsage{Input: 10, CacheCreate: 20, CacheRead: 30, Output: 40, Thinking: 5, Messages: 1}); got != want {
		t.Fatalf("Claude = %#v, want %#v", got, want)
	}
	if got, want := report.Codex, (TokenUsage{Input: 50, CacheCreate: 5, CacheRead: 20, Output: 10, Thinking: 5, Messages: 1}); got != want {
		t.Fatalf("Codex = %#v, want %#v", got, want)
	}
	if report.Grok.Messages != 7 || report.Grok.Tokens() != 0 {
		t.Fatalf("Grok = %#v, want seven messages and no token claim", report.Grok)
	}
	if report.Cursor.Messages != 3 || report.Cursor.Tokens() != 0 {
		t.Fatalf("Cursor = %#v, want three prompt messages and no token claim", report.Cursor)
	}
}
