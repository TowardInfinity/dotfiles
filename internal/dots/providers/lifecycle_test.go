package providers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TowardInfinity/dotfiles/internal/dots/ops"
)

func TestSSHScriptPreservesAdversarialArgumentAsData(t *testing.T) {
	dir := t.TempDir()
	fakeSSH := filepath.Join(dir, "ssh")
	if err := os.WriteFile(fakeSSH, []byte(`#!/bin/sh
last=
for arg do last=$arg; done
exec /bin/sh -c "$last"
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	captured := filepath.Join(dir, "captured")
	sentinel := filepath.Join(dir, "injected")
	t.Setenv("CAPTURED", captured)
	value := "cwd'; touch '" + sentinel + "'; #"
	call := SSHScript{
		Host:   "test-host",
		Args:   []string{value},
		Script: `printf '%s\n' "$1" > "$CAPTURED"` + "\n",
	}
	if err := call.Run(context.Background(), ops.IO{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(captured)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSuffix(string(got), "\n") != value {
		t.Fatalf("remote argument = %q, want %q", strings.TrimSuffix(string(got), "\n"), value)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("adversarial argument executed shell syntax: %v", err)
	}
}

func TestSSHScriptRejectsOptionLikeHost(t *testing.T) {
	err := (SSHScript{Host: "-oProxyCommand=bad", Script: "true\n"}).Run(context.Background(), ops.IO{})
	if err == nil || !strings.Contains(err.Error(), "unsafe SSH host") {
		t.Fatalf("error = %v", err)
	}
}
