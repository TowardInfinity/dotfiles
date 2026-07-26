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
bootstrap.sh       new machine: clone + optionally install packages + link
install.sh         detects the OS and links the right set
```

`install.sh` reads `uname -s` and links `common/` plus either `macos/` or
`linux/`. Neovim is genuinely OS-agnostic — no Homebrew paths, no `pbcopy` —
so it lives in `common/` and never drifts between machines.

The shells are *not* shared. Almost nothing overlaps between them (Homebrew vs
apt, `launchctl` vs systemd, `pbcopy` vs OSC 52), so two files beat one file
full of `if [[ $OSTYPE ]]`.

## Quick start

**This repo is private**, so there is no `curl … | bash` one-liner — an
unauthenticated fetch of `bootstrap.sh` returns 404. Authenticate once, then a
single command does everything.

### One command, via `gh`

```bash
gh auth login   # once per machine
gh api repos/TowardInfinity/dotfiles/contents/bootstrap.sh \
  -H "Accept: application/vnd.github.raw" | bash -s -- --deps
```

`gh` supplies the auth, so this works on a private repo. It clones the repo,
installs the packages the configs expect, and symlinks everything.

### One command, via SSH key

If the machine already has an SSH key on your GitHub account
(check with `ssh -T git@github.com`):

```bash
git clone git@github.com:TowardInfinity/dotfiles.git ~/Codes/dotfiles \
  && ~/Codes/dotfiles/bootstrap.sh --deps
```

### Options

| Flag | Effect |
|---|---|
| *(none)* | clone or update, then symlink |
| `--deps` | also install packages — brew on macOS, apt + the Neovim tarball on Ubuntu |
| `--dry` | print what would happen, change nothing |
| `--copy` | keep no repo: fetch a tarball, copy files into place, discard it |

### Why there's a clone at all

The configs are **symlinks into the repo**, so `~/.config/nvim/lua/options.lua`
and the tracked file are the same file. Edit either path, commit from the repo,
`git pull` to update. The whole `.git` costs 352 KB.

`--copy` skips it — real files, nothing left behind. Worth it for a container
or a box you'll destroy tomorrow. Not worth it for a machine you work on: the
files stop being tracked, local edits are invisible to git, and updating means
re-running bootstrap instead of `git pull`.

```bash
# throwaway machine, no repo kept
gh api repos/TowardInfinity/dotfiles/contents/bootstrap.sh \
  -H "Accept: application/vnd.github.raw" | bash -s -- --copy
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

### Prerequisites

`--deps` installs these for you; listed here for reference.

macOS:

```bash
brew install neovim tmux fzf zoxide eza bat lazygit ripgrep
brew install --cask ghostty
```

Linux (Ubuntu):

```bash
sudo apt install -y tmux zsh git curl
# neovim: use the tarball or a PPA — apt's is usually too old for this config
```

Both need Oh My Zsh:

```bash
sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)"
```

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
get zero parsers. `lazy-lock.json` records the branch too, but only
`:Lazy restore` honours it — `:Lazy sync` follows the remote default.

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
