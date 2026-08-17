package memory

import (
	"os"
	"path/filepath"
	"strings"
)

// Adapter reads one tool's session store.
//
// Adapters live in this package rather than a sub-package because they both
// produce and are collected by types defined here; splitting them out would
// mean either an import cycle or an interface package existing only to break
// one.
type Adapter interface {
	// Name is the tool id recorded on every Session it produces.
	Name() string
	// Available reports whether this tool keeps state on this machine. The
	// fleet is heterogeneous — Grok and ChatGPT are Mac-only here — so every
	// adapter must be skippable without that counting as an error.
	Available() bool
	// Scan returns sessions whose transcripts changed. Adapters return the
	// session metadata and a path to the transcript; they do not summarize.
	Scan() ([]Session, error)
}

// Adapters returns every adapter, in the order results should be indexed.
// Experimental ones are excluded unless asked for: Cursor and ChatGPT store
// their history in undocumented formats that can change without notice, and a
// parse failure there must not be able to stop Claude and Codex being captured.
//
// A package-level var, not a plain function, so tests can substitute a fake
// adapter and exercise ScanAll's merge logic without touching this machine's
// real tool state directories.
var Adapters = func(experimental bool) []Adapter {
	all := []Adapter{ClaudeAdapter{}, CodexAdapter{}, GrokAdapter{}}
	if experimental {
		all = append(all, CursorAdapter{}, ChatGPTAdapter{})
	}
	return all
}

// home is os.UserHomeDir without the error return. Every caller treats a
// missing home directory as "this adapter is unavailable", which the empty
// string already produces via the Stat in Available.
func home() string {
	h, _ := os.UserHomeDir()
	return h
}

// denied reports whether a path must never be read, whatever a directory walk
// turns up. Credential files sit beside session files in every one of these
// tools' state directories, and a summarizer is exactly the wrong thing to
// hand them to. This is a denylist rather than an allowlist only because the
// walk is already narrowed to *.jsonl and summary.json.
func denied(path string) bool {
	base := filepath.Base(path)
	switch base {
	case ".env", "auth.json", "credentials.json", ".credentials.json":
		return true
	}
	if strings.HasPrefix(base, "auth") && strings.HasSuffix(base, ".json") {
		return true
	}
	if strings.HasPrefix(base, ".credentials") {
		return true
	}
	return false
}
