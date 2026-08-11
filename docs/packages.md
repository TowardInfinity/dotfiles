---
title: Packages
group: Reference
order: 40
summary: What global package installs are tracked, and how
---

# Packages

`docs/tools.md` covers what `--deps` installs *directly*. This page covers
the layer above it: what gets installed *through* those tools afterward —
`pnpm add -g`, `uv tool install`, and so on. Two different tools answer two
different questions about it:

- **`dots doctor`'s Packages group** — "did the one thing `bootstrap.sh`
  declared stay installed and usable". Narrow, read-only, reports drift and
  never fixes it, same as the Config group.
- **The Packages route in `dots`** — "what's actually here, across
  every manager". A full browsable inventory, grouped by manager, with
  versions and — one keypress away — a per-package upgrade. This one *does*
  act, but only on a single row at a time, and only via the same
  confirm-then-stream overlay every other Manage action uses.

Neither installs anything `dots install`/`--deps` wouldn't already have
installed; the Manage section's upgrade key runs each manager's own upgrade
command on something already present, nothing more.

## What the configs actually declare

| Package | Installed via | Where |
| --- | --- | --- |
| `jupyterlab` | `uv tool install jupyterlab` (`install_uv_tools` in `bootstrap.sh`) | both |
| `claude` | the official installer (`--ai`) | both |
| `codex` | `pnpm add -g @openai/codex`, falling back to `npm install -g` (`install_ai_clis`) | both |
| `opencode` | the official installer (`--ai`) | both |

These are the only global installs anything in the repo asks for. `dots
doctor`'s Packages group checks the ones with a cheap, reliable way to verify
themselves after the fact — see below.

## What `dots doctor` checks

| Check | What it verifies | A `checkBad` row means |
| --- | --- | --- |
| `pnpm global bin` | `pnpm bin -g` resolves and its answer is where pnpm actually installs global packages | pnpm's own configured global bin directory is not reachable — see the gotcha below |
| `uv tool: jupyterlab` | `uv tool list` includes `jupyterlab` | `bootstrap.sh` declared it and it's since gone missing |

Both checks are skipped entirely (not reported at all) on a machine that
never installed `pnpm` or `uv` — a `checkBad` "pnpm global bin" row on a box
that doesn't use pnpm would be noise from a check that doesn't apply, not a
finding.

Each row's own detail text carries its fix — there's no single "run this to
repair the Packages group" command the way there is for Config drift
(`dots install`) or missing base tools (`--deps`), because a PATH problem and
a missing `uv tool install` aren't the same repair.

## The Packages route

Seven backends, each contributing nothing when its manager isn't installed —
the same "silently skip, never error" rule `cmd/dots/services.go` already
follows for launchd/systemd/docker:

| Manager | Versions from | Latest from | Upgrade (`u`) |
| --- | --- | --- | --- |
| brew | `brew list --versions` | `brew outdated --json=v2` | `brew upgrade <name>` |
| pnpm (global) | `pnpm list -g --depth 0 --json` | `pnpm outdated -g --format json` | `pnpm add -g <name>@latest` |
| npm (global) | `npm list -g --depth 0 --json` | `npm outdated -g --json` | `npm update -g <name>` |
| uv tool | `uv tool list` | not offered — no offline query | `uv tool upgrade <name>` |
| pip (`--user`) | `pip3 list --user --format=json` | `pip3 list --user --outdated --format=json` | `pip3 install --user --upgrade <name>` |
| go (`~/go/bin`) | `go version -m <binary>` per file | not offered — needs a network call per binary | not offered — `go install <mod>@latest` re-fetches rather than upgrading in place |
| installer (claude, opencode) | presence only (`have()` check) — no version query | not offered — no offline query | re-runs the same `curl \| bash` install script bootstrap.sh used, which is self-updating |

A blank `LATEST` column means "not knowable offline for this manager", never
a guessed "up to date" — the same principle the doctor checks above already
follow with `checkWarn`. The `u` key only appears in the footer when the
cursor is on a row that actually has an upgrade path; pressing it runs the
command through the same confirm-then-stream overlay every other Manage
action (services, dotfiles updates, machine doctor) already uses.

