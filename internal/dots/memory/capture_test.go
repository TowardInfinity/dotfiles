package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func transcriptOf(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A session judged too slight to summarize must retire. The original ordering
// tested `Summary == ""` before comparing CapturedBytes, which made the skip
// path unreachable: the session stayed due forever, so every capture pass
// re-read every abandoned transcript off disk. With Codex rollouts running to
// tens of thousands of lines that is the per-turn cost bug in a new form.
func TestNoteDueRetiresTrivialSessions(t *testing.T) {
	path := transcriptOf(t, "a trivial two-message session\n")
	s := Session{
		ID: "x", Tool: "codex", Messages: 2,
		Transcript: path,
		Updated:    time.Now().Add(-time.Hour),
	}
	now := time.Now()
	idle := time.Minute

	if !noteDue(s, now, idle) {
		t.Fatal("a never-examined session should be due")
	}

	// What the capture loop records when WorthSummarizing says no.
	s.Trivial = true
	s.CapturedBytes = transcriptSize(s)
	if noteDue(s, now, idle) {
		t.Error("trivial session still due after being examined — it will be re-read forever")
	}

	// Until the conversation actually continues, at which point it is worth
	// another look: a session is only trivial at the size it was read at.
	if err := os.WriteFile(path, []byte("much more conversation, now substantive\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !noteDue(s, now, idle) {
		t.Error("grown transcript did not reopen a trivial session")
	}
}

// The session that just invoked the hook is still moving, and summarizing it
// mid-thought is what produced 70 notes for 39 sessions.
func TestNoteDueSkipsLiveSessions(t *testing.T) {
	s := Session{ID: "x", Tool: "codex", Messages: 40, Updated: time.Now()}
	if noteDue(s, time.Now(), 20*time.Minute) {
		t.Error("a session updated seconds ago was treated as finished")
	}
}

// A note deleted from the vault by hand should come back on the next pass.
func TestNoteDueWhenNoteMissing(t *testing.T) {
	s := Session{
		ID: "x", Tool: "codex", Messages: 40,
		Summary: "something", VaultNote: "/nonexistent/note.md",
		Updated: time.Now().Add(-time.Hour),
	}
	if !noteDue(s, time.Now(), time.Minute) {
		t.Error("missing note was not rewritten")
	}
}

// Stop fires per assistant turn and runs async, so passes overlap. Two
// concurrent passes both summarize the same sessions and the later SaveIndex
// discards the earlier one's work.
func TestCaptureIsSingleFlight(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	release, ok := acquireLock()
	if !ok {
		t.Fatal("could not take the lock in a clean cache dir")
	}
	if _, ok := acquireLock(); ok {
		t.Error("a second pass took the lock while the first held it")
	}

	release()
	release2, ok := acquireLock()
	if !ok {
		t.Fatal("lock not released")
	}
	release2()
}

// A crashed pass must not block capture forever.
func TestCaptureLockExpires(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	release, ok := acquireLock()
	if !ok {
		t.Fatal("could not take the lock")
	}
	defer release()

	old := time.Now().Add(-2 * lockStale)
	if err := os.Chtimes(lockPath(), old, old); err != nil {
		t.Fatal(err)
	}
	stolen, ok := acquireLock()
	if !ok {
		t.Fatal("a stale lock was not broken")
	}
	stolen()
}

// fakeAdapter returns a fixed set of sessions, standing in for a real tool's
// state directory so ScanAll's merge logic can be tested without touching
// this machine's actual Claude/Codex/Grok files.
type fakeAdapter struct {
	name     string
	sessions []Session
}

func (f fakeAdapter) Name() string             { return f.name }
func (f fakeAdapter) Available() bool          { return true }
func (f fakeAdapter) Scan() ([]Session, error) { return f.sessions, nil }

// Trivial state must survive a round-trip through ScanAll, so sessions retired
// in one capture pass stay retired in the next one. The bug was that ScanAll
// copied VaultNote, CapturedBytes, and Summary from the previous index, but not
// Trivial — so every full scan un-retired every trivial session. This crosses
// the actual boundary the bug lived on: a real adapter re-reading metadata from
// disk knows nothing about Trivial and always returns it false, the same as any
// real adapter would.
func TestTrivialSurvivesScan(t *testing.T) {
	old := Adapters
	defer func() { Adapters = old }()
	Adapters = func(bool) []Adapter {
		// What a real adapter does on every scan: re-read metadata, know
		// nothing about summarization state.
		return []Adapter{fakeAdapter{name: "claude", sessions: []Session{
			{ID: "x", Tool: "claude", Trivial: false},
		}}}
	}

	idx := Index{Sessions: []Session{{
		ID: "x", Tool: "claude", Trivial: true, CapturedBytes: 1000,
	}}}

	ScanAll(&idx, NewRedactor(""), false)

	got, ok := idx.Find("claude", "x")
	if !ok {
		t.Fatal("session dropped by ScanAll")
	}
	if !got.Trivial {
		t.Error("Trivial was not preserved across ScanAll — trivial sessions will be re-read forever")
	}
}
