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
// Cursor record session timestamps but not timestamps for individual messages,
// so their message counts are intentionally omitted from bounded windows.
type UsageReport struct {
	Since                           time.Time
	Claude                          TokenUsage
	Codex                           TokenUsage
	GrokSessionsWithoutTimestamps   int
	CursorSessionsWithoutTimestamps int
}

func (r UsageReport) Tool(name string) TokenUsage {
	switch name {
	case "claude":
		return r.Claude
	case "codex":
		return r.Codex
	default:
		return TokenUsage{}
	}
}

// UsageSince reads local component activity after since. It does not read the
// memory index, which is derived data and may be unavailable during capture.
func UsageSince(project memory.ProjectKey, since time.Time) UsageReport {
	return UsageForWindows(project, []time.Time{since})[0]
}

// UsageForWindows scans every local transcript once and computes a report for
// each cutoff. It avoids doubling the filesystem walk for the normal 5h + 7d
// display while keeping each report's time boundary explicit.
func UsageForWindows(project memory.ProjectKey, cutoffs []time.Time) []UsageReport {
	reports := make([]UsageReport, len(cutoffs))
	for i, since := range cutoffs {
		reports[i].Since = since
	}
	claudeUsage(project, reports)
	codexUsage(project, reports)
	markGrokSessionsWithoutMessageTimestamps(project, reports)
	markCursorSessionsWithoutMessageTimestamps(project, reports)
	return reports
}

func claudeUsage(project memory.ProjectKey, reports []UsageReport) {
	root := filepath.Join(lookupHome(), ".claude", "projects")
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		readClaudeUsage(path, project, reports)
		return nil
	})
}

func readClaudeUsage(path string, project memory.ProjectKey, reports []UsageReport) {
	f, err := os.Open(path)
	if err != nil {
		return
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
		return
	}
	for _, event := range events {
		for i := range reports {
			if event.at.Before(reports[i].Since) {
				continue
			}
			reports[i].Claude.add(TokenUsage{Input: event.u.Input, CacheCreate: event.u.CacheCreate, CacheRead: event.u.CacheRead, Output: event.u.Output, Thinking: event.u.Details.Thinking, Messages: 1})
		}
	}
}

func codexUsage(project memory.ProjectKey, reports []UsageReport) {
	root := filepath.Join(lookupHome(), ".codex", "sessions")
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasPrefix(d.Name(), "rollout-") || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		readCodexUsage(path, project, reports)
		return nil
	})
}

func readCodexUsage(path string, project memory.ProjectKey, reports []UsageReport) {
	f, err := os.Open(path)
	if err != nil {
		return
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
	var id, cwd string
	var messages int
	var snapshots []snapshot
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var line struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				ID           string `json:"id"`
				SessionID    string `json:"session_id"`
				CWD          string `json:"cwd"`
				ThreadSource string `json:"thread_source"`
				PayloadType  string `json:"type"`
				Role         string `json:"role"`
				Info         struct {
					Total *totals `json:"total_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		if line.Type == "session_meta" {
			// Do not let a guardian/subagent rollout affect the user's usage or
			// cross-tool resume choice. See the matching metadata lookup guard.
			if line.Payload.ThreadSource == "subagent" {
				return
			}
			id = line.Payload.ID
			if id == "" {
				id = line.Payload.SessionID
			}
			cwd = line.Payload.CWD
		}
		if line.Type == "response_item" && line.Payload.PayloadType == "message" && (line.Payload.Role == "user" || line.Payload.Role == "assistant") {
			messages++
		}
		if line.Type != "event_msg" || line.Payload.Info.Total == nil {
			continue
		}
		at, err := time.Parse(time.RFC3339, line.Timestamp)
		if err == nil {
			snapshots = append(snapshots, snapshot{at, *line.Payload.Info.Total})
		}
	}
	if id == "" || messages == 0 || !matchesProject(cwd, "", project) {
		return
	}
	for i := 1; i < len(snapshots); i++ {
		current, previous := snapshots[i], snapshots[i-1]
		for j := range reports {
			if current.at.Before(reports[j].Since) {
				continue
			}
			reports[j].Codex.add(TokenUsage{
				Input:       positiveDelta(current.u.Input, previous.u.Input),
				CacheRead:   positiveDelta(current.u.CacheRead, previous.u.CacheRead),
				CacheCreate: positiveDelta(current.u.CacheWrite, previous.u.CacheWrite),
				Output:      positiveDelta(current.u.Output, previous.u.Output),
				Thinking:    positiveDelta(current.u.Thinking, previous.u.Thinking),
				Messages:    1,
			})
		}
	}
}

func positiveDelta(now, before int64) int64 {
	if now <= before {
		return 0
	}
	return now - before
}

func markGrokSessionsWithoutMessageTimestamps(project memory.ProjectKey, reports []UsageReport) {
	root := filepath.Join(lookupHome(), ".grok", "sessions")
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
			UpdatedAt  time.Time `json:"updated_at"`
			GitRemotes []string  `json:"git_remotes"`
		}
		if json.Unmarshal(b, &summary) != nil {
			return nil
		}
		updated := summary.UpdatedAt
		if updated.IsZero() {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			updated = info.ModTime()
		}
		remote := ""
		if len(summary.GitRemotes) > 0 {
			remote = summary.GitRemotes[0]
		}
		if !matchesProject(summary.Info.CWD, remote, project) {
			return nil
		}
		for i := range reports {
			if !updated.Before(reports[i].Since) {
				reports[i].GrokSessionsWithoutTimestamps++
			}
		}
		return nil
	})
}

func markCursorSessionsWithoutMessageTimestamps(project memory.ProjectKey, reports []UsageReport) {
	root := filepath.Join(lookupHome(), ".cursor", "chats")
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
		if json.Unmarshal(b, &meta) != nil || !matchesProject(meta.CWD, "", project) {
			return nil
		}
		updated := time.UnixMilli(meta.UpdatedAtMs)
		if meta.UpdatedAtMs == 0 {
			info, err := d.Info()
			if err != nil {
				return nil
			}
			updated = info.ModTime()
		}
		for i := range reports {
			if !updated.Before(reports[i].Since) {
				reports[i].CursorSessionsWithoutTimestamps++
			}
		}
		return nil
	})
}
