package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
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

func TestAIResumeNativeToolsDoNotResolveProject(t *testing.T) {
	binDir := t.TempDir()
	for _, name := range []string{"claude", "git"} {
		body := "#!/bin/sh\n"
		if name == "git" {
			body += ": > \"$DOTS_AI_GIT_MARKER\"\n"
		}
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(t.TempDir(), "git-ran")
	t.Setenv("DOTS_AI_GIT_MARKER", marker)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	original := launchAI
	t.Cleanup(func() { launchAI = original })
	launchAI = func(_ string, _ []string, _ []string) error { return nil }

	if code := runAIResumeCLI("claude", nil); code != 0 {
		t.Fatalf("runAIResumeCLI exit = %d, want 0", code)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("native resume unexpectedly resolved the project; git marker error = %v", err)
	}
}

func TestAIResumeReportsMissingBinaryWithoutExec(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	original := launchAI
	t.Cleanup(func() { launchAI = original })
	launchAI = func(_ string, _ []string, _ []string) error {
		t.Fatal("launchAI called for a binary absent from PATH")
		return nil
	}

	if code := runAIResumeCLI("grok", nil); code != 1 {
		t.Fatalf("runAIResumeCLI exit = %d, want 1", code)
	}
}

func TestAIResumeCursorUsesResolvedGitRoot(t *testing.T) {
	repo := t.TempDir()
	if err := exec.Command("git", "init", "-q", repo).Run(); err != nil {
		t.Fatal(err)
	}
	gitRoot, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

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
	if want := []string{"cursor-agent", "--workspace", gitRoot, "resume"}; !reflect.DeepEqual(gotArgv, want) {
		t.Errorf("exec argv = %#v, want %#v", gotArgv, want)
	}
}

func TestAIChatGPTOpensDesktopOnMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the desktop integration is intentionally macOS-only")
	}
	original := openChatGPT
	t.Cleanup(func() { openChatGPT = original })
	called := false
	openChatGPT = func() error {
		called = true
		return nil
	}
	if code := runAIChatGPT(nil); code != 0 {
		t.Fatalf("runAIChatGPT exit = %d, want 0", code)
	}
	if !called {
		t.Fatal("runAIChatGPT did not invoke the desktop launcher")
	}
}
