---
title: AI launches
group: dots
order: 36
summary: Resume Claude, Codex, Grok, or Cursor in the current project with one consistent command
---

# AI launches

Each AI CLI has its own spelling for “continue where I left off.” `dots` gives
the four installed tools the same project-scoped entry point without replacing
their own session picker, history, or configuration.

```sh
dots claude              # claude --continue
dots codex               # codex resume --last
dots grok                # grok --continue
dots cursor              # cursor-agent --workspace <project-root> resume
dots agent               # resume the tool most recently used in this project
dots ai                  # show recent local sessions across every tool
dots ai usage            # local activity for the last 5 hours and 7 days
```

The commands replace the current process with the selected CLI, so its normal
terminal controls, signals, and exit status behave exactly as if it had been
launched directly. Any trailing arguments are forwarded to that CLI.

## Project scope

Claude, Codex, and Grok all select their latest session from the current
working directory. `dots` preserves that native behaviour. Cursor receives the
resolved Git root explicitly, so invoking `dots cursor` from a subdirectory
does not create a second workspace identity.

Project identity follows `dots memory`: Git remote first, then Git root, then
the current directory. Phase 1 uses the tools' own cwd-scoped resume behaviour;
the later cross-tool console will use the same identity to select a session
without relying on an index or local model.

ChatGPT has no CLI resume surface. On macOS, `dots ai chatgpt` can only open
the desktop app; it cannot select or resume a particular chat.

## Agent and local activity

`dots agent` does not wait for `dots memory`, its index, a capture pass, or
Ollama. It makes a small metadata pass over each tool's own session files and
resumes the most recently touched session for this project. If the project has
no local session yet, it starts a fresh Claude session instead.

`dots ai` lists those same locally discovered sessions. It enriches a row with
the title from the memory index when available, but an absent or stale index
simply displays `(untitled)`.

`dots ai usage` reports local transcript activity. Claude records per-turn
token components, while Codex writes cumulative snapshots and is counted as
deltas between snapshots. Grok and Cursor expose only local message counts.
None of these numbers is a subscription allowance or a bill:

> Local activity only — excludes claude.ai / chatgpt.com web usage on the same
> account, which shares this plan's rate limit.
