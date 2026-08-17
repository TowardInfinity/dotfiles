package memory

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ClaudeAdapter reads ~/.claude/projects/<slug>/<session-uuid>.jsonl.
//
// The slug is a mangled form of the directory Claude was *launched* in, which
// is not necessarily where the work happened: a session filed under
// "-Users-towardinfinity-Codes" has cwd /Users/towardinfinity/Codes/Projects/
// nse-equity-data on its message lines. So the slug is ignored entirely and cwd
// is read from the transcript.
type ClaudeAdapter struct{}

func (ClaudeAdapter) Name() string { return "claude-code" }

func (ClaudeAdapter) Available() bool {
	_, err := os.Stat(filepath.Join(home(), ".claude", "projects"))
	return err == nil
}

// claudeLine is the subset of a transcript line this package reads. Claude
// writes many line types — mode, ai-title, last-prompt, attachment,
// file-history-delta, system — and only a few carry what is wanted here.
type claudeLine struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	GitBranch string `json:"gitBranch"`
	Timestamp string `json:"timestamp"`
	AITitle   string `json:"aiTitle"`

	// Sidechain lines are subagent turns. They are part of the session's work
	// but not of its conversation, and counting them inflates the message
	// count that decides whether a session is worth a note.
	IsSidechain bool `json:"isSidechain"`

	// ToolUseResult is set on user-role lines that are actually the output of
	// a tool call being fed back. They are the bulk of a long session's user
	// lines — counting them reported 4,050 "messages" for a conversation with
	// a few dozen turns.
	ToolUseResult json.RawMessage `json:"toolUseResult"`

	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// contentText pulls the text blocks out of a message's content, which is
// either a bare string or an array of typed blocks depending on the line.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (a ClaudeAdapter) Scan() ([]Session, error) {
	root := filepath.Join(home(), ".claude", "projects")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var out []Session
	for _, dir := range entries {
		if !dir.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, dir.Name()))
		if err != nil {
			continue // an unreadable project dir skips, it does not fail the scan
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(root, dir.Name(), f.Name())
			if denied(path) {
				continue
			}
			s, ok := a.readSession(path)
			if ok {
				out = append(out, s)
			}
		}
	}
	return out, nil
}

// readSession pulls metadata from one transcript. It streams rather than
// loading: these files reach tens of megabytes and reindex touches all of them.
func (a ClaudeAdapter) readSession(path string) (Session, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, false
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return Session{}, false
	}

	s := Session{
		Tool:       a.Name(),
		ID:         strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		Transcript: path,
		Updated:    info.ModTime(),
	}
	var prompts []string

	sc := bufio.NewScanner(f)
	// A single line holds a whole assistant turn and blows the 64K default.
	sc.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)

	for sc.Scan() {
		var line claudeLine
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue // a partial trailing line on a live session is expected
		}
		// The ai-title line is Claude's own summary of the session. It is
		// better than anything derivable from the first user message, and it
		// is free — the model already paid for it.
		if line.Type == "ai-title" && line.AITitle != "" {
			s.Title = line.AITitle
		}
		if line.CWD != "" && s.CWD == "" {
			s.CWD = line.CWD
		}
		if line.GitBranch != "" && s.Branch == "" {
			s.Branch = line.GitBranch
		}
		if line.Type != "user" && line.Type != "assistant" {
			continue
		}
		// Sidechain lines are subagent turns — part of the work, but not of
		// this conversation.
		if line.IsSidechain {
			continue
		}
		text := contentText(line.Message.Content)
		if line.Type == "user" {
			// Tool output and injected context both arrive wearing the user
			// role; neither is the user saying something.
			if len(line.ToolUseResult) > 0 || IsInjected(text) {
				continue
			}
			prompts = append(prompts, text)
		} else if text == "" {
			continue // an assistant line carrying only a tool call
		}
		s.Messages++
		if s.Started.IsZero() && line.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, line.Timestamp); err == nil {
				s.Started = t
			}
		}
	}

	// ai-title is Claude's own, and better than anything derived here. The
	// first real prompt only stands in when the session ended before one was
	// generated.
	if s.Title == "" {
		s.Title = FirstPrompt(prompts)
	}

	if s.Messages == 0 {
		return Session{}, false
	}
	s.Project, s.GitRoot, s.Remote = ResolveProject(s.CWD)
	return s, true
}
