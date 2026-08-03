---
title: Flags
group: Start
order: 20
summary: Install flags and updating an existing box
---

# Flags

| Flag | Effect |
| --- | --- |
| `(none)` | Clone or update, symlink, install the configs' own dependencies |
| `--deps` | Also install packages — brew on macOS, apt plus tarballs on Ubuntu |
| `--dry` | Print what would happen, change nothing |
| `--copy` | Keep no repo — fetch a tarball, copy files into place, discard it |
| `--nvim` | Also restore nvim plugins from `lazy-lock.json` after linking |
| `--ai` | Also install the AI CLIs: claude, codex, opencode |
| `DOTFILES_DIR=` | Env var — clone somewhere other than the default |

## Updating an existing box

```sh
cd ~/Codes/dotfiles && git pull
tmux source-file ~/.config/tmux/tmux.conf   # NOT kill-server
```

The servers run long-lived sessions. `source-file` applies changes in place
and leaves them alone.

> **Oh My Zsh and TPM install on every run**
>
> They are not optional packages. `.zshrc` sources Oh My Zsh and
> `tmux.conf` loads TPM plugins, so linking those configs without them
> gives you a shell that errors on every prompt. `--deps` is for things the
> configs *call*; this is for what they *are*.
