package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Reconciliation is the migration from the Python hook's output, and it has to
// happen before the Go path writes anything real.
//
// NotePath is date + short id + title, and the titles moved: the old notes were
// named from the first user message at a 40-character cap, the new ones come
// from a substance-preferring extractor at 60. No old note can match a new
// canonical path, so a first capture against the real vault would add ~39 notes
// *beside* the 70 already there, in two naming schemes — recreating the
// duplicate bug once, at cutover, which is precisely what this feature exists to
// fix.
//
// The instability is not only historical. Every improvement to title extraction
// moves NotePath and orphans the previous file; that already happened once in a
// scratch vault mid-development. So this runs on every full scan, not once as a
// migration, and it is what keeps one session to one note over time.
//
// Nothing is ever deleted. These are real summaries in an iCloud-synced,
// git-backed vault, and a deletion propagates to every machine on the account
// before anyone can look at it — so superseded notes move to a folder the user
// can read and empty themselves.
const supersededDir = "_superseded"

// vaultNote is one note on disk, identified by its frontmatter rather than its
// filename — the filename is the thing that is not stable.
type vaultNote struct {
	path    string
	session string
	mod     time.Time
	body    string
}

// scanVaultNotes indexes the vault by session id.
//
// Only files carrying a `session:` field are considered. The vault root holds
// unrelated material — hand-written notes, other tools' exports — and this must
// walk past all of it without touching anything.
func scanVaultNotes(vault string) map[string][]vaultNote {
	out := map[string][]vaultNote{}
	if vault == "" {
		return out
	}
	_ = filepath.Walk(vault, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil // an unreadable corner of the vault is not fatal
		}
		if fi.IsDir() {
			// Superseded notes are already resolved, and a dotdir is never ours.
			if fi.Name() == supersededDir || strings.HasPrefix(fi.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		id, body, ok := parseNote(string(b))
		if !ok {
			return nil
		}
		out[id] = append(out[id], vaultNote{path: path, session: id, mod: fi.ModTime(), body: body})
		return nil
	})
	return out
}

// parseNote pulls the session id and the prose out of a note. It returns false
// for anything that is not one of ours, which is the common case when walking a
// vault a person also writes in by hand.
func parseNote(s string) (session, body string, ok bool) {
	if !strings.HasPrefix(s, "---\n") {
		return "", "", false
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", false
	}
	front, after := rest[:end], rest[end:]
	if i := strings.Index(after, "\n"); i >= 0 {
		if j := strings.Index(after[i+1:], "\n"); j >= 0 {
			after = after[i+1+j+1:]
		}
	}

	for _, line := range strings.Split(front, "\n") {
		v, found := strings.CutPrefix(strings.TrimSpace(line), "session:")
		if !found {
			continue
		}
		session = strings.Trim(strings.TrimSpace(v), `"'`)
		break
	}
	if session == "" {
		return "", "", false
	}

	// Drop the "# Title" heading: the title is regenerated from the index, and
	// keeping it would nest a stale one inside the new note's body.
	body = strings.TrimSpace(after)
	if strings.HasPrefix(body, "# ") {
		if i := strings.Index(body, "\n"); i >= 0 {
			body = strings.TrimSpace(body[i+1:])
		} else {
			body = ""
		}
	}
	return session, body, true
}

// ReconcileReport records what a reconciliation pass moved and adopted.
type ReconcileReport struct {
	Adopted    int // summaries recovered from existing notes, costing no Ollama call
	Renamed    int // notes moved to their canonical path
	Superseded int // duplicates set aside for the user to review
	Errs       []error
}

// Reconcile matches the vault against the index and leaves one note per
// session, at the path NotePath will compute for it.
//
// Adoption is the reason this is worth doing carefully rather than by deleting
// and regenerating: the 70 existing notes were produced by the same model and
// the same prompt, so their prose is exactly what distillation would produce
// again. Taking it back into the index backfills project frontmatter into every
// one of them without a single Ollama call.
func Reconcile(vault string, idx *Index) ReconcileReport {
	var rep ReconcileReport
	if vault == "" {
		return rep
	}
	notes := scanVaultNotes(vault)
	if len(notes) == 0 {
		return rep
	}

	for i := range idx.Sessions {
		s := &idx.Sessions[i]
		found := notes[s.ID]
		if len(found) == 0 {
			continue
		}
		// Newest first: if several notes describe one session, the last one
		// written is the one whose summary saw the most of the conversation.
		sort.SliceStable(found, func(a, b int) bool { return found[a].mod.After(found[b].mod) })

		canonical := NotePath(vault, *s)
		keep := ""
		for _, n := range found {
			if n.path == canonical {
				keep = n.path
				break
			}
		}
		if keep == "" {
			// Move rather than write-and-orphan, so the note keeps its identity
			// across a change in how titles are extracted.
			if err := os.MkdirAll(filepath.Dir(canonical), 0o700); err != nil {
				rep.Errs = append(rep.Errs, err)
				continue
			}
			if err := os.Rename(found[0].path, canonical); err != nil {
				rep.Errs = append(rep.Errs, err)
				continue
			}
			found[0].path = canonical
			keep = canonical
			rep.Renamed++
		}

		if s.Summary == "" && strings.TrimSpace(found[0].body) != "" {
			s.Summary = found[0].body
			rep.Adopted++
		}
		s.VaultNote = canonical
		// The note is kept but its frontmatter predates project identity, so
		// leave CapturedBytes clear: the capture pass rewrites it once, which is
		// the backfill.

		for _, n := range found {
			if n.path == keep {
				continue
			}
			if err := supersede(vault, n.path); err != nil {
				rep.Errs = append(rep.Errs, err)
				continue
			}
			rep.Superseded++
		}
	}
	return rep
}

// supersede moves a duplicate note aside, preserving its folder so the user can
// see which tool it came from. Never a delete: these are the only copy.
func supersede(vault, path string) error {
	rel, err := filepath.Rel(vault, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	dst := filepath.Join(vault, supersededDir, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	// A name already taken means an earlier pass set aside a different note
	// that happened to share a filename. Keep both.
	base := strings.TrimSuffix(dst, ".md")
	for n := 1; n <= 10000; n++ {
		_, statErr := os.Stat(dst)
		if os.IsNotExist(statErr) {
			break
		}
		if statErr != nil {
			// Any error other than NotExist is not a collision; don't spin.
			return statErr
		}
		dst = fmt.Sprintf("%s (%d).md", base, n)
	}
	return os.Rename(path, dst)
}
