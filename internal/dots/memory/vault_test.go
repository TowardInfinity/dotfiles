package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testSession() Session {
	return Session{
		ID:         "cb3d2775-5259-447e-9713-09cccb7844e9",
		Tool:       "claude-code",
		Project:    "github.com/TowardInfinity/dotfiles",
		Title:      "global ai memory",
		Remote:     "git@github.com:TowardInfinity/dotfiles.git",
		GitRoot:    "/Users/x/Codes/dotfiles",
		Branch:     "main",
		CWD:        "/Users/x/Codes/dotfiles",
		Transcript: "/Users/x/.claude/projects/p/cb3d2775.jsonl",
		Messages:   109,
		Started:    time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC),
		Updated:    time.Date(2026, 8, 17, 18, 39, 0, 0, time.UTC),
	}
}

// This is the duplicate-note bug, as a test. The Python hook dated notes with
// *today*, so a session spanning three days produced three files — and because
// Stop fires every assistant turn, each was rewritten dozens of times. The
// vault holds 70 notes for 39 sessions as a result. The path must be a function
// of the session, not of when capture happened to run.
func TestNotePathIsStableAcrossDays(t *testing.T) {
	s := testSession()
	first := NotePath("/vault", s)

	// Same session, captured a week later after more conversation.
	s.Updated = s.Updated.Add(7 * 24 * time.Hour)
	s.Messages = 240
	if got := NotePath("/vault", s); got != first {
		t.Errorf("note path moved with capture time:\n  %s\n  %s", first, got)
	}
}

func TestNotePathPerTool(t *testing.T) {
	s := testSession()
	for tool, dir := range map[string]string{
		"claude-code": "Claude Chats",
		"codex":       "Codex Chats",
		"grok":        "Grok Chats",
		"something":   "Other Chats",
	} {
		s.Tool = tool
		if got := filepath.Base(filepath.Dir(NotePath("/vault", s))); got != dir {
			t.Errorf("tool %q filed under %q, want %q", tool, got, dir)
		}
	}
}

func TestWriteNoteFrontmatter(t *testing.T) {
	vault := t.TempDir()
	s := testSession()

	path, err := WriteNote(vault, s, Clean("Did the thing.\n\n- a bullet"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)

	// project is the key the entire feature turns on.
	if !strings.Contains(got, "project: github.com/TowardInfinity/dotfiles") {
		t.Errorf("missing project frontmatter:\n%s", got)
	}
	for _, want := range []string{"date: 2026-08-10", "tool: claude-code", "session_id: cb3d2775", "messages: 109"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "Did the thing.") {
		t.Errorf("body missing:\n%s", got)
	}

	// A colon in a remote would otherwise turn one frontmatter line into a
	// broken map and take the whole note's metadata with it.
	if !strings.Contains(got, `remote: "git@github.com:TowardInfinity/dotfiles.git"`) {
		t.Errorf("remote not quoted:\n%s", got)
	}

	// Written twice, the note is replaced rather than duplicated.
	if _, err := WriteNote(vault, s, Clean("Did the thing, again.")); err != nil {
		t.Fatal(err)
	}
	var count int
	_ = filepath.Walk(vault, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && strings.HasSuffix(p, ".md") {
			count++
		}
		return nil
	})
	if count != 1 {
		t.Errorf("second write produced %d notes, want 1", count)
	}
}

// Titles are user text; a title that is entirely punctuation must not produce a
// note named ".md" or escape its directory.
func TestWriteNoteAwkwardTitle(t *testing.T) {
	vault := t.TempDir()
	s := testSession()
	s.Title = "../../etc/passwd: ///"

	path, err := WriteNote(vault, s, Clean("body"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(vault)) {
		t.Errorf("note escaped the vault: %s", path)
	}
	if strings.Contains(filepath.Base(path), "..") {
		t.Errorf("traversal survived into the filename: %s", filepath.Base(path))
	}
}

func TestVaultDirEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(vaultEnv, dir)
	if got := VaultDir(); got != dir {
		t.Errorf("VaultDir() = %q, want %q", got, dir)
	}
}
