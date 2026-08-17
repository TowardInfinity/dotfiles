package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectIndexPath(t *testing.T) {
	got := ProjectIndexPath("/vault", "github.com/TowardInfinity/dotfiles")
	want := "/vault/Projects/github.com TowardInfinity dotfiles.md"
	if got != want {
		t.Errorf("ProjectIndexPath = %q, want %q", got, want)
	}

	if got := ProjectIndexPath("/vault", Unscoped); !strings.HasSuffix(got, "unscoped.md") {
		t.Errorf("ProjectIndexPath(Unscoped) = %q, want a path ending in unscoped.md", got)
	}
}

func TestProjectIndexBodyIsAQueryNotAList(t *testing.T) {
	body := projectIndexBody("github.com/TowardInfinity/dotfiles", 12)

	for _, want := range []string{
		"project: github.com/TowardInfinity/dotfiles",
		"```dataview",
		`WHERE project = "github.com/TowardInfinity/dotfiles"`,
		"SORT date DESC",
		"12",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("project index body missing %q:\n%s", want, body)
		}
	}
}

func TestDataviewEscape(t *testing.T) {
	cases := map[string]string{
		`plain/path`:         `plain/path`,
		`has "quotes" in it`: `has \"quotes\" in it`,
		`back\slash`:         `back\\slash`,
	}
	for in, want := range cases {
		if got := dataviewEscape(in); got != want {
			t.Errorf("dataviewEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

// A key containing a quote must not be able to break out of the query's
// string literal — the escaped form should still parenthesize as one
// self-contained WHERE clause.
func TestProjectIndexBodyEscapesQuotesInKey(t *testing.T) {
	body := projectIndexBody(`weird"project`, 1)
	if !strings.Contains(body, `WHERE project = "weird\"project"`) {
		t.Errorf("quote in project key was not escaped:\n%s", body)
	}
}

func TestWriteProjectIndexAndWriteProjectIndexes(t *testing.T) {
	vault := t.TempDir()

	path, err := WriteProjectIndex(vault, "github.com/TowardInfinity/dotfiles", 3)
	if err != nil {
		t.Fatalf("WriteProjectIndex: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("index page not written: %v", err)
	}

	// Rewriting must overwrite in place, not accumulate — this page is a
	// generated query, never hand-edited prose to preserve.
	path2, err := WriteProjectIndex(vault, "github.com/TowardInfinity/dotfiles", 9)
	if err != nil {
		t.Fatalf("WriteProjectIndex (rewrite): %v", err)
	}
	if path2 != path {
		t.Fatalf("rewrite produced a different path: %q vs %q", path2, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "across every tool: 3") || !strings.Contains(string(b), "across every tool: 9") {
		t.Errorf("rewritten page still shows the old count:\n%s", b)
	}

	idx := Index{Sessions: []Session{
		func() Session { s := testSession(); s.Project = "a/b"; return s }(),
		func() Session { s := testSession(); s.ID = "other"; s.Project = "c/d"; return s }(),
	}}
	n, err := WriteProjectIndexes(vault, idx)
	if err != nil {
		t.Fatalf("WriteProjectIndexes: %v", err)
	}
	if n != 2 {
		t.Fatalf("WriteProjectIndexes wrote %d pages, want 2", n)
	}
	for _, key := range []string{"a/b", "c/d"} {
		p := ProjectIndexPath(vault, ProjectKey(key))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected page for %q at %s: %v", key, p, err)
		}
	}
}

func TestWriteProjectIndexesEmptyIndex(t *testing.T) {
	vault := t.TempDir()
	n, err := WriteProjectIndexes(vault, Index{})
	if err != nil {
		t.Fatalf("WriteProjectIndexes on empty index: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
	entries, _ := os.ReadDir(filepath.Join(vault, "Projects"))
	if len(entries) != 0 {
		t.Errorf("expected no pages written, found %d", len(entries))
	}
}
