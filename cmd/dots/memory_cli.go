package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TowardInfinity/dotfiles/internal/dots/memory"
)

// dots memory — one memory across every AI tool on this machine, keyed by
// project rather than by directory. See docs/memory.md.

func runMemoryCLI(args []string) int {
	if len(args) == 0 {
		// Bare `dots memory` shows the topic page, consistent with `dots tmux`.
		if !printDoc("memory") {
			memoryUsage()
			return 1
		}
		return 0
	}
	switch args[0] {
	case "capture":
		return runMemoryCapture(args[1:])
	case "reindex":
		return runMemoryReindex(args[1:])
	case "recall":
		return runMemoryRecall(args[1:])
	case "search":
		return runMemorySearch(args[1:])
	case "status":
		return runMemoryStatus(args[1:])
	case "mcp":
		return runMemoryMCP(args[1:])
	case "-h", "--help", "help":
		memoryUsage()
		return 0
	}
	fmt.Fprintf(os.Stderr, "dots memory: unknown subcommand %q\n", args[0])
	memoryUsage()
	return 1
}

func memoryUsage() {
	fmt.Print(`dots memory — shared memory across your AI tools

  dots memory capture    record sessions; distil the ones that have gone quiet
                         (run from a Stop hook; silent and cheap by design)
  dots memory reindex    rescan every tool now, ignoring the idle gates
  dots memory recall     print a short digest of recent work in this project
  dots memory search     find sessions whose title or summary mentions a term
  dots memory status     what is indexed, by project and by tool
  dots memory mcp        run the stdio MCP server other tools register

Run a subcommand with -h for its flags.
`)
}

