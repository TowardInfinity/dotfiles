package memory

import (
	"fmt"
	"sort"
)

// ScanResult reports what one adapter contributed, so `dots memory reindex`
// can show per-tool counts and so a single broken adapter is visibly broken
// rather than silently contributing nothing.
type ScanResult struct {
	Tool     string
	Sessions int
	Err      error
}

// ScanAll runs every available adapter and merges the results into idx.
//
// One adapter's failure never stops the others: the tools are independent, and
// a format change in Cursor must not cost you Claude's history. Errors are
// collected and reported, not returned as a single fatal one.
//
// Titles are redacted here, at the boundary, rather than where they are used.
// A title is derived from the first thing a person typed, and it ends up in a
// note *filename* — where a leaked key is visible in directory listings, in the
// vault's git log and in iCloud sync metadata, which is worse than a leak in a
// body. Redacting once on the way in means no downstream consumer can forget.
func ScanAll(idx *Index, r *Redactor, experimental bool) []ScanResult {
	var results []ScanResult

	for _, a := range Adapters(experimental) {
		if !a.Available() {
			continue
		}
		sessions, err := a.Scan()
		if err != nil {
			results = append(results, ScanResult{Tool: a.Name(), Err: err})
			continue
		}
		for _, s := range sessions {
			s.Title = string(r.Scrub(s.Title))
			// Grok hands over a summary it wrote itself, so this field can
			// arrive carrying raw transcript text. The vault writer scrubs
			// again, but the index is read by more than the vault writer — the
			// MCP tools hand summaries straight to a model — so it is scrubbed
			// on the way in, where nothing downstream can forget to.
			s.Summary = string(r.Scrub(s.Summary))
			// Preserve what distillation already produced. Adapters re-read
			// metadata from disk every scan and know nothing about summaries,
			// so a naive Upsert would erase the note on every reindex and
			// make the next capture pay Ollama again for work already done.
			if prev, ok := idx.Find(s.Tool, s.ID); ok {
				s.VaultNote = prev.VaultNote
				s.CapturedBytes = prev.CapturedBytes
				s.Trivial = prev.Trivial
				// An adapter that carries its own summary (Grok) wins, so a
				// summary it has since revised is picked up. Otherwise keep
				// what distillation produced — adapters know nothing about
				// summaries, and a blind overwrite would erase the note on
				// every reindex and make the next capture pay Ollama again.
				if s.Summary == "" {
					s.Summary = prev.Summary
				}
				if s.Title == "" {
					s.Title = prev.Title
				}
			}
			idx.Upsert(s)
		}
		results = append(results, ScanResult{Tool: a.Name(), Sessions: len(sessions)})
	}

	sort.SliceStable(idx.Sessions, func(i, j int) bool {
		return idx.Sessions[i].Updated.After(idx.Sessions[j].Updated)
	})
	return results
}

// Projects lists every project in the index with a session count, busiest
// first.
func (idx Index) Projects() []ProjectCount {
	counts := map[ProjectKey]int{}
	for _, s := range idx.Sessions {
		counts[s.Project]++
	}
	out := make([]ProjectCount, 0, len(counts))
	for k, n := range counts {
		out = append(out, ProjectCount{Project: k, Sessions: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sessions != out[j].Sessions {
			return out[i].Sessions > out[j].Sessions
		}
		return out[i].Project < out[j].Project
	})
	return out
}

// ProjectCount is one row of Projects.
type ProjectCount struct {
	Project  ProjectKey
	Sessions int
}

func (p ProjectCount) String() string {
	return fmt.Sprintf("%-40s %d", p.Project, p.Sessions)
}
