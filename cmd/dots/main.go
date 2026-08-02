package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// version is stamped at release time by the workflow's
//
//	-ldflags "-X main.version=<tag>"
//
// It stayed "dev" until now because the variable did not exist and the linker
// silently no-ops a -X for an unknown symbol — so every release binary would
// have been unable to say which release it was.
var version = "dev"

// dots — reference and maintenance for this terminal setup.
//
// Two front doors on purpose. No arguments gives the TUI, which is for
// browsing and for anything stateful. A subcommand prints and exits, which is
// what you want from a script, from a pipe, or when you already know the
// answer's name and do not want a full-screen program for it.
func main() {
	args := os.Args[1:]

	// A bare `--` survives `sh -c "$(curl ...)" -- tmux`; drop it.
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}

	if len(args) == 0 {
		runTUI()
		return
	}

	switch args[0] {
	case "-h", "--help", "help":
		usage()
	case "version", "--version", "-v":
		fmt.Println(version)
	case "path":
		repo := findRepo()
		if repo == "" {
			fmt.Fprintln(os.Stderr, "dots: no checkout found")
			os.Exit(1)
		}
		fmt.Println(repo)
	case "topics", "list":
		docs, _ := loadDocs(findRepo())
		for _, d := range docs {
			fmt.Printf("%-18s %s\n", d.Name, d.Summary)
		}
	case "search":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: dots search <term>")
			os.Exit(1)
		}
		if !searchDocs(strings.Join(args[1:], " ")) {
			os.Exit(1)
		}
	case "doctor":
		os.Exit(runDoctorCLI())
	case "install":
		os.Exit(runInstallCLI(args[1:]))
	case "update":
		os.Exit(runUpdateCLI(args[1:]))
	case "docs":
		os.Exit(runDocsCLI(args[1:]))
	case "edit":
		editDoc(args[1:])
	default:
		if !printDoc(args[0]) {
			os.Exit(1)
		}
	}
}

func runTUI() {
	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "dots: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`dots — terminal setup reference

  dots                 browse interactively
  dots <topic>         print one page
  dots search <term>   search every page
  dots doctor          check the tools these configs call
  dots install         relink configs; --deps also installs the tools
  dots update          pull the latest configs, then relink
  dots topics          list every page
  dots edit [topic]    open a page in $EDITOR
  dots path            print the repo path
  dots version         print the version of this binary

install, update and docs are commands, not pages. The pages of those names
are at: dots docs install, dots docs update.

In the TUI: 1/2/3 or tab switch panes, / filters, j/k moves, q quits.
`)
}

// printDoc renders one page to stdout. Colour and word-wrap are dropped when
// stdout is not a terminal so `dots tmux | grep` behaves like a text file.
func printDoc(name string) bool {
	docs, _ := loadDocs(findRepo())
	var found *doc
	for i := range docs {
		if docs[i].Name == name || strings.EqualFold(docs[i].Title, name) {
			found = &docs[i]
			break
		}
	}
	if found == nil {
		fmt.Fprintf(os.Stderr, "dots: no such page: %s\nTry: dots topics\n", name)
		return false
	}

	if !isTTY() {
		fmt.Println(found.Body)
		return true
	}

	r, err := newRenderer(termWidth() - 4)
	if err != nil {
		fmt.Println(found.Body)
		return true
	}
	out, err := r.Render(found.Body)
	if err != nil {
		fmt.Println(found.Body)
		return true
	}
	fmt.Print(out)
	return true
}

func searchDocs(term string) bool {
	docs, _ := loadDocs(findRepo())
	q := strings.ToLower(term)
	hits := 0

	for _, d := range docs {
		var lines []string
		for _, line := range strings.Split(d.Body, "\n") {
			if strings.Contains(strings.ToLower(line), q) {
				lines = append(lines, cleanLine(line))
			}
		}
		if len(lines) == 0 {
			continue
		}
		hits++
		fmt.Printf("\n%s %s\n", styTitle.Render(d.Name), styMuted.Render(d.Group))
		for _, l := range lines {
			fmt.Printf("  %s\n", l)
		}
	}

	if hits == 0 {
		fmt.Fprintf(os.Stderr, "dots: nothing matched: %s\n", term)
		return false
	}
	return true
}

// cleanLine turns a raw markdown table row into something readable in results.
// `| Space y | Yank to the clipboard | n v |` is worse than useless as a hit.
func cleanLine(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "|") {
		s = strings.Trim(s, "|")
		parts := strings.Split(s, "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		s = strings.Join(parts, styMuted.Render("  ·  "))
	}
	s = strings.ReplaceAll(s, "`", "")
	return s
}

func editDoc(args []string) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nvim"
	}
	repo := findRepo()
	if repo == "" {
		fmt.Fprintln(os.Stderr, "dots: no checkout to edit — this is an embedded copy")
		os.Exit(1)
	}
	target := repo
	if len(args) > 0 {
		p := filepath.Join(repo, "docs", args[0]+".md")
		if _, err := os.Stat(p); err != nil {
			fmt.Fprintf(os.Stderr, "dots: no such page: %s\n", args[0])
			os.Exit(1)
		}
		target = p
	}
	// $EDITOR routinely carries arguments — "code --wait", "subl -w",
	// "vim -u NONE". Passing the whole string as one executable path meant
	// looking for a binary literally named "code --wait", which fails, and the
	// error was swallowed: bare exit 1, nothing on stderr, so `dots edit`
	// simply appeared to do nothing.
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		fmt.Fprintln(os.Stderr, "dots: $EDITOR is empty")
		os.Exit(1)
	}
	// Copy rather than append onto parts[1:] directly: append can write into
	// the backing array and clobber a later element.
	argv := append(append([]string{}, parts[1:]...), target)

	cmd := exec.Command(parts[0], argv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "dots: %s: %v\n", editor, err)
		os.Exit(1)
	}
}
