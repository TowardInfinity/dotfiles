---
title: Cost
group: Models
order: 30
summary: Relative cost per token, what actually burns it, and how to check usage
---

# Cost

## Relative cost per token

| Claude | | Codex | |
|---|---|---|---|
| Fable 5 | 5× | Sol | 5× |
| Opus 5 | 2.5× | Terra | 2.5× |
| Sonnet 5 | 1× | Luna | 1× |
| Haiku 4.5 | 0.5× | | |

Codex 5-hour message allowance on Plus: Sol 15–90 · Terra 20–110 · Luna 50–280.

**Never use Fable for engineering.** It is never a default, and as of
`availableModels` (see the Enforcement section of `dots models`) it cannot be
selected at all without a settings.json edit. On this account it
silently became the single largest line item — 4,256 main-session turns, ~46%
of all spend — while doing work Sonnet would have done at a fifth the price.

The honest caveat: Fable 5 does lead **SWE-Bench Pro at 80%** (vs Sol's 64.6%),
so it is genuinely the strongest repo-level coder in the table. It is still the
wrong choice on Pro — at 5× Sonnet and 2× Opus out of one shared bucket, it buys
a few points of pass rate for most of the month's allowance. It also *trails*
Opus 5 on general coding (Frontier-Bench 33.7% vs 43.3%), so it is not a
straight upgrade.

Escalation to Opus is worth it when the task is genuinely hard reasoning rather
than execution — Opus 5 scores 30.2% on ARC-AGI-3 against Sol's 7.8%, which is a
real gap, not a rounding difference. That is what `opusplan` is for: pay for the
thinking, not the typing.

## What actually costs money

Measured on this account across 101 sessions and 19,180 turns:

- **Context dominates, not output.** 4.4 *billion* cache-read tokens against
  14M output tokens. Every turn re-reads the whole window, so a session sitting
  at 800k tokens pays ~80k input-equivalent *per turn* before doing any work.
- Per-turn context measured p50 ≈ 300k, p90 ≈ 800k, max 982k. Only 6 of 101
  sessions ever compacted.

So the highest-leverage habits are about context, not model choice:

- **`/clear` between unrelated tasks.** It is free. `/compact` is not — it reads
  the whole conversation to summarise it.
- Auto-compact is pinned to a 250k window via `CLAUDE_CODE_AUTO_COMPACT_WINDOW`
  in `settings.json`. Raise it toward 400000 if you lose useful context.
- Don't re-read files already in context; don't dump a whole file when a range
  will do.

Reasoning effort is the other real lever. Claude: `/effort low|medium|high|xhigh|max`.
Codex: `model_reasoning_effort`, where `high`/`xhigh` run several times the
tokens of `medium` for the same prompt.

`MAX_THINKING_TOKENS` does nothing on Opus 5 / Sonnet 5 / Fable 5 — they use
adaptive reasoning, so effort level is the only control.

**Do not set `alwaysThinkingEnabled: false` to save tokens.** Per the binary it
is a kill switch, not a dial — *"When false, thinking is disabled"* — and it
overrides `effortLevel` entirely. On adaptive-reasoning models, leaving it
absent lets the model spend reasoning only where a turn needs it, which is
already the cheap behaviour. Turn the effort dial down instead if you want less.

## Plugins do not cost what you would expect

Disabling Codex plugins does **not** save tokens. Measured with
`codex debug prompt-input`: the skills block is budget-capped at ~22k chars
(~5.5k tokens) per turn. Disabling 9 skills took the list from 71 to 62 entries
but shrank the block by only 43 chars — the survivors' truncated descriptions
simply expanded to fill the freed budget.

Prune plugins for relevance, so the budget describes skills you actually use.
Do not expect a quota saving.

Claude Code's MCP overhead is genuinely small: tool search defers full schemas,
so a dozen connected servers cost low hundreds of tokens at startup. Check with
`/context` rather than guessing.

## Checking usage

| What | How |
|---|---|
| Claude quota | `/usage` |
| Claude context breakdown | `/context` |
| Claude spend by model, from transcripts | `bin/claude-usage.py` |
| Codex config as loaded | `codex doctor` |
| Codex per-turn base prompt | `codex debug prompt-input "hi"` |

`bin/claude-usage.py` parses `~/.claude/projects/*.jsonl` and reports turns,
tokens and a list-price cost proxy per model, plus context-size percentiles.

### When the numbers actually mean something

**The trigger is at least 10 sessions started after the policy timestamp
(`2026-08-05T01:10`) — not a date on the calendar.** Elapsed days produce no
signal by themselves. The first attempt to measure this ran seven days out and
was worthless: not one new session had been started in that time, so the whole
window was a single pre-policy session still running. The script now refuses to
validate below the threshold and says why.

Sessions that began before the policy landed are reported but **excluded from
validation** — they cannot say anything about a default they never saw.

Read both views; they answer different questions:

| View | Question it answers |
|---|---|
| **turn-weighted** (top table) | what the allowance actually paid for |
| **sessions started** | whether the configured default is taking effect |

They diverge sharply, and that is not a bug. `"model": "sonnet"` sets what a
*new* session opens on and cannot retarget one already running, so a single
long escalated session shows as ~98% Opus by turns while every fresh session
starts on Sonnet. Judging the default by turns alone reads that as a failure.

Success: **Fable at zero**, Opus under ~15% of *session starts*, p90 context
down from ~800k toward ~300k. Note the context percentiles lag — they include
old turns until the pre-policy sessions age out of the window.

The `~$ proxy` column is API list price standing in for quota weight. It is
directional only; the turn counts and context sizes are exact.
