package memory

import (
	"sort"
	"time"
)

// Session is one conversation with one tool, after distillation.
//
// The shape is Grok's summary.json rather than an invention of this package:
// Grok already writes cwd, git root, remotes, head branch, message count and a
// generated title per session, which is exactly the set needed here. Adopting
// it makes that adapter near-parsing-free and gives the others a target that is
// known to be sufficient in practice.
//
// The raw transcript is never copied — only Transcript, a path back to it.
type Session struct {
	ID      string     `json:"id"`   // tool-native session id
	Tool    string     `json:"tool"` // "claude-code", "codex", "grok", ...
	Project ProjectKey `json:"project"`

	Title   string `json:"title,omitempty"`   // one line, for a human scanning
	Summary string `json:"summary,omitempty"` // the distilled note body

	CWD        string `json:"cwd,omitempty"`
	GitRoot    string `json:"git_root,omitempty"`
	Remote     string `json:"remote,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Transcript string `json:"transcript,omitempty"` // path, not contents

	Started  time.Time `json:"started_at"`
	Updated  time.Time `json:"updated_at"`
	Messages int       `json:"messages"`

	// VaultNote is the full path to the Obsidian note, once written. Empty
	// means indexed but not yet distilled.
	VaultNote string `json:"vault_note,omitempty"`

	// Digest is the byte size of the transcript at last capture. Capture
	// compares against it to skip sessions that have not meaningfully grown —
	// the Stop hook fires every assistant turn, and re-summarizing the whole
	// transcript each time is what the Python original did.
	CapturedBytes int64 `json:"captured_bytes,omitempty"`

	// Trivial records that this session was read at CapturedBytes and judged
	// too slight to deserve a note. Without it there is no way to tell "not
	// looked at yet" from "looked at, nothing there": both have an empty
	// Summary, so every abandoned two-message session would be re-read from
	// disk on every capture pass forever. With ~120 sessions and Codex
	// rollouts running to tens of thousands of lines, that is the per-turn
	// cost bug wearing a different hat.
	Trivial bool `json:"trivial,omitempty"`
}

// Recent returns sessions for a project, newest first, at most n.
func Recent(all []Session, project ProjectKey, n int) []Session {
	var out []Session
	for _, s := range all {
		if s.Project == project {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Updated.After(out[j].Updated)
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
