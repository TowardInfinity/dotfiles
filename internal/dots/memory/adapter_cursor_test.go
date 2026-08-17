package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCursorSession lays out one ~/.cursor/chats/<hash>/<uuid>/ directory
// matching the real shape found on this machine: meta.json with the fields
// Cursor actually writes, plus a prompt_history.json holding the raw strings
// the user typed. Passing nil prompts omits the file entirely, matching a
// session that never got past its first (still-streaming) turn.
func writeCursorSession(t *testing.T, home, hash, id string, meta cursorMeta, prompts []string) string {
	t.Helper()
	dir := filepath.Join(home, ".cursor", "chats", hash, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if prompts != nil {
		b, err := json.Marshal(prompts)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "prompt_history.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCursorAdapterAvailable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	a := CursorAdapter{}
	if a.Available() {
		t.Fatal("Available() true before ~/.cursor/chats exists")
	}
	if err := os.MkdirAll(filepath.Join(home, ".cursor", "chats"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !a.Available() {
		t.Fatal("Available() false after ~/.cursor/chats exists")
	}
}

func TestCursorAdapterScan(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	created := time.UnixMilli(1786899022608)
	updated := time.UnixMilli(1786899043557)
	dir := writeCursorSession(t, home, "7eff3f8f", "addec809-9ee5-4323-9673-6c319cafb1df",
		cursorMeta{
			Title:       "Hello There",
			CWD:         filepath.Join(home, "project"),
			CreatedAtMs: created.UnixMilli(),
			UpdatedAtMs: updated.UnixMilli(),
		},
		[]string{"hi", "what does this repo do"},
	)

	// A session with no prompt_history.json at all — nothing a person typed
	// yet — must not produce a Session.
	writeCursorSession(t, home, "7eff3f8f", "00000000-0000-0000-0000-000000000000",
		cursorMeta{Title: "Empty", CreatedAtMs: created.UnixMilli(), UpdatedAtMs: updated.UnixMilli()},
		nil,
	)

	sessions, err := CursorAdapter{}.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1: %+v", len(sessions), sessions)
	}

	s := sessions[0]
	if s.Tool != "cursor" {
		t.Errorf("Tool = %q, want cursor", s.Tool)
	}
	if s.ID != "addec809-9ee5-4323-9673-6c319cafb1df" {
		t.Errorf("ID = %q, want the uuid directory name", s.ID)
	}
	if s.Title != "Hello There" {
		t.Errorf("Title = %q, want meta.json's own title", s.Title)
	}
	if s.Messages != 2 {
		t.Errorf("Messages = %d, want 2 (prompt count — this adapter never sees replies)", s.Messages)
	}
	if !s.Started.Equal(created) || !s.Updated.Equal(updated) {
		t.Errorf("Started/Updated = %v/%v, want %v/%v", s.Started, s.Updated, created, updated)
	}
	want := filepath.Join(dir, "prompt_history.json")
	if s.Transcript != want {
		t.Errorf("Transcript = %q, want %q (transcriptSize's debounce target)", s.Transcript, want)
	}
	if s.Project == "" {
		t.Error("Project not resolved")
	}
}

func TestCursorAdapterTitleFallsBackToFirstPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeCursorSession(t, home, "abc", "session-1",
		cursorMeta{CreatedAtMs: 1000, UpdatedAtMs: 2000}, // no Title
		[]string{"continue", "fix the flaky retry test in publish.go"},
	)

	sessions, err := CursorAdapter{}.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	// "continue" is filler; FirstPrompt should skip it for the real prompt.
	if got, want := sessions[0].Title, "fix the flaky retry test in publish.go"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}
}

func TestReadCursorPrompts(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{"well formed", `["hi","fix the bug"]`, []string{"hi", "fix the bug"}},
		{"non-string entries skipped", `["hi", 42, {"a":1}, "ok go"]`, []string{"hi", "ok go"}},
		{"empty strings dropped", `["", "real prompt", ""]`, []string{"real prompt"}},
		{"malformed json", `not json`, nil},
		{"not an array", `{"a":1}`, nil},
		{"empty array", `[]`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.name+".json")
			if err := os.WriteFile(p, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got := readCursorPrompts(p)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}

	if got := readCursorPrompts(filepath.Join(dir, "does-not-exist.json")); got != nil {
		t.Errorf("missing file: got %v, want nil", got)
	}
}

func TestCursorConversationDisclaimer(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "prompt_history.json")
	if err := os.WriteFile(p, []byte(`["hi","what does publish.go do","thanks"]`), 0o600); err != nil {
		t.Fatal(err)
	}

	text, users, err := cursorConversation(p)
	if err != nil {
		t.Fatalf("cursorConversation: %v", err)
	}
	// Every real prompt counts, "thanks" included — filler is a title-picking
	// concern (FirstPrompt), not a summarization one.
	if users != 3 {
		t.Errorf("users = %d, want 3", users)
	}
	for _, want := range []string{"Cursor", "encrypted", "hi", "what does publish.go do", "thanks"} {
		if !strings.Contains(text, want) {
			t.Errorf("conversation text missing %q:\n%s", want, text)
		}
	}
}

func TestCursorConversationErrors(t *testing.T) {
	if _, _, err := cursorConversation(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("want error for a missing transcript, got nil")
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(p, []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cursorConversation(p); err == nil {
		t.Error("want error for malformed json, got nil")
	}
}
