package memory

import (
	"fmt"
	"os"
	"strings"
)

// The digest is the only thing injected automatically into a new session, so it
// is the only part of this feature with an ongoing token cost — and it is paid
// on every turn of every session, because injected context is re-read each
// time. That is why the cap is enforced here in code rather than left to a
// convention about writing short summaries.
//
// Anything longer is pulled deliberately by the model through the MCP tools.

// DigestOptions selects and bounds the digest.
type DigestOptions struct {
	Project ProjectKey
	Dir     string
	Budget  int
	Limit   int
}

const (
	defaultBudget = 1024
	defaultLimit  = 6
)

// Digest renders recent work in a project as a short block, or "" when there is
// nothing worth saying.
//
// Silence is a normal, frequent result — a first session in a new repo — and it
// must stay silent rather than announcing that it knows nothing.
func Digest(opts DigestOptions) string {
	if opts.Budget <= 0 {
		opts.Budget = defaultBudget
	}
	if opts.Limit <= 0 {
		opts.Limit = defaultLimit
	}

	project := opts.Project
	if project == "" {
		dir := opts.Dir
		if dir == "" {
			dir, _ = os.Getwd()
		}
		project, _, _ = ResolveProject(dir)
	}
	if project == "" || project == Unscoped {
		return ""
	}

	var candidates []Session
	for _, s := range LoadIndex().Sessions {
		if worthListing(s) {
			candidates = append(candidates, s)
		}
	}
	sessions := Recent(candidates, project, opts.Limit)
	if len(sessions) == 0 {
		return ""
	}

	header := fmt.Sprintf("Recent AI sessions in %s:\n", project)
	var b strings.Builder
	b.WriteString(header)

	// The line is built from Title, not Summary. Titles are one line by
	// construction; a multi-paragraph summary body cannot survive a 1 KB budget
	// across several sessions, and truncating one mid-sentence reads as damage.
	for _, s := range sessions {
		line := fmt.Sprintf("· %s [%s] %s\n",
			s.Updated.Format("2006-01-02"), shortTool(s.Tool), TitleLine(s.Title))
		if b.Len()+len(line) > opts.Budget {
			break
		}
		b.WriteString(line)
	}

	// Only the header made it. Emitting a header with no entries is worse than
	// emitting nothing.
	if b.Len() <= len(header) {
		return ""
	}

	const more = "Ask for detail with the dots-memory MCP tools.\n"
	if b.Len()+len(more) <= opts.Budget {
		b.WriteString(more)
	}
	return b.String()
}

// worthListing filters out sessions that would waste a digest line.
//
// A real category on this machine: a session resumed with "continue" that the
// tool answered with "I don't have the prior context, what would you like?" and
// which was then abandoned. It is a genuine session with an honest title, and
// listing it crowds out work actually worth remembering. The budget is six
// lines; every one of them has to earn its place.
func worthListing(s Session) bool {
	if s.Title == "" || isFiller(s.Title) {
		return false
	}
	// Roughly two exchanges. Below that nothing was established.
	return s.Messages >= 4
}

// shortTool keeps the per-line overhead down; the budget is small enough that
// "claude-code" versus "claude" costs a visible fraction of one entry.
func shortTool(tool string) string {
	if tool == "claude-code" {
		return "claude"
	}
	return tool
}
