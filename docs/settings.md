---
title: Settings
group: Maintain
order: 10
summary: Where to edit each config and setting
---

# Settings

Where everything lives, and which file to open when you want to change
something. The live configs are **symlinks into this repo**, so editing
`~/.zshrc` and editing `macos/zsh/.zshrc` are the same act — there is no
copy step and no sync step. Commit from the repo afterwards.

## Where each thing lives

| You want to change | Edit | Applies to |
|---|---|---|
| Shell aliases, PATH, prompt | `macos/zsh/.zshrc` / `linux/zsh/.zshrc` | that OS |
| tmux keys, status bar | `macos/tmux/tmux.conf` / `linux/tmux/tmux.conf` | that OS |
| Terminal colours, font, opacity | `macos/ghostty/config` | mac |
| Neovim keymaps | `common/nvim/lua/mappings.lua` | both |
| Neovim plugins | `common/nvim/lua/plugins/init.lua` | both |
| Theme, statusline style | `common/nvim/lua/chadrc.lua` | both |
| LSP servers | `common/nvim/lua/configs/lspconfig.lua` | both |
| Formatters, format-on-save | `common/nvim/lua/configs/conform.lua` | both |
| Which tools get installed | `bootstrap.sh` | both |
| What gets symlinked where | `install.sh` | both |
| These docs | `docs/*.md` | both |

## Per-machine settings that stay untracked

Anything true of one box only belongs in **`~/.zshrc.local`**. It is sourced
near the end if it exists and is never tracked, which keeps the repo's `.zshrc`
byte-identical on every machine. Both macOS and Linux have this hook. SDKMAN's
initialisation follows it because SDKMAN requires its block to stay last, so a
local setting that SDKMAN itself rewrites must be applied another way.

Use it for: work-specific env vars, a machine's own PATH entries, credentials,
an override of any alias defined above it.

```sh
# ~/.zshrc.local
export AWS_PROFILE=work
alias deploy='./scripts/deploy.sh --env=staging'
```

The same principle applies to Claude Code: each machine's
`~/.claude/CLAUDE.md` stays machine-specific and untracked, and pulls in the
one shared piece with a single `@~/.claude/delegation.md` line.

## Environment variables this setup reads

| Variable | Effect |
|---|---|
| `DOTFILES_DIR` | Clone or look for the repo somewhere other than the default |
| `CONDA_HOME` | Where to find conda, if you keep it somewhere unusual |
| `EDITOR` | Used by `dots edit`, `tmuxconfig`, and git |
| `ZSH_CUSTOM` | Oh My Zsh custom plugin directory |

## Things that are deliberate, not accidental

**The two tmux prefixes differ.** macOS is `Ctrl a`, the servers keep the
stock `Ctrl b`. Do not "fix" this — see `dots gotchas`.

**The shells are not shared.** Almost nothing overlaps between macOS and
Linux (Homebrew vs apt, `launchctl` vs systemd, `pbcopy` vs OSC 52), so
there are two files rather than one full of OS conditionals.

**Neovim is shared verbatim.** No Homebrew paths, no absolute home
directories, nothing macOS-only. Keep it that way — anything you add there
runs unchanged on the servers.

**`lazy-lock.json` is tracked on purpose.** It pins plugin *commits*. It does
not pin branches, however much it looks like it does; that needs `branch =`
in the plugin spec.

## Changing the theme

Everything shares one Tokyo Night palette on purpose — Ghostty, tmux and
Neovim all match. Changing one means changing all three:

- `common/nvim/lua/chadrc.lua` — `M.base46.theme`
- `macos/ghostty/config` — `background`, `foreground`
- `macos/tmux/tmux.conf` and `linux/tmux/tmux.conf` — the hex values in
  `status-left`, `status-right` and the window formats

Inside Neovim, `<Space>th` opens the theme picker to preview before
committing to one.
