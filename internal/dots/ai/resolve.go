package ai

import (
	"fmt"

	"github.com/TowardInfinity/dotfiles/internal/dots/memory"
)

// ResolveLaunch decides what the CLI should exec for tool in project with any
// trailing arguments. It is intentionally pure: it does not inspect PATH,
// resolve a working directory, read sessions, or start a process. Those
// responsibilities belong at the CLI edge and in the later console phase.
//
// Phase 1 always uses each tool's native cwd-scoped resume operation. The
// project key is part of the stable seam now because phase 2's explicit
// session lookup needs it; retaining it here prevents a second launch API
// whose scoping silently diverges later.
func ResolveLaunch(tool string, _ memory.ProjectKey, args []string) ([]string, error) {
	t, ok := toolForAlias(tool)
	if !ok {
		return nil, fmt.Errorf("unknown AI tool %q", tool)
	}

	var prefix []string
	switch t.Alias {
	case "claude", "grok":
		prefix = []string{t.Binary, "--continue"}
	case "codex":
		prefix = []string{t.Binary, "resume", "--last"}
	case "cursor":
		// Cursor's --workspace is a global flag, so ai_cli.go prepends it
		// before any user arguments and this resolver places it before the
		// resume subcommand. Ordinary trailing arguments remain untouched.
		if len(args) >= 2 && args[0] == "--workspace" {
			prefix = []string{t.Binary, "--workspace", args[1], "resume"}
			args = args[2:]
		} else {
			prefix = []string{t.Binary, "resume"}
		}
	}

	return append(prefix, args...), nil
}
