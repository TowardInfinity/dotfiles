package memory

import (
	"sort"
	"strings"
)

// SearchOptions selects and bounds a search. Search itself is a
// case-insensitive substring scan over Title and Summary. At the current
// corpus size (a few hundred sessions, roughly a kilobyte of text each) this
// is comfortably sub-millisecond — see the plan's note on why that means no
// sqlite, no cgo, and nothing that complicates cross-compiling the release
// binary. Reach for something heavier only if the corpus outgrows this, not
// before.
//
// A trivial session has no Summary, so a query can only match its Title —
// which is exactly right: nothing was judged worth describing, so nothing
// beyond the one line it left behind should be findable.
type SearchOptions struct {
	Query   string
	Project ProjectKey // empty searches every project
	Limit   int
}

// SearchResult is one match, with enough of the record to act on: Search
// itself never touches the transcript or the vault.
type SearchResult struct {
	Session Session
	// Hits is which fields matched, so a caller can show why a result is
	// here — e.g. a title match reads very differently from a match buried
	// in three paragraphs of summary.
	Hits []string
}

// Search scans the index for sessions whose Title or Summary contains Query,
// most recently updated first. An empty Query matches nothing — that is a
// caller bug, not "give me everything," and returning everything for a typo'd
// empty string would be a surprising way to find out.
func Search(idx Index, opts SearchOptions) []SearchResult {
	q := strings.ToLower(strings.TrimSpace(opts.Query))
	if q == "" {
		return nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	var out []SearchResult
	for _, s := range idx.Sessions {
		if opts.Project != "" && s.Project != opts.Project {
			continue
		}
		var hits []string
		if strings.Contains(strings.ToLower(s.Title), q) {
			hits = append(hits, "title")
		}
		if strings.Contains(strings.ToLower(s.Summary), q) {
			hits = append(hits, "summary")
		}
		if len(hits) == 0 {
			continue
		}
		out = append(out, SearchResult{Session: s, Hits: hits})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Session.Updated.After(out[j].Session.Updated)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
