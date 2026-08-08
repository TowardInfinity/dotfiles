---
title: Cost
group: Models
order: 30
summary: Relative cost per token, what actually burns it, and what plugins add
---

# Cost

## Relative cost per token

| Model | Relative cost |
|---|---|
| Fable | 5x |
| Opus | 2.5x |
| Sonnet | 1x |
| Haiku | 0.5x |

Sonnet is the baseline the rest of this repo's policy is written against —
`dots models`'s Enforcement section covers the setting that keeps Fable out
of reach entirely rather than just discouraged in prose.

## What actually costs money

Every turn re-reads the whole context window, so a long session pays for its
full history on every single turn, not once. A 800k-token session pays
roughly 80k input-equivalent tokens *per turn* before doing any new work.
Cache reads, not output tokens, dominate spend on a long-running session —
which is why trimming what stays in context matters more than being terse in
any one reply.

Two concrete levers, both already set:

- `/clear` between unrelated tasks. It's free; `/compact` is not — compacting
  still has to read and summarize everything being dropped.
- Auto-compact is pinned to a 250k-token window
  (`CLAUDE_CODE_AUTO_COMPACT_WINDOW` in `settings.json`) rather than left at
  whatever the default is, so a session compacts before it grows large enough
  for that per-turn re-read to get expensive.

Not re-reading files already in context, and pulling a line range instead of
a whole file when a range will do, are the same lever applied by hand rather
than by a setting.

## Plugins do not cost what you would expect

An enabled plugin or skill adds to context just by being enabled — its
description has to be loaded so Claude can decide whether to reach for it —
whether or not any given session ever invokes it. `settings.json`'s
`skillOverrides` turns off several bundled skills entirely (the
Cloudflare-specific ones, `wrangler`, `sandbox-sdk`, `web-perf`,
`turnstile-spin`, `agents-sdk`) rather than leaving them installed-but-idle:
an idle skill is not a free skill, it's context paid on every turn for a
capability not being used in this repo. Only `frontend-design` stays enabled
in `enabledPlugins`.