The installer group's `VERSION` column is blank the same way — presence
only, deliberately. Parsing each tool's own `--version` output doesn't
scale as more tools get added to `installerCLIs`, so this manager tracks
"is it here" and nothing else. The curated list is still just claude and
opencode: fnm looked like an obvious third entry but isn't a real
candidate — `bootstrap.sh` installs it via `brew` on macOS and only falls
back to a web install script on Linux, so this backend (which can't tell
which OS installed a given binary) would show it twice, once correctly
under Brew and once spuriously under Installer, and offer to re-run the
Linux installer over a brew install. A real addition here has to be a
single binary installed the same `curl | bash` way on every OS, with no
other install path anywhere in `bootstrap.sh` — claude and opencode are the
only two that currently qualify.

Groups are shown in a fixed order — pnpm, npm, uv tool, pip, go, installer,
then brew last. Brew typically outnumbers every other manager's packages
combined, so putting it first pushed the shorter, often more interesting
groups below the fold; this way they're what you see without scrolling.

Two keys work on top of that grouping without changing it:

- **`s`** toggles the secondary sort within each group — outdated-first
  (default) or alphabetical by name. The group order above never moves; only
  what comes within one group does.
- **`m`** cycles a manager filter — All → pnpm → npm → uv tool → pip → go →
  installer → brew → All — narrowing the table to one group at a time
  instead of scrolling past the others to reach it. The breadcrumb names the
  active one (`Packages › Brew`), same as the outer rail names the current
  section.

An **Outdated overview** block sits above the table, capped at five rows with
a "+N more" line past that. It always summarizes every manager regardless of
the `m` filter or the `/` text filter beneath it — it answers "what does the
machine look like," not "what's currently on screen."

**Consolidation advisories**, computed after discovery and shown above the
table, are read-only — a suggested command, never a command run for you:

- npm has global packages **and** pnpm's global bin dir is reachable → "`N`
  package(s) on npm global — pnpm is set up here; `pnpm add -g <name>` moves
  it over."
- pip has user-site packages **and** `uv` is installed → "`N` package(s) on
  pip — uv tool install is preferred here."

No auto-migration, on purpose: moving a package between managers is an
uninstall-then-reinstall, not a version bump, and something like `codex()`'s
shell wrapper in `macos/zsh/.zshrc` cares which install path won. That
decision stays with whoever types the suggested command by hand.

## Not tracked here, on purpose

Personal installs that nothing in the repo calls for still show up in the
Packages route's inventory above — that section is intentionally a full
snapshot, not filtered to what `bootstrap.sh` declared. What's *not* tracked
is narrower, and specific to the doctor checks:

- **`dots doctor`'s Packages group** only ever grows a row for something a
  config in this repo explicitly declared (see the table above) — the same
  boundary `tools.md` draws around the AI CLIs before `--ai` existed. Adding
  a doctor check for `kaggle` or `open-webui` just because they happen to be
  installed would be surveillance of the machine, not drift detection.
- **A "codex install path" check** (warn if codex resolved via the npm
  fallback instead of pnpm) — dropped once a live audit showed codex on this
  Mac actually runs via the standalone installer's own symlink
  (`~/.local/bin/codex -> ~/.codex/packages/standalone/current/bin/codex`),
  not through either package manager's global `node_modules`. The fallback
  logic in `install_ai_clis` is real, but this machine never exercised it —
  checking for it would assert something the tools on hand can't actually
  confirm one way or the other.
- **`brew leaves` reconciliation** against `docs/tools.md`'s table — the
  Packages route's brew rows are already the full `brew list --versions` output,
  so this would just be re-deriving a subset of data already visible there,
  not new information.
- **Bulk "upgrade everything outdated"**, per manager — the Manage section
  is deliberately per-row only; a batch action is a different, higher-risk
  feature, not a v1 requirement.
