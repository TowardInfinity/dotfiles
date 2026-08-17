package memory

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GrokAdapter reads ~/.grok/sessions/<percent-encoded-cwd>/<uuid>/summary.json.
//
// The cheapest adapter by far: Grok already writes a structured per-session
// summary containing cwd, git root, remotes, head branch, message count and a
// generated title. Nothing needs parsing out of a transcript, and its schema is
// what Session is modelled on.
type GrokAdapter struct{}

func (GrokAdapter) Name() string { return "grok" }

func (GrokAdapter) Available() bool {
	_, err := os.Stat(filepath.Join(home(), ".grok", "sessions"))
	return err == nil
}

type grokSummary struct {
	Info struct {
		ID  string `json:"id"`
		CWD string `json:"cwd"`
	} `json:"info"`
	SessionSummary  string    `json:"session_summary"`
	GeneratedTitle  string    `json:"generated_title"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	NumChatMessages int       `json:"num_chat_messages"`
	NumMessages     int       `json:"num_messages"`
	GitRootDir      string    `json:"git_root_dir"`
	GitRemotes      []string  `json:"git_remotes"`
	HeadBranch      string    `json:"head_branch"`
}

func (a GrokAdapter) Scan() ([]Session, error) {
	root := filepath.Join(home(), ".grok", "sessions")

	var out []Session
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "summary.json" || denied(p) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		var g grokSummary
		if json.Unmarshal(b, &g) != nil || g.Info.ID == "" {
			return nil
		}

		s := Session{
			Tool:       a.Name(),
			ID:         g.Info.ID,
			Title:      g.GeneratedTitle,
			CWD:        g.Info.CWD,
			GitRoot:    g.GitRootDir,
			Branch:     g.HeadBranch,
			Started:    g.CreatedAt,
			Updated:    g.UpdatedAt,
			Messages:   g.NumChatMessages,
			Transcript: filepath.Dir(p),
		}
		// Grok writes its own session summary, so this adapter is the one tool
		// whose notes cost no Ollama call at all. Taking it here means the
		// distillation pass sees these sessions as already done.
		s.Summary = strings.TrimSpace(g.SessionSummary)
		if s.Title == "" {
			s.Title = TitleLine(g.SessionSummary)
		}
		if s.Messages == 0 {
			s.Messages = g.NumMessages
		}

		// Grok records the remotes directly, so the project key comes from its
		// own data rather than from shelling out to git — which also means it
		// still resolves correctly for a directory that has since been moved
		// or deleted.
		if len(g.GitRemotes) > 0 {
			if k := NormalizeRemote(g.GitRemotes[0]); k != "" {
				s.Project, s.Remote = ProjectKey(k), g.GitRemotes[0]
			}
		}
		if s.Project == "" {
			s.Project, s.GitRoot, s.Remote = ResolveProject(s.CWD)
		}

		out = append(out, s)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
