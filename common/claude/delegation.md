# Model and delegation policy

Shared across every machine — imported by each box's `~/.claude/CLAUDE.md` via
`@~/.claude/delegation.md`. The rest of that file stays machine-specific (the
Mac's toolchain preferences, a server's cost/security guardrails); only this
policy is common, so edit it here and `git pull` everywhere else.

This file is loaded into **every** session's context. Keep it short.

## Budget

Claude Pro **$20/mo**. The 5-hour and weekly windows are **shared across all
models and shared with claude.ai chat** — switching model with `/model` does not
restore access once capped. Spend accordingly.

Relative cost per token: **Fable 5× · Opus 2.5× · Sonnet 1× · Haiku 0.5×**.

## Which model

- **Sonnet 5 is the default** and set in `settings.json`. Leave it there.
- **Opus** only for genuinely hard design work, via `/model opusplan` — Opus
  while planning, automatic switch to Sonnet for execution. Plain `/model opus`
  only when a whole session is architecture.
- **Haiku** for mechanical work; it is the configured fallback.
- **Never Fable for engineering.** It is 5× Sonnet and is never a default —
  it only runs if something selects it. It once silently became the single
  largest line item on this account (4,256 main-session turns). Creative and
  narrative work only.
- **Never enable agent teams** (`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS`) — ~7×
  a normal session.

Effort is the reasoning lever (`/effort low|medium|high|xhigh|max`), persisted at
`high`. `MAX_THINKING_TOKENS` does nothing on Opus 5 / Sonnet 5 — they use
adaptive reasoning.

**The advisor is set to Opus** (`advisorModel` in `settings.json`), not Fable —
Claude decides when to call it, unattended, so it's a worse fit for the
never-Fable rule than the main model, which needs a deliberate `/model` switch.
`CLAUDE_CODE_DISABLE_ADVISOR_TOOL=1` kills it outright if needed.

## Context is the real cost

Every turn re-reads the whole context window, so a 800k-token session pays ~80k
input-equivalent *per turn* before doing any work. Cache reads, not output,
dominate spend. Therefore:

- `/clear` between unrelated tasks. It is free; `/compact` is not.
- Auto-compact is pinned to a 250k window in `settings.json`.
- Don't re-read files already in context, and don't dump a whole file when a
  range will do.

## When to delegate

**Default is inline.** Do the work in the main session.

Delegating does **not** save tokens — a subagent costs *more* in total, because
it starts cold and reloads CLAUDE.md, git status, and the files it needs. What it
buys is a smaller main context.

- **Delegate** when the work produces verbose, disposable output: test runs, log
  triage, broad multi-file search, dependency scans. The raw output stays in the
  subagent and only a summary returns. This is the case that genuinely wins.
- **Do not delegate** quick targeted edits, single-file changes, or anything
  where the cold start costs more than the task itself.
- `Explore` and `Plan` skip CLAUDE.md and git status, so they are the cheap
  agent types — prefer them for search and design fan-out.
- Keep orchestration, synthesis, and user-facing decisions on the main session.

Pass an explicit `model` on every Agent call: **Sonnet** for work needing
judgment, **Haiku** for mechanical lookups and rote transforms (batch these into
one call rather than spawning several). If the tier is ambiguous, round up — a
redo costs more than starting at the right tier.

**Do not set `CLAUDE_CODE_SUBAGENT_MODEL`.** It is a hard override that beats the
per-call `model` parameter and any agent's `model:` frontmatter, collapsing the
tiering above onto one model. With the main session on Sonnet, subagents inherit
Sonnet anyway.

## Two tools, two quotas

ChatGPT Plus $20 runs Codex CLI on a separate allowance. Draining one while the
other idles is the most common waste — route deliberately:

| Work | Tool |
|---|---|
| Architecture, planning, "what should I build" | Claude Code, `/model opusplan` |
| Multi-file refactors, cross-cutting changes, review | Claude Code (Sonnet) |
| Bulk implementation from a written spec | Codex (Terra) |
| Test writing, log/CI triage, mechanical transforms | Codex (Luna) |
| Anything after one tool caps out | the other one |

When handing a task across, **write the plan to a file first**. Re-explaining
context to a cold tool is the expensive part; a spec file makes it nearly free.

Full reference: `dots models`.
