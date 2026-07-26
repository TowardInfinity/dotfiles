# dotfiles

Terminal setup for macOS — Ghostty, zsh, tmux, Neovim. One `git clone` plus
`./install.sh` gets a new Mac to the same place.

```
nvim/          NvChad-based Neovim config (Java/Spring, TS, Python, Go)
tmux/          tmux.conf (Mac) + vm-tmux.conf (deployed to the a1 VM)
ghostty/       terminal config
zsh/.zshrc     shell
install.sh     symlinks everything into place
```

## Install

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

```bash
brew install neovim tmux fzf zoxide eza bat lazygit ripgrep
brew install --cask ghostty
sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)"
```

Also expects a Nerd Font — the configs assume **JetBrainsMono Nerd Font** for
the status-bar glyphs.

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

## Deploying tmux to the VM

`tmux/vm-tmux.conf` is the source of truth; the VM copy is downstream. It runs
long-lived sessions, so never `kill-server` — source in place:

```bash
scp tmux/vm-tmux.conf a1:/tmp/new.conf
# validate on an isolated socket first, live sessions untouched
ssh a1 'tmux -L probe -f /tmp/new.conf new-session -d -s p \
        && tmux -L probe show-messages | grep -i error; tmux -L probe kill-server'
ssh a1 'cp /tmp/new.conf ~/.tmux.conf && tmux source-file ~/.tmux.conf'
```

It targets **tmux 3.2a** (older than the Mac's 3.7b) and assumes no `fzf`, so it
avoids `allow-passthrough` and uses `choose-tree` pickers.

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

`~/.ssh/config` is deliberately absent — it holds VM addresses. Machine-local
state (tmux plugins and resurrect snapshots, nvim's `lazy`/`mason` downloads)
is gitignored; `install.sh` and the package managers regenerate all of it.
`nvim/lazy-lock.json` *is* tracked, so plugin versions are reproducible.
