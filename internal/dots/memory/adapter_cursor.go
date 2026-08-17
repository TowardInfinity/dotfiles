package memory

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// CursorAdapter reads ~/.cursor/chats/<hash>/<uuid>/{meta.json,prompt_history.json}.
//
// It reads less than the plan assumed, on evidence rather than a guess:
// store.db in the same directory has a "blobs" table that looks like a
// content-addressed transcript store — each blob's id is literally the
// SHA-256 hash of its own bytes — but meta.json also carries a
// blobEncryptionKey, and the one blob inspected while building this adapter
// is genuinely encrypted with it, not merely stored in an undocumented shape.
// So there is no path to the assistant's side of the conversation here.
//
// What is left is real and plaintext: meta.json (Cursor's own title, cwd and
// timestamps) and prompt_history.json (a flat JSON array of the strings the
// user actually typed). That is enough for an honest, one-sided note.
// cursorConversation in conversation.go says so explicitly in what it hands
// to the summarizer, so a distilled note never reads as if it saw a reply
// that was never readable.
//
// One caveat this adapter cannot fully rule out from a single fixture:
// meta.json's updatedAtMs might only advance on a user turn rather than
// during assistant generation too, in which case a very long-running reply
// could look idle before the idle window (20 minutes by default) has really
// elapsed. --experimental only, and 20 minutes is generous for a Cursor
// response, so this is noted rather than blocking.
type CursorAdapter struct{}

func (CursorAdapter) Name() string { return "cursor" }

func (CursorAdapter) Available() bool {
	_, err := os.Stat(filepath.Join(home(), ".cursor", "chats"))
	return err == nil
}

type cursorMeta struct {
	Title       string `json:"title"`
	CWD         string `json:"cwd"`
	CreatedAtMs int64  `json:"createdAtMs"`
	UpdatedAtMs int64  `json:"updatedAtMs"`
}

func (a CursorAdapter) Scan() ([]Session, error) {
	root := filepath.Join(home(), ".cursor", "chats")

	var out []Session
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "meta.json" || denied(p) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		var m cursorMeta
		if json.Unmarshal(b, &m) != nil {
			return nil
		}

		dir := filepath.Dir(p)
		promptsPath := filepath.Join(dir, "prompt_history.json")
		prompts := readCursorPrompts(promptsPath)
		if len(prompts) == 0 {
			return nil // nothing a person typed here — nothing to remember
		}

		s := Session{
			Tool: a.Name(),
			// The uuid directory name, not anything inside meta.json — it
			// carries no session id field of its own.
			ID:         filepath.Base(dir),
			Title:      m.Title,
			CWD:        m.CWD,
			Transcript: promptsPath,
			Started:    time.UnixMilli(m.CreatedAtMs),
			Updated:    time.UnixMilli(m.UpdatedAtMs),
			// The only messages this adapter can see are the user's; an
			// assistant-inclusive count would be a number for data that was
			// never read.
			Messages: len(prompts),
		}
		if s.Title == "" {
			s.Title = FirstPrompt(prompts)
		}
		s.Project, s.GitRoot, s.Remote = ResolveProject(s.CWD)

		out = append(out, s)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// readCursorPrompts reads the flat JSON array of strings Cursor keeps
// alongside meta.json. A malformed or missing file yields no prompts rather
// than an error — one bad session should not fail the whole scan.
func readCursorPrompts(path string) []string {
	if denied(path) {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw []json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	var out []string
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil && s != "" {
			out = append(out, s)
		}
	}
	return out
}
