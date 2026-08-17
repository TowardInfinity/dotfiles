// Package memory keeps one cross-tool record of what was worked on, keyed by
// project rather than by directory.
//
// Five AI tools on this machine each keep their own history in their own
// format, and none can see the others'. This package reads all of them, throws
// away the raw transcripts, and keeps one short distilled note per session in
// the Obsidian vault — so a new chat in a familiar repo can start with what
// happened there last week, whichever tool did the work.
package memory

import (
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ProjectKey identifies the thing being worked on, stably across machines,
// clones and worktrees.
//
// The obvious key is the working directory, and it is wrong. Two facts on this
// machine prove it: ~/Codes/Projects/screener has remote TowardInfinity/almanac
// — directory name and project disagree — and Claude files that session's
// transcript under a slug of the *launch* directory, which is a third value
// again. A second clone, a worktree, or the same repo on another box would each
// invent a new identity under a cwd key, and the memory would fragment exactly
// where it is most wanted.
//
// So resolution goes remote first, and cwd is kept only as a field.
type ProjectKey string

// Unscoped is the key for work with no project at all — ChatGPT desktop chats,
// a session started in $HOME. Better one honest bucket than a key per
// directory that happened to be current.
const Unscoped ProjectKey = "unscoped"

type resolved struct {
	key     ProjectKey
	gitRoot string
	remote  string
}

// resolveCache memoizes by directory for the life of the process.
//
// Resolution shells out to git twice, and a reindex resolves once per session —
// hundreds of them, nearly all sharing a handful of directories. Caching turns
// that back into two forks per distinct directory. Staleness is not a concern:
// these are one-shot CLI invocations, and the hook path is far too short-lived
// for a repo to gain a remote mid-run.
var resolveCache sync.Map // string -> resolved

// ResolveProject returns the key for a directory, and the git root and remote
// it derived it from. All three are recorded on the session: the key is what
// memory is grouped by, the other two are how a human recognises it later.
func ResolveProject(dir string) (key ProjectKey, gitRoot, remote string) {
	if dir == "" {
		return Unscoped, "", ""
	}
	if v, ok := resolveCache.Load(dir); ok {
		r := v.(resolved)
		return r.key, r.gitRoot, r.remote
	}
	key, gitRoot, remote = resolveUncached(dir)
	resolveCache.Store(dir, resolved{key, gitRoot, remote})
	return key, gitRoot, remote
}

func resolveUncached(dir string) (key ProjectKey, gitRoot, remote string) {

	root := gitOutput(dir, "rev-parse", "--show-toplevel")
	if root == "" {
		// Not a repo. An absolute path is a poor key but a truthful one, and
		// it at least keeps ~/notes distinct from ~/scratch.
		return ProjectKey(filepath.Clean(dir)), "", ""
	}

	remote = gitOutput(dir, "remote", "get-url", "origin")
	if k := NormalizeRemote(remote); k != "" {
		return ProjectKey(k), root, remote
	}
	// A repo with no origin — common for a local scratch repo. The toplevel
	// basename is stable across worktrees of it, which is the property that
	// matters.
	return ProjectKey(filepath.Base(root)), root, remote
}

// NormalizeRemote collapses the several ways of writing one repository into a
// single host/owner/repo key. These must all agree, or the same project splits
// in two depending on how it happened to be cloned:
//
//	git@github.com:TowardInfinity/almanac.git
//	https://github.com/TowardInfinity/almanac
//	ssh://git@github.com/TowardInfinity/almanac.git
//
// Returns "" when the remote is empty or unparseable; the caller falls back.
func NormalizeRemote(remote string) string {
	r := strings.TrimSpace(remote)
	if r == "" {
		return ""
	}
	r = strings.TrimSuffix(r, "/")
	r = strings.TrimSuffix(r, ".git")

	// scp-style — git@host:owner/repo — is not a URL and url.Parse quietly
	// mis-reads it rather than failing, so it has to be spotted first. The
	// "no //" test is what distinguishes it from ssh://git@host/owner/repo.
	if !strings.Contains(r, "//") {
		if at := strings.LastIndex(r, "@"); at >= 0 {
			r = r[at+1:]
		}
		if colon := strings.Index(r, ":"); colon >= 0 {
			host, path := r[:colon], strings.TrimPrefix(r[colon+1:], "/")
			if host != "" && path != "" {
				return strings.ToLower(host) + "/" + path
			}
		}
		return ""
	}

	u, err := url.Parse(r)
	if err != nil || u.Host == "" {
		return ""
	}
	// u.Host carries the port and url.Parse keeps userinfo out of it already.
	host := strings.ToLower(u.Hostname())
	path := strings.Trim(u.Path, "/")
	if host == "" || path == "" {
		return ""
	}
	return host + "/" + path
}

// gitOutput runs one read-only git command in dir, returning "" for any
// failure. Every caller here treats "not a repo", "git missing" and "git broke"
// identically — they all mean "fall back to the next rule" — so distinguishing
// them would only add error paths that no one branches on.
func gitOutput(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
