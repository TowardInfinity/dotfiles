package memory

import (
	"testing"
	"time"
)

func TestSearchMatchesTitleOrSummary(t *testing.T) {
	idx := Index{Sessions: []Session{
		{ID: "1", Tool: "claude", Project: "p1", Title: "Fixed the auth race", Updated: time.Now()},
		{ID: "2", Tool: "codex", Project: "p1", Title: "Unrelated", Summary: "touched the auth flow", Updated: time.Now()},
		{ID: "3", Tool: "grok", Project: "p1", Title: "Nothing to do with it", Summary: "wrote docs", Updated: time.Now()},
	}}

	got := Search(idx, SearchOptions{Query: "auth"})
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	ids := map[string]bool{}
	for _, r := range got {
		ids[r.Session.ID] = true
	}
	if !ids["1"] || !ids["2"] {
		t.Errorf("expected sessions 1 and 2 to match, got %v", ids)
	}
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	idx := Index{Sessions: []Session{
		{ID: "1", Tool: "claude", Project: "p1", Title: "The AUTH Bug", Updated: time.Now()},
	}}
	if got := Search(idx, SearchOptions{Query: "auth"}); len(got) != 1 {
		t.Errorf("case-insensitive match failed: got %d results", len(got))
	}
}

func TestSearchScopesByProject(t *testing.T) {
	idx := Index{Sessions: []Session{
		{ID: "1", Tool: "claude", Project: "p1", Title: "auth fix", Updated: time.Now()},
		{ID: "2", Tool: "claude", Project: "p2", Title: "auth fix", Updated: time.Now()},
	}}
	got := Search(idx, SearchOptions{Query: "auth", Project: "p1"})
	if len(got) != 1 || got[0].Session.ID != "1" {
		t.Errorf("project scoping failed: got %+v", got)
	}
}

func TestSearchEmptyQueryMatchesNothing(t *testing.T) {
	idx := Index{Sessions: []Session{
		{ID: "1", Tool: "claude", Project: "p1", Title: "anything", Updated: time.Now()},
	}}
	if got := Search(idx, SearchOptions{Query: "  "}); got != nil {
		t.Errorf("empty query should match nothing, got %+v", got)
	}
}

func TestSearchRespectsLimitNewestFirst(t *testing.T) {
	now := time.Now()
	idx := Index{Sessions: []Session{
		{ID: "old", Tool: "claude", Project: "p1", Title: "auth", Updated: now.Add(-time.Hour)},
		{ID: "new", Tool: "claude", Project: "p1", Title: "auth", Updated: now},
	}}
	got := Search(idx, SearchOptions{Query: "auth", Limit: 1})
	if len(got) != 1 || got[0].Session.ID != "new" {
		t.Errorf("expected newest-first with limit 1, got %+v", got)
	}
}

func TestSearchReportsWhichFieldsHit(t *testing.T) {
	idx := Index{Sessions: []Session{
		{ID: "1", Tool: "claude", Project: "p1", Title: "auth bug", Summary: "also mentions auth here", Updated: time.Now()},
	}}
	got := Search(idx, SearchOptions{Query: "auth"})
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	if len(got[0].Hits) != 2 {
		t.Errorf("expected both title and summary to be reported as hits, got %v", got[0].Hits)
	}
}
