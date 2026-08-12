---
title: Tools
group: Reference
order: 20
summary: CLI tools installed by --deps
---

# Tools

Installed by `--deps`. The list is driven by what the configs actually
call — if `.zshrc` references a tool, it is here.

| Tool | What it is for | Where |
| --- | --- | --- |
| `neovim` | The editor. On Ubuntu it comes from the upstream tarball — apt's is too old | both |
| `tmux` | Terminal multiplexer | both |
| `ghostty` | The terminal itself — servers are headless and need none | mac |
| JetBrainsMono NF | Nerd Font — supplies the status bar glyphs. Only needed where you *look* | mac |
| `ripgrep` | Fast search, used by `rg` and by Telescope | both |
| `fzf` | Fuzzy finder — history search, file search, tmux pickers | mac |
| `zoxide` | Smart `cd` that learns the directories you use | mac |
| `eza` | Backs `ls`, `ll`, `la`, `tree` | mac |
| `bat` | Backs `cat` | mac |
| `lazygit` | Git UI — `lg`, and the tmux `g` popup | mac |
| `glow` | Markdown renderer — backs Linux's `cat` | both |
| `jq` | JSON on the command line | both |
| `fd` | Friendlier `find` | mac |
| `btop` | Process and resource monitor | mac |
| `uv` | Python packages and virtualenvs — the default for Python work here | both |
| `jupyterlab` | Notebooks, installed via `uv tool` so each project registers its own kernel | both |
| `fnm` | Node version manager — switches automatically on `.nvmrc` | both |
| `pnpm` | Node packages — preferred over npm | both |
| `go` | Go toolchain | both |
| `gh` | GitHub CLI — convenience only now the repo is public | both |
| Oh My Zsh | Installed on every run, not just `--deps` | both |
| TPM | tmux plugin manager. `prefix I` installs plugins | both |
| SDKMAN | Java version manager. Both `.zshrc` files source it, and Neovim points jdtls at `~/.sdkman/candidates/java/current` | both |
| `java` | Installed *through* SDKMAN, not directly — `sdk install java` | both |

Version managers rather than languages — **fnm** for Node, **uv** for Python,
**SDKMAN** for Java — so versions stay per-project. `--deps` installs the
manager and a default JDK; `sdk install java <version>` gets you others. `dots
version-managers` has the actual install/list/switch/remove commands for all
three.

## AI CLIs — opt in with `--ai`

Not part of `--deps`, and deliberately so: nothing in these configs calls
them, which is the line `--deps` draws, and installing three of them on every
server would be disk and noise on a free-tier box for no benefit.

```sh
sh -c "$(curl -fsSL https://toin.in/install)" -- --ai
```

| Tool | Installed via |
|---|---|
| `claude` | the official installer, lands in `~/.local/bin` |
| `codex` | `pnpm add -g @openai/codex`, falling back to npm |
| `opencode` | the official installer, lands in `~/.opencode/bin` |

Each is skipped if already present, so the flag is safe to re-run.

## Repository checks

`--deps` also provisions the repository's pinned Go analyzer, Staticcheck, for
`./bin/lint.sh`. Its version is shared by CI and the local installer through
`tools/go-tools.env`; it is a development check, not a shell runtime
dependency. The light profile deliberately does not install it.
