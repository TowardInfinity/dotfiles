---
title: Packages
group: Reference
order: 40
summary: What global package installs are tracked, and how
---

# Packages

`docs/tools.md` covers what `--deps` installs *directly*. This page covers
the layer above it: what gets installed *through* those tools afterward —
`pnpm add -g`, `uv tool install`, and so on. Nothing here is installed by
`dots install`; it's read-only visibility, the same as the Config group in
`dots doctor` — it reports drift, it never fixes it.

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

## Not tracked here, on purpose

Personal installs that nothing in the repo calls for — `kaggle`,
`open-webui`, or `brew leaves` beyond `docs/tools.md`'s table — are a
deliberate choice to install by hand, the same boundary `tools.md` already
draws around the AI CLIs before `--ai` existed: if the configs don't
reference it, tracking it here would just be surveillance of your own
machine, not drift detection.

Two things considered and dropped for the same reason:

- **A "codex install path" check** (warn if codex resolved via the npm
  fallback instead of pnpm) — dropped once a live audit showed codex on this
  Mac actually runs via the standalone installer's own symlink
  (`~/.local/bin/codex -> ~/.codex/packages/standalone/current/bin/codex`),
  not through either package manager's global `node_modules`. The fallback
  logic in `install_ai_clis` is real, but this machine never exercised it —
  checking for it would assert something the tools on hand can't actually
  confirm one way or the other.
- **`brew leaves` reconciliation** against `docs/tools.md`'s table, and a
  general `npm list -g` / `go install` inventory — both would be one more
  function shaped like `packageChecks()` in `cmd/dots/packagehealth.go`, not
  a design change, so there's nothing stopping either from being added later
  if a specific drift shows up worth catching.
