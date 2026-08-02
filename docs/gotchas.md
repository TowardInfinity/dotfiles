# Gotchas

Things that cost real time to work out. Each one is a decision, not an
accident — changing it back will reintroduce the bug.

## Why the two tmux prefixes differ

Mac is `Ctrl a`, servers keep the stock `Ctrl b`. When you SSH into a box
and run tmux there, you have two tmux layers stacked. Different prefixes
mean each keystroke lands unambiguously — no toggle mode, no double-tap.
The status bar colour tells you which layer you are in.

*Source: macos/tmux/tmux.conf · linux/tmux/tmux.conf*

## Don't add xclip to the servers

Copying inside tmux on a server lands on your Mac's clipboard already, via
OSC 52, through both tmux layers and the SSH pipe. That is why the Linux
copy bindings don't pipe to a local clipboard tool — there isn't one, and
there doesn't need to be.

*Source: linux/tmux/tmux.conf*

## The `%%` in the tmux clock is not a typo

tmux runs `status-right` through strftime *before* executing any `#()`
shell job inside it. So a plain `date +%H:%M` gets rewritten to
`date +14:29` first, and the shell just echoes that back — silently
showing UTC while looking like it worked. Escaping as `%%H:%%M` passes a
literal `%` through to the shell.

The servers deliberately stay on UTC. Changing the system timezone would
shift cron and every log line, so the bar renders IST for wall-clock
parity and keeps raw UTC beside it.

*Source: linux/tmux/tmux.conf*

## nvim-treesitter must stay on `master`

Upstream made `main` — a rewrite — the default branch, but the pinned
NvChad v2.5 uses the old API that exists only on `master`. Without an
explicit pin, a fresh clone silently gets `main` and every parser fails
with a module-not-found error.

*Source: common/nvim/lua/plugins/init.lua*

## `lazy-lock.json` is a snapshot, not a pin

It records a branch per plugin, but lazy only ever checks out *commits* —
never branches — so `:Lazy restore` does not honour that field. An
unpinned plugin follows whatever the remote's default branch was on the
day that machine cloned it. Two boxes set up months apart land on
different branches with nobody running a wrong command, and because lazy
rewrites the lockfile from disk afterwards, the drift gets recorded as
truth.

Only `branch =` in the spec enforces anything, which is why
nvim-treesitter, base46 and ui all carry one.

*Source: common/nvim/lua/plugins/init.lua*

## The Obsidian vault is exempt from format-on-save

Format-on-save is on everywhere else. Prettier strips the trailing
double-space hard breaks that the prose-mode autocmds treat as
meaningful, and reflows callouts, checklists and wiki-links that Obsidian
owns.

Matched by vault directory name rather than an absolute path, because
this file is shared verbatim with the Linux servers.

*Source: common/nvim/lua/configs/conform.lua*

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

## Jupyter comes from `uv tool`, never brew or apt

Those builds ship their own externally-managed Python, so the single
kernel they register points inside the package's own site-packages. You
cannot install project dependencies into it, and an upgrade replaces the
lot — which is usually what ends with conda being installed to work
around it.

Instead: one JupyterLab on PATH, and each project registers its own venv
as a kernel with `uv run python -m ipykernel install --user --name <proj>`.

*Source: bootstrap.sh*
