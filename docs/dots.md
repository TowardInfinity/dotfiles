---
title: Subcommands
group: dots
order: 10
summary: The command you are already running — every subcommand and what it's for
---

# Subcommands

Every other page here documents a tool. This one documents the thing you are
reading them in — split across a few pages now that it covers this much: this
one is the CLI surface, `dots dots-panes` is the TUI, `dots dots-doctor` is
what health checks it runs, `dots dots-pages` is how a page like this one gets
written.

`dots` is two programs wearing one name. With no arguments it opens a
full-screen browser, which is what you want when you are looking *for*
something. With a subcommand it prints and exits, which is what you want in a
script, in a pipe, or when you already know the answer's name and do not want a
full-screen program in the way.

```sh
dots              # browse
dots tmux         # print the tmux page
dots search copy  # find which page mentions it
```

## Subcommands

| | |
|---|---|
| `dots` | browse interactively |
| `dots <topic>` | print one page |
| `dots search <term>` | search every page |
| `dots topics` | list every page with its summary |
| `dots memory` | shared memory across your AI tools; see `dots memory help` |
| `dots claude [args…]` | resume Claude's latest session in this project |
| `dots codex [args…]` | resume Codex's latest session in this project |
| `dots grok [args…]` | resume Grok's latest session in this project |
| `dots cursor [args…]` | resume Cursor's latest session in this project |
| `dots doctor` | check the tools these configs call, and the configuration itself |
| `dots doctor --online` | also check the installed `dots` against the latest release |
| `dots status [--online] [--fleet] [--json]` | report state; never repair |
| `dots apply [--dry-run]` | relink and merge locally, with no network |
| `dots deps [--dry-run] [-y]` | install dependencies explicitly |
| `dots publish <paths…> -m "…"` | validate, commit only the selection, and push; never roll out |
| `dots rollout <hosts…> [--revision …]` | apply one published revision to selected machines |
| `dots install` | compatibility alias for Apply; `--deps` routes to `dots deps` |
| `dots sync [--check] [--dry-run]` | safely fetch, fast-forward, and apply this machine; never pushes or SSHes |
| `dots update` | deprecated alias for `dots sync` |
| `dots edit [topic]` | open a page in `$EDITOR` |
| `dots path` | print the repo path |
| `dots version` | print this binary's version |

The scopes and safety boundaries are listed together in `dots operations`.

### install, update and docs are verbs first

`dots install` and `dots update` *do* things. The pages of those names are one
level down:

```sh
dots docs install
dots docs update
```

Everything else is a page directly: `dots tmux`, `dots models`, `dots zsh`.

### It behaves in a pipe

Colour and word-wrapping are dropped when stdout is not a terminal, so
`dots tmux | grep prefix` reads like a text file rather than a screenshot.
`dots doctor` exits non-zero when something is missing, so it works as a CI or
script step. `dots search` exits non-zero on no match.
