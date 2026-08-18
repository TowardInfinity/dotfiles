package ai

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/TowardInfinity/dotfiles/internal/dots/memory"
)

// These seams keep the lookup tests hermetic. Production uses the same home
// and project resolution as memory, but lookup never opens its index or takes
// its capture lock.
var lookupHome = func() string {
	h, _ := os.UserHomeDir()
	return h
}

var lookupProject = func(dir string) memory.ProjectKey {
	key, _, _ := memory.ResolveProject(dir)
	return key
}

// RecentSessions returns the locally touched sessions for project, newest
// first. It reads only enough session metadata to establish a project key and
// modification time; the memory index is optional enrichment for the console,
// never a dependency for this result.
func RecentSessions(project memory.ProjectKey) []SessionRef {
	bySession := map[string]SessionRef{}
	for _, tool := range tools {
		for _, ref := range sessionsForTool(tool, project) {
			key := ref.Tool + "\x00" + ref.ID
			if prior, ok := bySession[key]; !ok || ref.Updated.After(prior.Updated) {
				bySession[key] = ref
			}
		}
	}
	all := make([]SessionRef, 0, len(bySession))
	for _, ref := range bySession {
		all = append(all, ref)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Updated.After(all[j].Updated) })
	return all
}

// LastTouched reports tool's newest local session for project.
func LastTouched(tool string, project memory.ProjectKey) (string, time.Time) {
	return lastTouched(tool, project)
}

func lastTouched(tool string, project memory.ProjectKey) (string, time.Time) {
	t, ok := toolForAlias(tool)
	if !ok {
		return "", time.Time{}
	}
	refs := sessionsForTool(t, project)
	if len(refs) == 0 {
		return "", time.Time{}
	}
	best := refs[0]
	for _, ref := range refs[1:] {
		if ref.Updated.After(best.Updated) {
			best = ref
		}
	}
	return best.ID, best.Updated
}

// BestTool returns the tool most recently used in project. It is deliberately
// based on on-disk metadata rather than memory.LoadIndex so a missing index,
// live capture pass, or unavailable Ollama cannot stop a resume.
func BestTool(project memory.ProjectKey) (tool, ref string, at time.Time, ok bool) {
	return bestTool(project)
}

func bestTool(project memory.ProjectKey) (tool, ref string, at time.Time, ok bool) {
	for _, candidate := range RecentSessions(project) {
		return candidate.Tool, candidate.ID, candidate.Updated, true
	}
	return "", "", time.Time{}, false
}

func sessionsForTool(tool Tool, project memory.ProjectKey) []SessionRef {
	switch tool.Alias {
	case "claude":
		return claudeSessions(project)
	case "codex":
		return codexSessions(project)
	case "grok":
		return grokSessions(project)
	case "cursor":
		return cursorSessions(project)
	default:
		return nil
	}
}

func claudeSessions(project memory.ProjectKey) []SessionRef {
	root := filepath.Join(lookupHome(), ".claude", "projects")
	var out []SessionRef
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		id, cwd := claudeSessionMeta(path)
		if id == "" || !matchesProject(cwd, "", project) {
			return nil
		}
		if info, err := d.Info(); err == nil {
			out = append(out, SessionRef{Tool: "claude", ID: id, Updated: info.ModTime()})
		}
		return nil
	})
	return out
}

func claudeSessionMeta(path string) (id, cwd string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var line struct {
			SessionID string `json:"sessionId"`
			CWD       string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		if id == "" {
			id = line.SessionID
		}
		if cwd == "" {
			cwd = line.CWD
		}
		if id != "" && cwd != "" {
			return id, cwd
		}
	}
	return id, cwd
}

func codexSessions(project memory.ProjectKey) []SessionRef {
	root := filepath.Join(lookupHome(), ".codex", "sessions")
	var out []SessionRef
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasPrefix(d.Name(), "rollout-") || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		id, cwd := codexSessionMeta(path)
		if id == "" || !matchesProject(cwd, "", project) {
			return nil
		}
		if info, err := d.Info(); err == nil {
			out = append(out, SessionRef{Tool: "codex", ID: id, Updated: info.ModTime()})
		}
		return nil
	})
	return out
}

func codexSessionMeta(path string) (id, cwd string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var line struct {
			Type    string `json:"type"`
			Payload struct {
				ID        string `json:"id"`
				SessionID string `json:"session_id"`
				CWD       string `json:"cwd"`
			} `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "session_meta" {
			continue
		}
		id = line.Payload.ID
		if id == "" {
			id = line.Payload.SessionID
		}
		return id, line.Payload.CWD
	}
	return "", ""
}

func grokSessions(project memory.ProjectKey) []SessionRef {
	root := filepath.Join(lookupHome(), ".grok", "sessions")
	var out []SessionRef
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
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			} `json:"info"`
			UpdatedAt  time.Time `json:"updated_at"`
			GitRemotes []string  `json:"git_remotes"`
		}
		if json.Unmarshal(b, &summary) != nil || summary.Info.ID == "" {
			return nil
		}
		remote := ""
		if len(summary.GitRemotes) > 0 {
			remote = summary.GitRemotes[0]
		}
		if !matchesProject(summary.Info.CWD, remote, project) {
			return nil
		}
		updated := summary.UpdatedAt
		if updated.IsZero() {
			if info, err := d.Info(); err == nil {
				updated = info.ModTime()
			}
		}
		out = append(out, SessionRef{Tool: "grok", ID: summary.Info.ID, Updated: updated})
		return nil
	})
	return out
}

func cursorSessions(project memory.ProjectKey) []SessionRef {
	root := filepath.Join(lookupHome(), ".cursor", "chats")
	var out []SessionRef
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
			if info, err := d.Info(); err == nil {
				updated = info.ModTime()
			}
		}
		out = append(out, SessionRef{Tool: "cursor", ID: filepath.Base(filepath.Dir(path)), Updated: updated})
		return nil
	})
	return out
}

func matchesProject(cwd, remote string, project memory.ProjectKey) bool {
	if project == "" {
		return false
	}
	if remote != "" && memory.ProjectKey(memory.NormalizeRemote(remote)) == project {
		return true
	}
	return cwd != "" && lookupProject(cwd) == project
}
