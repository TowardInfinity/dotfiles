package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Builds something isRepo() accepts: install.sh plus a docs directory.
func fakeCheckout(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The regression: with a release binary, dots resolves its own symlink into
// ~/.cache/dots, which has no route back to the checkout. A repo at a custom
// DOTFILES_DIR was then invisible to `dots update`, `sync`, `path` and
// `install` in every shell where that variable was no longer exported.
// install.sh records the path so a later shell can still find it.
func TestRecordedRepoFoundWithoutTheEnvVar(t *testing.T) {
	home := t.TempDir()
	repo := fakeCheckout(t, filepath.Join(home, "somewhere", "custom", "dotfiles"))

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	os.Unsetenv("DOTFILES_DIR")

	if got := recordedRepo(); got != "" {
		t.Fatalf("recordedRepo() = %q before anything was recorded, want empty", got)
	}

	cfg := filepath.Join(home, ".config", "dots")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	// Trailing newline is what `printf '%s\n'` writes; it must be trimmed.
	if err := os.WriteFile(filepath.Join(cfg, "repo"), []byte(repo+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := recordedRepo(); got != repo {
		t.Errorf("recordedRepo() = %q, want %q", got, repo)
	}
	if got := findRepo(); got != repo {
		t.Errorf("findRepo() = %q, want %q", got, repo)
	}
}

// XDG_CONFIG_HOME wins over ~/.config when set, or the record is written to
// one place and read from another.
func TestRecordedRepoHonoursXDG(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	repo := fakeCheckout(t, filepath.Join(home, "elsewhere"))

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	os.Unsetenv("DOTFILES_DIR")

	if err := os.MkdirAll(filepath.Join(xdg, "dots"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "dots", "repo"), []byte(repo), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := recordedRepo(); got != repo {
		t.Errorf("recordedRepo() = %q, want %q", got, repo)
	}
}

// A recorded path that no longer holds a checkout must not be returned — the
// repo may have been deleted or moved since the last install.
func TestStaleRecordIsIgnored(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	os.Unsetenv("DOTFILES_DIR")

	cfg := filepath.Join(home, ".config", "dots")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(home, "deleted-checkout")
	if err := os.WriteFile(filepath.Join(cfg, "repo"), []byte(gone+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := findRepo(); got == gone {
		t.Errorf("findRepo() returned %q, which is not a checkout", got)
	}
}
