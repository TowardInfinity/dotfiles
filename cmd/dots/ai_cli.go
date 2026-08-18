package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

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
	return execAI(tool, argv)
}

func execAI(tool string, argv []string) int {
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

func currentAIProject() (memory.ProjectKey, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	project, gitRoot, _ := memory.ResolveProject(cwd)
	return project, gitRoot, nil
}

func runAIAgentCLI(args []string) int {
	project, gitRoot, err := currentAIProject()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots agent: current directory: %v\n", err)
		return 1
	}

	tool, ref, _, ok := ai.BestTool(project)
	var argv []string
	if !ok {
		// Claude is the configured interactive default. A project with no
		// local history should still get a useful fresh session in one command.
		tool = "claude"
		argv, err = ai.ResolveFreshLaunch(tool, args)
	} else {
		if tool == "cursor" && gitRoot != "" {
			args = append([]string{"--workspace", filepath.Clean(gitRoot)}, args...)
		}
		argv, err = ai.ResolveSessionLaunch(tool, project, ref, args)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots agent: %v\n", err)
		return 1
	}
	return execAI(tool, argv)
}

func runAIConsoleCLI(args []string) int {
	if len(args) == 0 {
		return printAIRecent()
	}
	switch args[0] {
	case "usage":
		return runAIUsage(args[1:])
	case "chatgpt":
		return runAIChatGPT(args[1:])
	case "-h", "--help", "help":
		aiUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "dots ai: unknown subcommand %q\n", args[0])
		aiUsage()
		return 1
	}
}

func aiUsage() {
	fmt.Print(`dots ai — local AI sessions in this project

  dots ai                 list recent sessions across every local AI tool
  dots ai usage           local activity for the last 5 hours and 7 days
  dots ai usage --since 24h
  dots ai chatgpt         open ChatGPT desktop (macOS only)

  dots agent [args…]      resume the tool most recently used in this project
`)
}

func runAIChatGPT(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "usage: dots ai chatgpt")
		return 2
	}
	if runtime.GOOS != "darwin" {
		fmt.Println("no CLI or app integration for ChatGPT desktop on this platform")
		return 0
	}
	if err := openChatGPT(); err != nil {
		fmt.Fprintf(os.Stderr, "dots ai chatgpt: %v\n", err)
		return 1
	}
	return 0
}

var openChatGPT = func() error {
	return exec.Command("open", "-a", "ChatGPT").Run()
}

func printAIRecent() int {
	project, _, err := currentAIProject()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots ai: current directory: %v\n", err)
		return 1
	}
	refs := ai.RecentSessions(project)
	if len(refs) == 0 {
		fmt.Println("no local AI sessions for this project — dots agent starts a fresh Claude session")
		return 0
	}

	titles := map[string]string{}
	for _, s := range memory.LoadIndex().Sessions {
		if s.Project != project || s.Title == "" {
			continue
		}
		titles[s.Tool+"\x00"+s.ID] = s.Title
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TOOL\tUPDATED\tTITLE\tSESSION")
	for _, ref := range refs {
		name := ref.Tool
		if ref.Tool == "claude" {
			name = "claude-code"
		}
		title := titles[name+"\x00"+ref.ID]
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ref.Tool, ref.Updated.Local().Format("2006-01-02 15:04"), oneLine(title), ref.ID)
	}
	_ = w.Flush()
	return 0
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func runAIUsage(args []string) int {
	fs := flag.NewFlagSet("usage", flag.ContinueOnError)
	since := fs.Duration("since", 0, "one local-activity window, e.g. 5h or 7d")
	projectFlag := fs.String("project", "", "project key (default: current project)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "usage: dots ai usage [--since duration] [--project key]")
		return 2
	}
	project := memory.ProjectKey(*projectFlag)
	if project == "" {
		var err error
		project, _, err = currentAIProject()
		if err != nil {
			fmt.Fprintf(os.Stderr, "dots ai usage: current directory: %v\n", err)
			return 1
		}
	}

	windows := []time.Duration{5 * time.Hour, 7 * 24 * time.Hour}
	if *since > 0 {
		windows = []time.Duration{*since}
	}
	for i, window := range windows {
		if i > 0 {
			fmt.Println()
		}
		printAIUsageReport(ai.UsageSince(project, time.Now().Add(-window)), window)
	}
	return 0
}

func printAIUsageReport(report ai.UsageReport, window time.Duration) {
	fmt.Printf("local activity in the last %s\n", window)
	fmt.Println("Local activity only — excludes claude.ai / chatgpt.com web usage on the same account, which shares this plan's rate limit.")
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TOOL\tINPUT\tCACHE WRITE\tCACHE READ\tOUTPUT\tTHINKING\tMESSAGES")
	for _, tool := range []string{"claude", "codex", "grok", "cursor"} {
		u := report.Tool(tool)
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\n", tool, u.Input, u.CacheCreate, u.CacheRead, u.Output, u.Thinking, u.Messages)
	}
	_ = w.Flush()
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
