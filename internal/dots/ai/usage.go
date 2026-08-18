package ai

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TowardInfinity/dotfiles/internal/dots/memory"
)

// TokenUsage is activity observed in local transcripts. It deliberately makes
// no claim about subscription allowance, resets, or web activity.
type TokenUsage struct {
	Input       int64
	CacheCreate int64
	CacheRead   int64
	Output      int64
	Thinking    int64
	Messages    int
}

func (u *TokenUsage) add(v TokenUsage) {
	u.Input += v.Input
	u.CacheCreate += v.CacheCreate
	u.CacheRead += v.CacheRead
	u.Output += v.Output
	u.Thinking += v.Thinking
	u.Messages += v.Messages
}

func (u TokenUsage) Tokens() int64 {
	return u.Input + u.CacheCreate + u.CacheRead + u.Output
}

// UsageReport contains local activity by tool for a time window. Grok and
// Cursor expose message counts only; their zero token totals are intentional.
type UsageReport struct {
	Since  time.Time
	Claude TokenUsage
	Codex  TokenUsage
	Grok   TokenUsage
	Cursor TokenUsage
}

func (r UsageReport) Tool(name string) TokenUsage {
	switch name {
	case "claude":
		return r.Claude
	case "codex":
		return r.Codex
	case "grok":
		return r.Grok
	case "cursor":
		return r.Cursor
	default:
		return TokenUsage{}
	}
}

// UsageSince reads local component activity after since. It does not read the
// memory index, which is derived data and may be unavailable during capture.
func UsageSince(project memory.ProjectKey, since time.Time) UsageReport {
	return UsageReport{
		Since:  since,
		Claude: claudeUsage(project, since),
		Codex:  codexUsage(project, since),
		Grok:   grokUsage(project, since),
		Cursor: cursorUsage(project, since),
	}
}

func claudeUsage(project memory.ProjectKey, since time.Time) TokenUsage {
	root := filepath.Join(lookupHome(), ".claude", "projects")
	var total TokenUsage
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		total.add(readClaudeUsage(path, project, since))
		return nil
	})
	return total
}

func readClaudeUsage(path string, project memory.ProjectKey, since time.Time) TokenUsage {
	f, err := os.Open(path)
	if err != nil {
		return TokenUsage{}
	}
	defer func() { _ = f.Close() }()
	type usage struct {
		Input       int64 `json:"input_tokens"`
		CacheCreate int64 `json:"cache_creation_input_tokens"`
		CacheRead   int64 `json:"cache_read_input_tokens"`
		Output      int64 `json:"output_tokens"`
		Details     struct {
			Thinking int64 `json:"thinking_tokens"`
		} `json:"output_tokens_details"`
	}
	var cwd string
	var events []struct {
		at time.Time
		u  usage
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var line struct {
			CWD       string `json:"cwd"`
			Timestamp string `json:"timestamp"`
			Message   struct {
				Usage *usage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		if cwd == "" && line.CWD != "" {
			cwd = line.CWD
		}
		if line.Message.Usage == nil {
			continue
		}
		at, err := time.Parse(time.RFC3339, line.Timestamp)
		if err == nil {
			events = append(events, struct {
				at time.Time
				u  usage
			}{at, *line.Message.Usage})
		}
	}
	if !matchesProject(cwd, "", project) {
		return TokenUsage{}
	}
	var total TokenUsage
	for _, event := range events {
		if event.at.Before(since) {
			continue
		}
		total.add(TokenUsage{Input: event.u.Input, CacheCreate: event.u.CacheCreate, CacheRead: event.u.CacheRead, Output: event.u.Output, Thinking: event.u.Details.Thinking, Messages: 1})
	}
	return total
}

func codexUsage(project memory.ProjectKey, since time.Time) TokenUsage {
	root := filepath.Join(lookupHome(), ".codex", "sessions")
	var total TokenUsage
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasPrefix(d.Name(), "rollout-") || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		total.add(readCodexUsage(path, project, since))
		return nil
	})
	return total
}

