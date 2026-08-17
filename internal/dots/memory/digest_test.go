package memory

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// seedIndex points the store at a temp cache and fills it.
func seedIndex(t *testing.T, sessions []Session) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := SaveIndex(Index{Sessions: sessions}); err != nil {
		t.Fatal(err)
	}
}

// The digest is injected into every session and re-read on every turn, so the
// budget is the one number that must hold. This asserts against the rendered
// output rather than recomputing the producer's arithmetic.
func TestDigestRespectsBudget(t *testing.T) {
	const project ProjectKey = "github.com/TowardInfinity/dotfiles"

	var sessions []Session
	for i := range 50 {
		sessions = append(sessions, Session{
			ID:       fmt.Sprintf("session-%02d", i),
			Tool:     "claude-code",
			Project:  project,
			Messages: 20,
			Title:    fmt.Sprintf("%02d %s", i, strings.Repeat("a long title that will not fit ", 4)),
			Updated:  time.Now().Add(-time.Duration(i) * time.Hour),
		})
	}
	seedIndex(t, sessions)

	for _, budget := range []int{256, 512, 1024, 4096} {
		out := Digest(DigestOptions{Project: project, Budget: budget})
		if len(out) > budget {
			t.Errorf("budget %d: digest was %d bytes:\n%s", budget, len(out), out)
		}
	}
}

// Silence is a normal result — a first session in a new repo — and it must stay
// silent rather than announcing that it knows nothing.
func TestDigestSilentWhenNothingToSay(t *testing.T) {
	seedIndex(t, nil)
	if out := Digest(DigestOptions{Project: "github.com/nobody/nothing"}); out != "" {
		t.Errorf("expected silence, got %q", out)
	}
	if out := Digest(DigestOptions{Project: Unscoped}); out != "" {
		t.Errorf("unscoped should be silent, got %q", out)
	}
}

// A header with no entries under it is worse than no output at all.
func TestDigestNeverEmitsLoneHeader(t *testing.T) {
	const project ProjectKey = "github.com/TowardInfinity/dotfiles"
	seedIndex(t, []Session{{
		ID: "a", Tool: "claude-code", Project: project, Messages: 20,
		Title: strings.Repeat("x", 200), Updated: time.Now(),
	}})

	// A budget large enough for the header but not for any entry.
	out := Digest(DigestOptions{Project: project, Budget: 60})
	if out != "" {
		t.Errorf("expected silence rather than a lone header, got %q", out)
	}
}

// Abandoned "continue" stubs are real sessions with honest titles, and listing
// them crowds out work worth remembering.
func TestDigestSkipsTrivialSessions(t *testing.T) {
	const project ProjectKey = "github.com/TowardInfinity/dotfiles"
	now := time.Now()
	seedIndex(t, []Session{
		{ID: "junk", Tool: "codex", Project: project, Messages: 2, Title: "continue", Updated: now},
		{ID: "real", Tool: "codex", Project: project, Messages: 40, Title: "rework the fleet cache schema", Updated: now.Add(-time.Hour)},
	})

	out := Digest(DigestOptions{Project: project, Budget: 1024})
	if strings.Contains(out, "continue") {
		t.Errorf("trivial session reached the digest:\n%s", out)
	}
	if !strings.Contains(out, "rework the fleet cache schema") {
		t.Errorf("substantive session missing:\n%s", out)
	}
}

// The point of the whole feature: one project, every tool.
func TestDigestSpansTools(t *testing.T) {
	const project ProjectKey = "github.com/TowardInfinity/almanac"
	now := time.Now()
	seedIndex(t, []Session{
		{ID: "1", Tool: "claude-code", Project: project, Messages: 40, Title: "cache miss fallback", Updated: now},
		{ID: "2", Tool: "codex", Project: project, Messages: 40, Title: "review the plan", Updated: now.Add(-time.Hour)},
		{ID: "3", Tool: "grok", Project: project, Messages: 40, Title: "landing palette", Updated: now.Add(-2 * time.Hour)},
	})

	out := Digest(DigestOptions{Project: project, Budget: 1024})
	for _, want := range []string{"claude", "codex", "grok"} {
		if !strings.Contains(out, want) {
			t.Errorf("digest missing %s:\n%s", want, out)
		}
	}
}

// A corrupt index must read as "no memory yet", not as a failure — this path
// runs in the hook that gates starting Claude.
func TestDigestSurvivesCorruptIndex(t *testing.T) {
	seedIndex(t, []Session{{ID: "a", Tool: "codex", Project: "p", Messages: 9, Title: "t", Updated: time.Now()}})
	if err := writeFileAtomic(IndexPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := Digest(DigestOptions{Project: "p", Budget: 1024}); out != "" {
		t.Errorf("corrupt index produced output: %q", out)
	}
}
