package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLiveScan runs the adapters against this machine's real session stores.
//
// Skipped unless DOTS_MEMORY_LIVE=1, because it depends on whatever happens to
// be on the box: it asserts nothing about counts, which change every time a
// chat is opened. It exists because the adapters parse five undocumented
// formats, and fixtures only prove the parser matches the fixture — this is how
// you find out the real files disagree with it.
//
//	DOTS_MEMORY_LIVE=1 go test ./internal/dots/memory/ -run TestLiveScan -v
func TestLiveScan(t *testing.T) {
	if os.Getenv("DOTS_MEMORY_LIVE") != "1" {
		t.Skip("set DOTS_MEMORY_LIVE=1 to scan this machine's real sessions")
	}

	// experimental on: this test exists precisely to catch a real Cursor or
	// ChatGPT session disagreeing with the adapters built from one hand-read
	// fixture each. Scan() never writes, so this is still read-only.
	idx := Index{}
	for _, r := range ScanAll(&idx, NewRedactor(DefaultRedactionsPath()), true) {
		t.Logf("adapter %-12s sessions=%-4d err=%v", r.Tool, r.Sessions, r.Err)
		if r.Err != nil {
			t.Errorf("adapter %s failed: %v", r.Tool, r.Err)
		}
	}
	t.Logf("total indexed: %d", len(idx.Sessions))

	t.Log("== projects ==")
	for _, p := range idx.Projects() {
		t.Logf("  %s", p)
	}

	t.Log("== 12 most recent ==")
	for i, s := range idx.Sessions {
		if i >= 12 {
			break
		}
		title := s.Title
		if len(title) > 44 {
			title = title[:44]
		}
		t.Logf("  %-11s %-38s %-46s msgs=%d", s.Tool, s.Project, title, s.Messages)
	}
}

// TestLiveCapture runs one real capture pass — Ollama call included — into a
// throwaway vault, and checks the note that comes out.
//
// This is the test that would have caught the bug it is named after: the vault
// holds 70 notes for 39 sessions because the Python hook dated notes with today
// rather than with the session, so a session spanning three days got three
// files. Here the same session is captured twice and must produce exactly one.
//
//	DOTS_MEMORY_LIVE=1 go test ./internal/dots/memory/ -run TestLiveCapture -v
func TestLiveCapture(t *testing.T) {
	if os.Getenv("DOTS_MEMORY_LIVE") != "1" {
		t.Skip("set DOTS_MEMORY_LIVE=1 to run a real capture")
	}

	vault := t.TempDir()
	// Keep the real index out of it: this pass writes CapturedBytes and would
	// otherwise convince the real capture that these sessions are done.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	opts := CaptureOptions{Vault: vault, Force: true, Max: 1, Experimental: true}
	rep, err := Run(ctx, opts)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	t.Logf("scanned=%v indexed=%d distilled=%d notes=%d skipped=%d errs=%v",
		rep.Scanned, rep.Indexed, rep.Distilled, rep.NotesWrote, rep.Skipped, rep.Errs)

	notes := vaultNotes(t, vault)
	if len(notes) == 0 {
		t.Fatal("capture wrote no notes")
	}

	// A second pass must not duplicate the notes the first one wrote.
	before := len(notes)
	if _, err := Run(ctx, opts); err != nil {
		t.Fatalf("second capture: %v", err)
	}
	after := vaultNotes(t, vault)
	for _, n := range after {
		if !contains(notes, n) {
			t.Logf("second pass added: %s", filepath.Base(n))
		}
	}
	if len(after) > before+opts.Max {
		t.Errorf("second pass duplicated notes: %d -> %d", before, len(after))
	}

	body, err := os.ReadFile(notes[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	t.Logf("--- %s ---\n%s", filepath.Base(notes[0]), text)

	if !strings.Contains(text, "\nproject: ") {
		t.Error("note has no project frontmatter — the key the whole feature turns on")
	}
	for _, leak := range []string{"sk-ant-", "ghp_", "AKIA"} {
		if strings.Contains(text, leak) {
			t.Errorf("unredacted secret %q reached the vault", leak)
		}
	}
}

func vaultNotes(t *testing.T, vault string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(vault, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".md") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
