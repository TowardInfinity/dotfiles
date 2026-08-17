package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The Obsidian vault is the durable, human-readable tier. The index in
// ~/.cache is derived and disposable; this is the part that survives a laptop,
// travels via git and iCloud, and can be read by a person with no tooling.

// vaultEnv overrides vault discovery. install.sh sets it per machine, which is
// what keeps a /Users/towardinfinity path out of the binary — the mistake the
// Python hook made and install.sh:336 warns about.
const vaultEnv = "DOTS_MEMORY_VAULT"

// defaultVaultRel is the main vault's location relative to home on macOS. It is
// a probe, not an assumption: if it is absent the vault is simply unconfigured
// and capture keeps the index without writing notes.
var defaultVaultRel = filepath.Join(
	"Library", "Mobile Documents", "iCloud~md~obsidian", "Documents", "Obsidian Notes",
)

// VaultDir returns the "AI Chats" root, or "" when no vault is configured on
// this machine. An absent vault is a normal state on a Linux fleet box.
func VaultDir() string {
	if v := strings.TrimSpace(os.Getenv(vaultEnv)); v != "" {
		return v
	}
	d := filepath.Join(home(), defaultVaultRel, "AI Chats")
	if st, err := os.Stat(d); err == nil && st.IsDir() {
		return d
	}
	return ""
}

// toolDir keeps each tool's notes in its own folder, matching the "Claude
// Chats" directory the existing 70 notes already live in.
func toolDir(tool string) string {
	switch tool {
	case "claude-code":
		return "Claude Chats"
	case "codex":
		return "Codex Chats"
	case "grok":
		return "Grok Chats"
	case "cursor":
		return "Cursor Chats"
	case "chatgpt":
		return "ChatGPT Chats"
	}
	return "Other Chats"
}

// shortID is the 8-character session prefix used in filenames and frontmatter,
// carried over from the Python hook so existing notes keep matching.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// noteDate is the session's start date, falling back to last-updated.
//
// This is the fix for the duplicate-note bug. The Python hook dated notes with
// *today*, so a session spanning three days produced three files — and since
// Stop fires every assistant turn, each was rewritten dozens of times. The
// vault holds 70 notes for 39 sessions because of it. Keying on the start date
// makes the path a stable function of the session, so a re-capture overwrites
// its own note instead of creating another one.
func noteDate(s Session) time.Time {
	if !s.Started.IsZero() {
		return s.Started
	}
	return s.Updated
}

// NotePath is where a session's note belongs. Same session, same path, always.
func NotePath(vault string, s Session) string {
	name := fmt.Sprintf("%s %s", noteDate(s).Format("2006-01-02"), shortID(s.ID))
	if t := SafeFilename(s.Title); t != "" {
		name += " - " + t
	}
	return filepath.Join(vault, toolDir(s.Tool), name+".md")
}

// WriteNote writes the distilled note for a session.
//
// body is a Clean because everything written here is synced to iCloud and
// committed to a git repo; what reaches the vault is effectively published to
// every machine on the account.
func WriteNote(vault string, s Session, body Clean) (string, error) {
	path := NotePath(vault, s)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("date: " + noteDate(s).Format("2006-01-02") + "\n")
	b.WriteString("updated: " + s.Updated.Format("2006-01-02 15:04") + "\n")
	b.WriteString("tool: " + yamlValue(s.Tool) + "\n")
	// project is the key the whole feature turns on: it is what makes "what
	// have I done in this repo" answerable across tools and across clones.
	b.WriteString("project: " + yamlValue(string(s.Project)) + "\n")
	if s.Remote != "" {
		b.WriteString("remote: " + yamlValue(s.Remote) + "\n")
	}
	if s.GitRoot != "" {
		b.WriteString("git_root: " + yamlValue(s.GitRoot) + "\n")
	}
	if s.Branch != "" {
		b.WriteString("branch: " + yamlValue(s.Branch) + "\n")
	}
	if s.CWD != "" {
		b.WriteString("cwd: " + yamlValue(s.CWD) + "\n")
	}
	b.WriteString("session_id: " + shortID(s.ID) + "\n")
	b.WriteString("session: " + yamlValue(s.ID) + "\n")
	b.WriteString("transcript: " + yamlValue(s.Transcript) + "\n")
	b.WriteString("messages: " + fmt.Sprint(s.Messages) + "\n")
	b.WriteString("tags:\n  - ai-chat\n  - summary\n  - " + yamlValue(s.Tool) + "\n")
	b.WriteString("---\n\n")

	title := s.Title
	if title == "" {
		title = "Session " + shortID(s.ID)
	}
	b.WriteString("# " + title + "\n\n")
	b.WriteString(strings.TrimSpace(string(body)))
	b.WriteString("\n")

	if err := writeFileAtomic(path, []byte(b.String()), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// yamlValue quotes a scalar when it would otherwise change the meaning of the
// line. Titles are user text and routinely contain a colon, which silently
// turns one frontmatter field into a broken map.
func yamlValue(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, `:#"'{}[],&*?|<>=!%@`+"`") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
	}
	return s
}
