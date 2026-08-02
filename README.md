# dotfiles

Terminal setup for macOS and Linux — Ghostty, zsh, tmux, Neovim. One command
gets a new Mac, or a fresh Ubuntu server, to the same place — see
[Quick start](#quick-start). Currently on: this MacBook and the `a1` / `v1` /
`v2` Ubuntu boxes.

```
common/nvim/       NvChad-based Neovim config — shared by macOS and Linux
macos/
  zsh/.zshrc       Homebrew, conda, iTerm2, launchctl aliases
  tmux/tmux.conf   prefix Ctrl-a, pbcopy clipboard
  ghostty/config   terminal (macOS only — servers are headless)
linux/
  zsh/.zshrc       apt, ss, TERM fallback, glow-backed cat
  tmux/tmux.conf   prefix Ctrl-b, OSC 52 clipboard, targets tmux 3.2a
bootstrap.sh       the curl'd one-liner — clones, optional packages, then runs:
install.sh         detects the OS and links the right set
```

`install.sh` reads `uname -s` and links `common/` plus either `macos/` or
`linux/`. Neovim is genuinely OS-agnostic — no Homebrew paths, no `pbcopy` —
so it lives in `common/` and never drifts between machines.

The shells are *not* shared. Almost nothing overlaps between them (Homebrew vs
apt, `launchctl` vs systemd, `pbcopy` vs OSC 52), so two files beat one file
full of `if [[ $OSTYPE ]]`.

## Quick start

One command on a new machine — macOS or Ubuntu:

```sh
sh -c "$(curl -fsSL https://toin.in/install)"
```

With packages installed too:

```sh
sh -c "$(curl -fsSL https://toin.in/install)" -- --deps
```

**No GitHub account needed.** This repo is public, so a machine you will never
log into GitHub from can still install from it. If the machine happens to have
an SSH key on the account, the clone uses SSH so you can push config changes
back; otherwise it clones read-only over HTTPS and says so. It never stops to
ask for credentials.

That one-liner is `bootstrap.sh` in this repo, served through a Worker route on
`toin.in` so the public command stays stable even when the file moves.
`https://toin.in/install.sh` is kept working as an alias.

### Options

| Flag | Effect |
|---|---|
| *(none)* | clone or update, symlink, and install the configs' own dependencies |
| `--deps` | also install packages — brew on macOS, apt + the Neovim tarball on Ubuntu |
| `--dry` | print what would happen, change nothing |
| `--copy` | keep no repo: fetch a tarball, copy files into place, discard it |
| `--nvim` | also restore nvim plugins from `lazy-lock.json` after linking |

**Oh My Zsh and TPM install on every run, not just under `--deps`.** They are not
optional packages: `.zshrc` does `source $ZSH/oh-my-zsh.sh` and `tmux.conf`
loads TPM plugins, so linking those configs without them gives you a shell that
errors on every prompt and a tmux with no plugins. `--deps` is for things the
configs *call* (ripgrep, fzf, a Node manager); this is for things they *are*.

### Why there's a clone at all

The configs are **symlinks into the repo**, so `~/.config/nvim/lua/options.lua`
and the tracked file are the same file. Edit either path, commit from the repo,
`git pull` to update. The whole `.git` costs 352 KB.

`--copy` skips it — real files, nothing left behind. Worth it for a container
or a box you'll destroy tomorrow. Not worth it for a machine you work on: the
files stop being tracked, local edits are invisible to git, and updating means
re-running the installer instead of `git pull`.

```sh
# throwaway machine, no repo kept
sh -c "$(curl -fsSL https://toin.in/install)" -- --copy
```

Set `DOTFILES_DIR=/some/path` to clone somewhere other than the default
(`~/Codes/Projects/dotfiles` on macOS, `~/Codes/dotfiles` on Linux).

**Always safe to re-run.** Existing files are moved to
`<path>.backup.<timestamp>` rather than overwritten, and links already pointing
at the repo are left alone. Try `--dry` first if unsure.

### Manual, if you'd rather

```bash
git clone git@github.com:TowardInfinity/dotfiles.git ~/Codes/dotfiles
cd ~/Codes/dotfiles
./install.sh --dry
./install.sh
```

`install.sh` only does the linking. Cloning, package installation and `--copy`
live in `bootstrap.sh`, so there is exactly one copy of that logic.

The two are split by what they can assume, not by what they do: `bootstrap.sh`
is POSIX `sh` because it runs on a machine where nothing is installed yet,
while `install.sh` is bash and only ever runs from a clone.

### Prerequisites

`--deps` installs all of this; listed here for reference. The list is driven by
what the shell configs actually call — if `.zshrc` references a tool, it is
here, otherwise a fresh machine errors on every prompt.

| | macOS | Linux |
|---|---|---|
| editor | neovim | neovim (upstream tarball — apt's is too old) |
| terminal | tmux, ghostty, JetBrainsMono Nerd Font | tmux |
| shell | *(Oh My Zsh — always, see above)* | *(+ zsh-autosuggestions, zsh-syntax-highlighting)* |
| CLI | fzf, zoxide, eza, bat, ripgrep, fd, jq, lazygit, btop, glow | jq, ripgrep, glow |
| languages | uv, pnpm, fnm, go | uv, pnpm, fnm, go |
| notebooks | jupyterlab (via `uv tool`, not brew) | jupyterlab (via `uv tool`) |
| git | git, gh | git, gh |

Version managers rather than languages: **fnm** for Node and **uv** for Python,
so versions stay per-project. Java is left to SDKMAN, which `.zshrc` sources if
present.

Already-installed tools are skipped. The check looks in `~/.local/bin`,
`~/go/bin` and the pnpm directory as well as `PATH`, because a non-interactive
shell has not sourced `.zshrc` and a bare `command -v` would miss them.

The status bars assume a Nerd Font — **JetBrainsMono Nerd Font** — installed on
whatever machine you *look* at, i.e. the Mac. Servers need nothing extra;
glyphs are rendered by your local terminal.

### Machine-specific bits

Anything true of one box only goes in `~/.zshrc.local`, which is sourced if
present and never tracked. That keeps `linux/zsh/.zshrc` byte-identical across
every server.

## How the pieces fit

Everything shares one **Tokyo Night** palette: Ghostty's colours, tmux's status
bar, and NvChad's theme all match on purpose.

**Prefixes differ by design.** The Mac's tmux uses `Ctrl-a`; the remote VM keeps
the stock `Ctrl-b`. Nested sessions therefore never fight — `Ctrl-a` acts on the
Mac, `Ctrl-b` passes through to the VM — and the status bar is blue locally,
green on the VM, so a glance tells you which layer you're in.

Clipboard is **OSC 52** end to end, which is why copying inside a tmux session
on the VM still lands on the macOS clipboard, through both tmux layers and the
SSH pipe. Don't "fix" the VM config by adding `xclip`.

## Updating a server

The repo is cloned on the servers too, so there is no `scp` step any more:

```bash
ssh a1
cd ~/Codes/dotfiles && git pull
tmux source-file ~/.config/tmux/tmux.conf     # NOT kill-server
```

These boxes run long-lived sessions — `source-file` applies the config in place
and leaves them alone. To check a risky change first, load it on a throwaway
socket where it cannot touch anything live:

```bash
tmux -L probe -f linux/tmux/tmux.conf new-session -d \
  && tmux -L probe show-messages | grep -i error
tmux -L probe kill-server
```

`linux/tmux/tmux.conf` targets **tmux 3.2a** (Ubuntu 22.04's version, older than
the Mac's 3.7b) and assumes no `fzf`: no `allow-passthrough`, `choose-tree`
pickers instead of fzf popups.

## Gotchas worth remembering

**`%` inside a tmux `#()` job.** tmux runs `status-right` through `strftime`
*before* executing `#()`, so `date +%H:%M` becomes `date +14:29` and silently
echoes back the wrong timezone. Escape it as `%%H:%%M`.

**Stray mouse-reporting after a dropped SSH session.** The VM's tmux enables
mouse mode; if the link dies uncleanly it never sends the disable sequence, and
the local terminal starts turning mouse movement into fake keystrokes
(`zsh: command not found: 35`). `.zshrc` clears mouse mode before every prompt
to make that unreachable; `fixterm` is the manual rescue.

**nvim-treesitter must stay on `master`.** Upstream made `main` (the rewrite)
the default branch, but NvChad v2.5 drives the old API and
`require("nvim-treesitter.configs")` exists only on `master`. The spec pins
`branch = "master"` for this reason. Without it a fresh clone installs `main`,
every parser fails with *module 'nvim-treesitter.configs' not found*, and you
get zero parsers.

**`lazy-lock.json` is a snapshot, not a pin.** It records a `branch` per plugin,
but lazy only ever runs `git checkout <sha>` — never `git checkout <branch>` —
so `:Lazy restore` does not honour that field. An unpinned plugin follows
whatever the remote's default branch was *on the day that machine cloned it*,
frozen in its `origin/HEAD`. Two boxes set up months apart land on different
branches with nobody running a wrong command, and because lazy rewrites the
whole lockfile from disk after every operation, the drift gets recorded as
truth. Only `branch =` in the spec enforces anything — which is why
`nvim-treesitter`, `nvchad/base46` and `nvchad/ui` all carry one.

**Switching a machine off another nvim distro? Clear `~/.local/share/nvim/site/`.**
nvim-treesitter's `main` branch installs parsers and queries there, and `site/`
sits *earlier* on the runtimepath than the plugin directory — so leftovers
shadow the new ones. On a1 this surfaced as
`Query error … Invalid node type "except*"` for Python: a four-day-old parser
being used against a current query. Deleting `site/parser`, `site/parser-info`
and `site/queries` fixed it.

**`nvim/lua/configs/tscompat.lua` is load-bearing.** nvim-treesitter's master
branch registers query directives with `all = false`, which Neovim 0.12 removed
— directives now receive a *list* of nodes, and the plugin still treats them as
a single node, which crashes Markdown highlighting. That file re-registers the
affected directives. Delete it only once nvim-treesitter fixes it upstream.

**NvChad's `opts` discards yours unless you use the function form.** NvChad
declares `opts = function() return require "nvchad.configs.treesitter" end`,
which ignores its arguments. A plain `opts = { ... }` in a user spec is silently
thrown away. Always use `opts = function(_, opts) ... return opts end`.

## Not included

`~/.ssh/config` is deliberately absent — it holds server addresses. Machine-local
state (tmux plugins and resurrect snapshots, nvim's `lazy`/`mason` downloads)
is gitignored; `install.sh` and the package managers regenerate all of it.
`nvim/lazy-lock.json` *is* tracked, so plugin versions are reproducible.
