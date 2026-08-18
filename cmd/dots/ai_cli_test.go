package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAIResumeLaunchesResolvedBinaryWithBareArgvZero(t *testing.T) {
	binDir := t.TempDir()
	stub := filepath.Join(binDir, "claude")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	original := launchAI
	t.Cleanup(func() { launchAI = original })
	var gotPath string
	var gotArgv []string
	launchAI = func(path string, argv, _ []string) error {
		gotPath = path
		gotArgv = append([]string(nil), argv...)
		return nil
	}

	if code := runAIResumeCLI("claude", []string{"continue the work"}); code != 0 {
		t.Fatalf("runAIResumeCLI exit = %d, want 0", code)
	}
	if gotPath != stub {
		t.Errorf("exec path = %q, want %q", gotPath, stub)
	}
	if want := []string{"claude", "--continue", "continue the work"}; !reflect.DeepEqual(gotArgv, want) {
		t.Errorf("exec argv = %#v, want %#v", gotArgv, want)
	}
}

func TestAIResumeCursorUsesResolvedGitRoot(t *testing.T) {
	binDir := t.TempDir()
	stub := filepath.Join(binDir, "cursor-agent")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	original := launchAI
	t.Cleanup(func() { launchAI = original })
	var gotArgv []string
	launchAI = func(_ string, argv, _ []string) error {
		gotArgv = append([]string(nil), argv...)
		return nil
	}

	if code := runAIResumeCLI("cursor", nil); code != 0 {
		t.Fatalf("runAIResumeCLI exit = %d, want 0", code)
	}
	repo := findRepo()
	if repo == "" {
		t.Fatal("test needs the repository checkout")
	}
	if want := []string{"cursor-agent", "--workspace", repo, "resume"}; !reflect.DeepEqual(gotArgv, want) {
		t.Errorf("exec argv = %#v, want %#v", gotArgv, want)
	}
}
