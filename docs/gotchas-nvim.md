---
title: Neovim Gotchas
group: Maintain
order: 40
summary: Neovim plugin pinning and format-on-save gotchas
---

# Neovim Gotchas

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
