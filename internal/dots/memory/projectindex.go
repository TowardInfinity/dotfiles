package memory

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Project index pages are the vault's answer to "what have I done here,
// across every tool" — one page per project, at "AI Chats/Projects/<project>.md",
// holding a Dataview query rather than a hand-maintained list.
//
// A written list goes stale the moment a new session lands; a query does not.
// So these pages are never reconciled or merged the way session notes are —
// they are regenerated whole on every scan, which is safe only because
// nothing here is prose a person added. Deleting one costs nothing: the next
// `dots memory reindex` rewrites it from the current index.

// ProjectIndexPath is where a project's index page belongs.
func ProjectIndexPath(vault string, key ProjectKey) string {
	name := SafeFilename(string(key))
	if name == "" {
		name = "unscoped"
	}
	return filepath.Join(vault, "Projects", name+".md")
}

// projectIndexBody renders the Dataview query page for one project. sessions
// is shown as a plain count above the query — Dataview's own COUNT would
// double as a staleness check, but a query that queries its own table just to
// print a number the caller already has is needless indirection.
func projectIndexBody(key ProjectKey, sessions int) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("project: " + yamlValue(string(key)) + "\n")
	b.WriteString("tags:\n  - ai-chat\n  - project-index\n")
	b.WriteString("---\n\n")
	b.WriteString("# " + string(key) + "\n\n")
	b.WriteString("Sessions indexed for this project across every tool: ")
	b.WriteString(strconv.Itoa(sessions))
	b.WriteString(".\n\n")
	b.WriteString("This page is generated. It holds a live query, not a list — " +
		"`dots memory reindex` rewrites it from the current index every pass, " +
		"and hand-added prose here does not survive that.\n\n")
	b.WriteString("```dataview\n")
	b.WriteString("TABLE tool AS Tool, date AS Date, messages AS Msgs\n")
	b.WriteString(`FROM "AI Chats"` + "\n")
	b.WriteString(`WHERE project = "` + dataviewEscape(string(key)) + `"` + "\n")
	b.WriteString("SORT date DESC\n")
	b.WriteString("```\n")
	return b.String()
}

// WriteProjectIndex writes (or rewrites) one project's index page.
func WriteProjectIndex(vault string, key ProjectKey, sessions int) (string, error) {
	path := ProjectIndexPath(vault, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	body := projectIndexBody(key, sessions)
	if err := writeFileAtomic(path, []byte(body), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// WriteProjectIndexes regenerates every project's index page from idx. It
// returns how many pages were written, and the first error encountered — one
// project's write failing (a read-only vault, a full disk) does not stop the
// others.
func WriteProjectIndexes(vault string, idx Index) (int, error) {
	var n int
	var firstErr error
	for _, p := range idx.Projects() {
		if _, err := WriteProjectIndex(vault, p.Project, p.Sessions); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	return n, firstErr
}

// dataviewEscape lets a project key sit safely inside a Dataview query's
// double-quoted string literal. Keys are host/owner/repo strings or absolute
// paths in practice, so a backslash or quote is not expected — but a
// directory name is user-chosen text, and an unescaped one would silently
// break the query it landed in rather than fail loudly.
func dataviewEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
