package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	dotfiles "github.com/TowardInfinity/dotfiles"
)

// doc is one page of the reference.
type doc struct {
	Name    string // file stem, and the CLI argument
	Title   string
	Group   string
	Order   int
	Summary string
	Body    string // markdown, front-matter stripped
}

// groupOrder fixes the sidebar order. Alphabetical would put Maintain before
// Neovim and Start last, which is nobody's reading order.
var groupOrder = map[string]int{
	"Start":     0,
	"tmux":      1,
	"zsh":       2,
	"Neovim":    3,
	"Reference": 4,
	"Maintain":  5,
}

// loadDocs prefers docs on disk and falls back to the embedded copy.
//
// On a machine with a checkout, the files are the truth and edits show up
// immediately without rebuilding. Everywhere else the embedded copy makes the
// binary self-sufficient. Preferring disk also means `dots` in a repo can never
// show you something staler than the repo itself.
func loadDocs(repo string) ([]doc, string) {
	if repo != "" {
		dir := filepath.Join(repo, "docs")
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			var out []doc
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				b, err := os.ReadFile(filepath.Join(dir, e.Name()))
				if err != nil {
					continue
				}
				out = append(out, parseDoc(strings.TrimSuffix(e.Name(), ".md"), string(b)))
			}
			if len(out) > 0 {
				sortDocs(out)
				return out, dir
			}
		}
	}

	var out []doc
	_ = fs.WalkDir(dotfiles.DocsFS, "docs", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		b, err := dotfiles.DocsFS.ReadFile(p)
		if err != nil {
			return nil
		}
		out = append(out, parseDoc(strings.TrimSuffix(filepath.Base(p), ".md"), string(b)))
		return nil
	})
	sortDocs(out)
	return out, "(embedded)"
}

func sortDocs(d []doc) {
	sort.SliceStable(d, func(i, j int) bool {
		gi, gj := groupOrder[d[i].Group], groupOrder[d[j].Group]
		if gi != gj {
			return gi < gj
		}
		if d[i].Order != d[j].Order {
			return d[i].Order < d[j].Order
		}
		return d[i].Title < d[j].Title
	})
}

// parseDoc splits the YAML-ish front-matter off the body.
//
// Hand-rolled rather than pulling in a YAML parser: the format is five known
// scalar keys, and a dependency whose failure mode is "the docs will not open"
// is not worth it for that.
func parseDoc(name, raw string) doc {
	d := doc{Name: name, Title: name, Group: "Reference", Order: 999, Body: raw}

	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(raw, "---\n") {
		return d
	}
	end := strings.Index(raw[4:], "\n---")
	if end < 0 {
		return d
	}
	head := raw[4 : 4+end]

	// body currently starts at the "\n---" that closes the front-matter. Drop
	// that newline, then everything up to and including the end of the "---"
	// line. Skipping only to the first newline leaves the "---" behind, and
	// markdown then renders it as a horizontal rule above the title.
	body := strings.TrimPrefix(raw[4+end:], "\n")
	if i := strings.Index(body, "\n"); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}
	d.Body = strings.TrimLeft(body, "\n")

	for _, line := range strings.Split(head, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "title":
			d.Title = v
		case "group":
			d.Group = v
		case "order":
			if n, err := strconv.Atoi(v); err == nil {
				d.Order = n
			}
		case "summary":
			d.Summary = v
		}
	}
	return d
}

// findRepo walks up from the executable, resolving symlinks, looking for the
// checkout. Mirrors what the bash version does — the binary usually lives in
// ~/.local/bin as a symlink into the repo.
func findRepo() string {
	if v := os.Getenv("DOTFILES_DIR"); v != "" {
		if isRepo(v) {
			return v
		}
	}
	exe, err := os.Executable()
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dir := filepath.Dir(exe)
		for i := 0; i < 4; i++ {
			if isRepo(dir) {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	for _, c := range []string{
		filepath.Join(os.Getenv("HOME"), "Codes", "Projects", "dotfiles"),
		filepath.Join(os.Getenv("HOME"), "Codes", "dotfiles"),
	} {
		if isRepo(c) {
			return c
		}
	}
	return ""
}

func isRepo(dir string) bool {
	if dir == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "install.sh")); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "docs"))
	return err == nil
}
