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
| `--light` | Minimal install for a low-memory box — shell and tmux, no editor toolchain |
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

## `--light`, for small boxes

`v1` and `v2` have 956 MB of RAM, under 500 MB of it free, and **no swap**.
Disk is not the constraint — they have 40 GB spare. Memory is, and the full
`--deps` set is actively harmful there:

| | why it hurts |
|---|---|
| `go build` | peaks around 1 GB — invokes the OOM killer, which may take sshd rather than the compiler |
| Mason LSPs | jdtls alone asks for roughly a gigabyte of heap |
| a JDK | installs fine, then cannot run |
| node / pnpm / uv | toolchain for work that box will never do |

```sh
sh -c "$(curl -fsSL https://toin.in/install)" -- --light
```

That gets zsh, tmux, git, curl, ripgrep and jq — all from apt, all small —
plus Oh My Zsh and the two zsh plugins the config's `plugins=(...)` lists.
The configs degrade on their own: the Linux `.zshrc` guards fnm, uv and glow
behind existence checks, and its `cat` wrapper falls back to real `cat` when
glow is absent.

`dots` still installs, as a prebuilt release binary. The resolver refuses to
build from source when a machine has under 900 MB of headroom including swap,
because on such a box a build is not a slow success but an OOM kill. Override
with `DOTS_ALLOW_LOW_MEM_BUILD=1` if you disagree.

`--light` and `--deps` together is not an error — `--light` wins, since it is
the constrained one.