// runMemoryCapture is the hook entry point. It is silent unless asked, and it
// never returns non-zero for a background bookkeeping failure — a hook that
// fails loudly interrupts the session it was supposed to be quietly recording.
func runMemoryCapture(args []string) int {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	var (
		verbose      = fs.Bool("verbose", false, "report what the pass did")
		dryRun       = fs.Bool("dry", false, "do everything except write notes and the index")
		force        = fs.Bool("force", false, "ignore the idle and scan-interval gates")
		experimental = fs.Bool("experimental", false, "include the reverse-engineered adapters")
		vault        = fs.String("vault", "", "Obsidian vault root (default: macOS standard path)")
		max          = fs.Int("max", 0, "cap summaries produced in this pass")
		timeout      = fs.Duration("timeout", 10*time.Minute, "overall bound on the pass")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	rep, err := memory.Run(ctx, memory.CaptureOptions{
		Vault:        *vault,
		Max:          *max,
		DryRun:       *dryRun,
		Force:        *force,
		Experimental: *experimental,
	})
	if err != nil {
		if *verbose {
			fmt.Fprintf(os.Stderr, "dots memory capture: %v\n", err)
		}
		return 0
	}
	if *verbose {
		if rep.Locked {
			fmt.Println("another capture pass is running; nothing to do")
			return 0
		}
		fmt.Printf("indexed %d  distilled %d  notes %d  skipped %d\n",
			rep.Indexed, rep.Distilled, rep.NotesWrote, rep.Skipped)
		reportReconcile(rep.Reconciled)
		if rep.ProjectIndexes > 0 {
			fmt.Printf("vault: refreshed %d project index page(s)\n", rep.ProjectIndexes)
		}
		for _, e := range rep.Errs {
			fmt.Fprintf(os.Stderr, "  warn: %v\n", e)
		}
	}
	return 0
}

// reportReconcile is worth printing whenever it did anything: it moves files in
// the user's Obsidian vault, and a vault changing under you without a word is
// alarming even when the change is right.
func reportReconcile(r memory.ReconcileReport) {
	if r.Adopted == 0 && r.Renamed == 0 && r.Superseded == 0 {
		return
	}
	fmt.Printf("vault: adopted %d existing summaries, renamed %d, set aside %d duplicates\n",
		r.Adopted, r.Renamed, r.Superseded)
	if r.Superseded > 0 {
		fmt.Println("       duplicates moved to \"AI Chats/_superseded\" — review and delete when happy")
	}
}

// runMemoryReindex is the same pass with the gates off and output on. This is
// the one a person runs; capture is the one the hook runs.
func runMemoryReindex(args []string) int {
	fs := flag.NewFlagSet("reindex", flag.ContinueOnError)
	var (
		experimental = fs.Bool("experimental", false, "include the reverse-engineered adapters")
		vault        = fs.String("vault", "", "Obsidian vault root (default: macOS standard path)")
		max          = fs.Int("max", 0, "cap summaries produced in this pass")
		timeout      = fs.Duration("timeout", 30*time.Minute, "overall bound on the pass")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fmt.Println("scanning every available tool…")
	rep, err := memory.Run(ctx, memory.CaptureOptions{
		Vault:        *vault,
		Max:          *max,
		Force:        true,
		Experimental: *experimental,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dots memory reindex: %v\n", err)
		return 1
	}
	if rep.Locked {
		fmt.Println("another capture pass is already running; try again shortly")
		return 0
	}
	fmt.Printf("indexed %d sessions, distilled %d, wrote %d notes, skipped %d\n",
		rep.Indexed, rep.Distilled, rep.NotesWrote, rep.Skipped)
	reportReconcile(rep.Reconciled)
	if rep.ProjectIndexes > 0 {
		fmt.Printf("vault: refreshed %d project index page(s)\n", rep.ProjectIndexes)
	}
	for _, e := range rep.Errs {
		fmt.Fprintf(os.Stderr, "  warn: %v\n", e)
	}
	if rep.Skipped > 0 {
		fmt.Println("run again to continue distilling the backlog")
	}
	return 0
}

// runMemoryRecall prints the digest injected at session start.
//
// It reads the prebuilt index and nothing else. It must never summarize inline:
// the SessionStart hook has a 10 s timeout and Ollama takes minutes. The
// deadline is a hard bound, not an expectation — a stalled iCloud read or a
// half-written index degrades to silence and exit 0, never to a hung launch.
func runMemoryRecall(args []string) int {
	fs := flag.NewFlagSet("recall", flag.ContinueOnError)
	var (
		project  = fs.String("project", "", "project key (default: resolved from the current directory)")
		dir      = fs.String("dir", "", "resolve the project from this directory instead of the cwd")
		budget   = fs.Int("budget", 1024, "hard byte cap on the digest")
		limit    = fs.Int("limit", 6, "most sessions to list")
		deadline = fs.Duration("deadline", 2*time.Second, "print nothing and exit 0 past this")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	done := make(chan string, 1)
	go func() {
		done <- memory.Digest(memory.DigestOptions{
			Project: memory.ProjectKey(*project),
			Dir:     *dir,
			Budget:  *budget,
			Limit:   *limit,
		})
	}()

	select {
	case out := <-done:
		if strings.TrimSpace(out) != "" {
			fmt.Print(out)
		}
	case <-time.After(*deadline):
		// Silence is the correct output. This hook gates starting Claude.
	}
	return 0
}

// runMemorySearch is the on-demand counterpart to recall: recall is a small,
// unconditional digest at session start; search is what a person — or the MCP
// tools in a later phase — reaches for when that was not enough. It reads the
// prebuilt index and nothing else, so it has none of recall's deadline
// concerns: an in-memory scan over a few hundred summaries has no way to
// stall.
func runMemorySearch(args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	var (
		project = fs.String("project", "", "limit to one project key (default: every project)")
		limit   = fs.Int("limit", 20, "most results to show")
		asJSON  = fs.Bool("json", false, "print results as a JSON array")
	)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: dots memory search [flags] <query>\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	query := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(query) == "" {
		fmt.Fprintln(os.Stderr, "usage: dots memory search [flags] <query>")
		return 2
	}

	results := memory.Search(memory.LoadIndex(), memory.SearchOptions{
		Query:   query,
		Project: memory.ProjectKey(*project),
		Limit:   *limit,
	})

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			fmt.Fprintf(os.Stderr, "dots memory search: %v\n", err)
			return 1
		}
		return 0
	}

	if len(results) == 0 {
		fmt.Println("no matches")
		return 0
	}
	for _, r := range results {
		s := r.Session
		fmt.Printf("%s  [%s/%s]  %s\n", s.Updated.Format("2006-01-02"), s.Tool, s.Project, s.Title)
		if s.VaultNote != "" {
			fmt.Printf("    %s\n", s.VaultNote)
		}
	}
	return 0
}

// runMemoryMCP is the cross-tool interface: a stdio JSON-RPC server other
// MCP-capable tools register once (see docs/memory.md) and query directly,
// instead of each shelling out to this binary per lookup. It runs until
// stdin closes, which is how every MCP client ends a session.
//
// Every diagnostic goes to stderr, never stdout: stdout is the wire, and one
// stray line on it would corrupt the protocol stream for the rest of the
// session.
func runMemoryMCP(args []string) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := memory.RunMCPServer(os.Stdin, os.Stdout, os.Stderr, version); err != nil {
		fmt.Fprintf(os.Stderr, "dots memory mcp: %v\n", err)
		return 1
	}
	return 0
}

func runMemoryStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	limit := fs.Int("limit", 15, "projects to list")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	idx := memory.LoadIndex()
	if len(idx.Sessions) == 0 {
		fmt.Println("nothing indexed yet — run: dots memory reindex")
		return 0
	}

	tools := map[string]int{}
	distilled := 0
	for _, s := range idx.Sessions {
		tools[s.Tool]++
		if s.Summary != "" {
			distilled++
		}
	}

	fmt.Printf("%d sessions indexed, %d distilled, last scan %s\n",
		len(idx.Sessions), distilled, idx.Built.Format("2006-01-02 15:04"))
	fmt.Print("by tool: ")
	var parts []string
	for t, n := range tools {
		parts = append(parts, fmt.Sprintf("%s %d", t, n))
	}
	fmt.Println(strings.Join(parts, "  "))

	if v := memory.VaultDir(); v != "" {
		fmt.Printf("vault:   %s\n", v)
	} else {
		fmt.Println("vault:   not configured on this machine (index only)")
	}

	fmt.Println("\nprojects:")
	for i, p := range idx.Projects() {
		if i >= *limit {
			fmt.Printf("  … and %d more\n", len(idx.Projects())-*limit)
			break
		}
		fmt.Printf("  %s\n", p)
	}
	return 0
}
