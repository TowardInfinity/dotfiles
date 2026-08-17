package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// countNotes counts .md files under dir, excluding the superseded folder.
func countNotes(t *testing.T, dir string) int {
	t.Helper()
	var n int
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			// Skip the superseded folder, unless it is what we were asked to
			// count.
			if err == nil && fi.IsDir() && fi.Name() == supersededDir && p != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".md") {
			n++
		}
		return nil
	})
	return n
}

// writeLegacyNote imitates what the Python hook produced: dated with the day it
// ran rather than the day the session started, titled from the raw first
// message, and with no project or tool frontmatter.
func writeLegacyNote(t *testing.T, vault, day, title, id, body string) string {
	t.Helper()
	dir := filepath.Join(vault, "Claude Chats")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, day+" "+shortID(id)+" - "+title+".md")
	content := "---\ndate: " + day + "\nsession_id: " + shortID(id) + "\nsession: " + id +
		"\ntranscript: \"/tmp/x.jsonl\"\ntags:\n  - claude\n  - ai-chat\n---\n\n# " + title + "\n\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The cutover case. Old notes cannot match the new NotePath — the date and the
// title extraction both changed — so without reconciliation the first real
// capture writes a second note beside every existing one, in two naming
// schemes. That is the duplicate bug this feature exists to fix, recreated at
// the moment of the fix.
func TestReconcileClaimsLegacyNotesInsteadOfDuplicating(t *testing.T) {
	vault := t.TempDir()
	s := testSession() // starts 2026-08-10, title "global ai memory"

	// Written on three separate days by the per-turn hook, all one session.
	writeLegacyNote(t, vault, "2026-08-15", "hey can you", s.ID, "First day body.")
	writeLegacyNote(t, vault, "2026-08-16", "hey can you", s.ID, "Second day body.")
	newest := writeLegacyNote(t, vault, "2026-08-17", "hey can you", s.ID, "Final body, most complete.")
	if err := os.Chtimes(newest, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}

	idx := Index{Sessions: []Session{s}}
	rep := Reconcile(vault, &idx)
	if len(rep.Errs) != 0 {
		t.Fatalf("errors: %v", rep.Errs)
	}

	if got := countNotes(t, vault); got != 1 {
		t.Errorf("reconcile left %d live notes, want 1", got)
	}
	if rep.Superseded != 2 {
		t.Errorf("superseded %d, want 2", rep.Superseded)
	}

	got := idx.Sessions[0]
	if want := NotePath(vault, s); got.VaultNote != want {
		t.Errorf("VaultNote = %q, want canonical %q", got.VaultNote, want)
	}
	if _, err := os.Stat(got.VaultNote); err != nil {
		t.Errorf("canonical note not on disk: %v", err)
	}
	// Adoption is what makes the backfill free: the existing prose came from
	// the same model and prompt, so re-summarizing it would spend an Ollama
	// call to produce the same thing.
	if !strings.Contains(got.Summary, "Final body, most complete.") {
		t.Errorf("did not adopt the newest note's body: %q", got.Summary)
	}

	// Nothing is deleted — the superseded notes are still readable.
	if n := countNotes(t, filepath.Join(vault, supersededDir)); n != 2 {
		t.Errorf("%d notes set aside, want 2 (a delete here propagates to every machine)", n)
	}
}

// Reconciliation runs on every scan, not once, because any change to title
// extraction moves NotePath. Running it twice must be a no-op.
func TestReconcileIsIdempotent(t *testing.T) {
	vault := t.TempDir()
	s := testSession()
	writeLegacyNote(t, vault, "2026-08-15", "hey can you", s.ID, "Body.")

	idx := Index{Sessions: []Session{s}}
	Reconcile(vault, &idx)
	second := Reconcile(vault, &idx)

	if second.Renamed != 0 || second.Superseded != 0 || second.Adopted != 0 {
		t.Errorf("second pass was not a no-op: %+v", second)
	}
	if got := countNotes(t, vault); got != 1 {
		t.Errorf("%d live notes after two passes, want 1", got)
	}
}

// The vault is a place a person also writes in by hand. Reconcile walks all of
// it and must not touch anything that is not one of ours — including, on this
// machine, a file holding a plaintext API key.
func TestReconcileIgnoresForeignNotes(t *testing.T) {
	vault := t.TempDir()
	foreign := filepath.Join(vault, "openrouter", "setup.md")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o700); err != nil {
		t.Fatal(err)
	}
	// Concatenated, not one literal — see the comment in redact_test.go on
	// why a whole-in-the-diff match still trips push protection.
	secret := "sk-or-v1-" + strings.Repeat("x", 16)
	if err := os.WriteFile(foreign, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	idx := Index{Sessions: []Session{testSession()}}
	rep := Reconcile(vault, &idx)

	if rep.Renamed != 0 || rep.Superseded != 0 {
		t.Errorf("touched a foreign note: %+v", rep)
	}
	b, err := os.ReadFile(foreign)
	if err != nil {
		t.Fatalf("foreign note moved or removed: %v", err)
	}
	if !strings.Contains(string(b), secret) {
		t.Error("foreign note was rewritten")
	}
}

func TestParseNoteRejectsPlainMarkdown(t *testing.T) {
	for _, s := range []string{
		"just some prose",
		"---\ndate: 2026-01-01\n---\n\nno session field",
		"---\nunterminated frontmatter\n",
		"",
	} {
		if _, _, ok := parseNote(s); ok {
			t.Errorf("parseNote(%.30q) claimed a foreign note", s)
		}
	}
}
