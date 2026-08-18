package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/TowardInfinity/dotfiles/internal/dots/ai"
	"github.com/TowardInfinity/dotfiles/internal/dots/memory"
)

// runAIResumeCLI is deliberately small: Phase 1 is a spelling-normalizer for
// the native resume operations, not another session index. The later console
// phase adds cross-tool selection without changing this direct launch path.
func runAIResumeCLI(tool string, args []string) int {
	project := memory.Unscoped
	if tool == "cursor" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "dots %s: current directory: %v\n", tool, err)
			return 1
		}
		var gitRoot string
		project, gitRoot, _ = memory.ResolveProject(cwd)
		if gitRoot != "" {
			// Cursor otherwise uses the invocation subdirectory as its workspace.
			// The project root is the stable identity memory resolved above.
			args = append([]string{"--workspace", filepath.Clean(gitRoot)}, args...)
		}
	}

	argv, err := ai.ResolveLaunch(tool, project, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots %s: %v\n", tool, err)
		return 1
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots %s: %s is not installed or not on PATH\n", tool, argv[0])
		return 1
	}
	if err := launchAI(path, argv, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "dots %s: exec %s: %v\n", tool, argv[0], err)
		return 1
	}
	return 0
}

// launchAI is replaceable only so the CLI test can inspect the exact exec
// handoff without replacing the test process. Production always calls the
// syscall wrapper below.
var launchAI = execAILaunch

// execAILaunch is the single process handoff in this feature. The resolved
// executable path satisfies syscall.Exec while argv retains the tool's bare
// binary name as argv[0], which is what its own CLI expects to see.
func execAILaunch(path string, argv, env []string) error {
	return syscall.Exec(path, argv, env)
}
