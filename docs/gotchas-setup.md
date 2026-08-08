---
title: Setup Gotchas
group: Maintain
order: 50
summary: Install and terminal gotchas from setup
---

# Setup Gotchas

## The servers are arm64, and getting that wrong fails silently

The Ubuntu boxes are Oracle Ampere — `aarch64`. When the installer
hardcoded x86_64, the download still succeeded (that asset always
exists), tar succeeded, the symlink was made, and nothing failed until
the first `nvim` died with *Exec format error*. A check for the
executable bit can't catch it either. Architecture now comes from
`uname -m`.

*Source: bootstrap.sh*

## Never run the Oh My Zsh installer directly

It moves any existing `~/.zshrc` aside and drops in its own template.
Since yours is a symlink into the repo, that silently replaces your
config with a stock file. The bootstrap passes `KEEP_ZSHRC=yes` for
exactly this reason — if you ever run it by hand, pass it too.

*Source: bootstrap.sh*

## Garbage keystrokes after a dropped SSH session

The server's tmux enables mouse mode. If the link dies uncleanly it never
sends the disable sequence, and your local terminal starts turning mouse
movement into fake keystrokes — `zsh: command not found: 35`. The macOS
`.zshrc` clears mouse mode before every prompt to make that unreachable;
`fixterm` is the manual rescue.

*Source: macos/zsh/.zshrc*

## `pnpm add -g` can succeed and still install somewhere nothing can run

pnpm has its own configured global bin directory, separate from wherever
`pnpm` itself lives, and `pnpm add -g` does not check whether that directory
is on `PATH` before it installs there. A fresh pnpm setup that never ran
`pnpm setup` (or added the directory to `.zshrc` by hand) will report every
global install as successful while nothing it puts there actually runs.
`dots doctor`'s "pnpm global bin" check catches it by asking `pnpm bin -g`
the same question — it refuses to print a directory it can't reach, and its
exit code is the check. Fix with `pnpm setup`, then restart the shell.

*Source: cmd/dots/packagehealth.go*

## Jupyter comes from `uv tool`, never brew or apt

Those builds ship their own externally-managed Python, so the single
kernel they register points inside the package's own site-packages. You
cannot install project dependencies into it, and an upgrade replaces the
lot — which is usually what ends with conda being installed to work
around it.

Instead: one JupyterLab on PATH, and each project registers its own venv
as a kernel with `uv run python -m ipykernel install --user --name <proj>`.

*Source: bootstrap.sh*