func readCodexUsage(path string, project memory.ProjectKey, since time.Time) TokenUsage {
	f, err := os.Open(path)
	if err != nil {
		return TokenUsage{}
	}
	defer func() { _ = f.Close() }()
	type totals struct {
		Input      int64 `json:"input_tokens"`
		CacheRead  int64 `json:"cached_input_tokens"`
		CacheWrite int64 `json:"cache_write_input_tokens"`
		Output     int64 `json:"output_tokens"`
		Thinking   int64 `json:"reasoning_output_tokens"`
	}
	type snapshot struct {
		at time.Time
		u  totals
	}
	var cwd string
	var snapshots []snapshot
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var line struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				CWD  string `json:"cwd"`
				Info struct {
					Total *totals `json:"total_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		if line.Type == "session_meta" && cwd == "" {
			cwd = line.Payload.CWD
		}
		if line.Type != "event_msg" || line.Payload.Info.Total == nil {
			continue
		}
		at, err := time.Parse(time.RFC3339, line.Timestamp)
		if err == nil {
			snapshots = append(snapshots, snapshot{at, *line.Payload.Info.Total})
		}
	}
	if !matchesProject(cwd, "", project) {
		return TokenUsage{}
	}
	var total TokenUsage
	for i := 1; i < len(snapshots); i++ {
		current, previous := snapshots[i], snapshots[i-1]
		if current.at.Before(since) {
			continue
		}
		total.add(TokenUsage{
			Input:       positiveDelta(current.u.Input, previous.u.Input),
			CacheRead:   positiveDelta(current.u.CacheRead, previous.u.CacheRead),
			CacheCreate: positiveDelta(current.u.CacheWrite, previous.u.CacheWrite),
			Output:      positiveDelta(current.u.Output, previous.u.Output),
			Thinking:    positiveDelta(current.u.Thinking, previous.u.Thinking),
			Messages:    1,
		})
	}
	return total
}

func positiveDelta(now, before int64) int64 {
	if now <= before {
		return 0
	}
	return now - before
}

func grokUsage(project memory.ProjectKey, since time.Time) TokenUsage {
	var total TokenUsage
	for _, ref := range grokUsageSessions(project, since) {
		total.Messages += ref.Messages
	}
	return total
}

type messageSession struct{ Messages int }

func grokUsageSessions(project memory.ProjectKey, since time.Time) []messageSession {
	root := filepath.Join(lookupHome(), ".grok", "sessions")
	var out []messageSession
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "summary.json" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var summary struct {
			Info struct {
				CWD string `json:"cwd"`
			} `json:"info"`
			UpdatedAt       time.Time `json:"updated_at"`
			NumChatMessages int       `json:"num_chat_messages"`
			NumMessages     int       `json:"num_messages"`
			GitRemotes      []string  `json:"git_remotes"`
		}
		if json.Unmarshal(b, &summary) != nil || summary.UpdatedAt.Before(since) {
			return nil
		}
		remote := ""
		if len(summary.GitRemotes) > 0 {
			remote = summary.GitRemotes[0]
		}
		if !matchesProject(summary.Info.CWD, remote, project) {
			return nil
		}
		messages := summary.NumChatMessages
		if messages == 0 {
			messages = summary.NumMessages
		}
		out = append(out, messageSession{messages})
		return nil
	})
	return out
}

func cursorUsage(project memory.ProjectKey, since time.Time) TokenUsage {
	root := filepath.Join(lookupHome(), ".cursor", "chats")
	var total TokenUsage
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "meta.json" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var meta struct {
			CWD         string `json:"cwd"`
			UpdatedAtMs int64  `json:"updatedAtMs"`
		}
		if json.Unmarshal(b, &meta) != nil || time.UnixMilli(meta.UpdatedAtMs).Before(since) || !matchesProject(meta.CWD, "", project) {
			return nil
		}
		prompts, err := os.ReadFile(filepath.Join(filepath.Dir(path), "prompt_history.json"))
		if err != nil {
			return nil
		}
		var entries []string
		if json.Unmarshal(prompts, &entries) == nil {
			total.Messages += len(entries)
		}
		return nil
	})
	return total
}
