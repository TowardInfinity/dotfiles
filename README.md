# dotfiles

Terminal setup for macOS and Linux — Ghostty, zsh, tmux, Neovim. One
`git clone` plus `./install.sh` gets a new Mac, or a fresh server, to the same
place. Currently on: this MacBook, and the `a1` / `v1` / `v2` Ubuntu boxes.

```
common/nvim/       NvChad-based Neovim config — shared by macOS and Linux
macos/
  zsh/.zshrc       Homebrew, conda, iTerm2, launchctl aliases
  tmux/tmux.conf   prefix Ctrl-a, pbcopy clipboard
  ghostty/config   terminal (macOS only — servers are headless)
linux/
  zsh/.zshrc       apt, ss, TERM fallback, glow-backed cat
  tmux/tmux.conf   prefix Ctrl-b, OSC 52 clipboard, targets tmux 3.2a
install.sh         detects the OS and links the right set
```

`install.sh` reads `uname -s` and links `common/` plus either `macos/` or
`linux/`. Neovim is genuinely OS-agnostic — no Homebrew paths, no `pbcopy` —
so it lives in `common/` and never drifts between machines.

The shells are *not* shared. Almost nothing overlaps between them (Homebrew vs
apt, `launchctl` vs systemd, `pbcopy` vs OSC 52), so two files beat one file
full of `if [[ $OSTYPE ]]`.

## Install

Same two commands on either OS:

```bash
git clone git@github.com:TowardInfinity/dotfiles.git ~/Codes/Projects/dotfiles
cd ~/Codes/Projects/dotfiles
./install.sh --dry      # see what it would do
./install.sh            # do it
```

Existing files are moved to `<path>.backup.<timestamp>` rather than
overwritten, and re-running is safe — links already pointing at the repo are
left alone.

### Prerequisites

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
