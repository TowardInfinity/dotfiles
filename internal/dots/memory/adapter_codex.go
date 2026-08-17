package memory

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CodexAdapter reads ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl.
//
// The first line is a session_meta whose payload carries everything needed, so
// unlike the Claude adapter this one rarely reads past the head of the file.
type CodexAdapter struct{}

func (CodexAdapter) Name() string { return "codex" }

func (CodexAdapter) Available() bool {
	_, err := os.Stat(filepath.Join(home(), ".codex", "sessions"))
	return err == nil
}

type codexLine struct {
	Type    string `json:"type"`
	Payload struct {
		SessionID    string `json:"session_id"`
		ID           string `json:"id"`
		CWD          string `json:"cwd"`
		Timestamp    string `json:"timestamp"`
		ThreadSource string `json:"thread_source"`
		CLIVersion   string `json:"cli_version"`

		// response_item fields. PayloadType distinguishes a conversation turn
		// from the reasoning traces and tool calls that share the wrapper.
		PayloadType string `json:"type"`
		Role        string `json:"role"`
		Content     []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"payload"`
}

func (a CodexAdapter) Scan() ([]Session, error) {
	root := filepath.Join(home(), ".codex", "sessions")

	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable day directory skips
		}
		if d.IsDir() || !strings.HasPrefix(d.Name(), "rollout-") || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		if !denied(p) {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var out []Session
	for _, p := range paths {
		if s, ok := a.readSession(p); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func (a CodexAdapter) readSession(path string) (Session, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, false
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return Session{}, false
	}

	s := Session{Tool: a.Name(), Transcript: path, Updated: info.ModTime()}
	var prompts []string

	sc := bufio.NewScanner(f)
	// session_meta embeds the agent's full base instructions, which for a
	// guardian subagent is several thousand words on one line.
	sc.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)

	for sc.Scan() {
		var line codexLine
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		switch line.Type {
		case "session_meta":
			// Subagent rollouts — guardian review passes and the like — are
			// machinery, not conversation. They outnumber real sessions and
			// summarizing them would bury the sessions worth remembering.
			if line.Payload.ThreadSource == "subagent" {
				return Session{}, false
			}
			s.ID = line.Payload.ID
			if s.ID == "" {
				s.ID = line.Payload.SessionID
			}
			s.CWD = line.Payload.CWD
			if t, err := time.Parse(time.RFC3339, line.Payload.Timestamp); err == nil {
				s.Started = t
			}
		case "response_item":
			// response_item also wraps reasoning traces and tool calls, and
			// counting those reported sessions of 20,000 "messages". Only a
			// message payload is a conversation turn, and the developer role
			// is injected policy, not either party talking.
			if line.Payload.PayloadType != "message" {
				continue
			}
			if line.Payload.Role != "user" && line.Payload.Role != "assistant" {
				continue
			}
			s.Messages++
			if line.Payload.Role == "user" && len(line.Payload.Content) > 0 {
				prompts = append(prompts, line.Payload.Content[0].Text)
			}
		}
	}

	// Codex writes no title of its own, so the first message that a person
	// actually typed stands in for one.
	s.Title = FirstPrompt(prompts)

	if s.ID == "" || s.Messages == 0 {
		return Session{}, false
	}
	s.Project, s.GitRoot, s.Remote = ResolveProject(s.CWD)
	return s, true
}
