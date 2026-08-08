---
title: Delegation
group: Models
order: 40
summary: Why subagents cost more not less, picking Terra vs Luna, and the two-tool split
---

# Delegation

## Subagents cost more, not less

Delegating to a subagent does **not** save tokens — a subagent costs *more*
in total, because it starts cold and reloads `CLAUDE.md`, git status, and
whatever files it needs from scratch. What it buys is a smaller *main*
context, not a smaller total spend.

The default is inline: do the work in the main session. Delegate only when
the work produces verbose, disposable output — test runs, log triage, a
broad multi-file search, a dependency scan — where the raw output can stay in
the subagent and only a summary comes back. That's the case that genuinely
wins, because the alternative is that verbose output sitting in the main
session's context for every subsequent turn.

Quick targeted edits, single-file changes, or anything where the cold start
costs more than the task itself: don't delegate those. `Explore` and `Plan`
skip `CLAUDE.md` and git status, so they're the cheap agent types — prefer
them for search and design fan-out over a general-purpose agent.

Keep orchestration, synthesis, and user-facing decisions in the main session
— that's the judgment work a cold-started subagent is worst positioned to do
well, since it never had the conversation that led here.

## Luna is not "the cheap Terra"

Terra is Codex's default; Luna is its lighter tier — but the split between
them in the "Two tools, two quotas" table below is by *task shape*, not by a
sliding cost dial. Test writing, log/CI triage, and mechanical transforms go
to Luna because that's what it's suited for, not because it's Terra at a
discount. Bulk implementation from a written spec goes to Terra because that
task needs the fuller model, not because it happens to be the default.

### Why not "Luna at high effort" instead

Claude's effort levels (`/effort low|medium|high|xhigh|max`) are a real dial
on one model — they change how hard *that* model thinks, not which model it
is. There's no equivalent move on the Codex side that turns Luna into Terra
by turning a knob; picking Terra vs. Luna is picking the tool for the task
shape, the same way picking Sonnet vs. Haiku vs. Opus on the Claude side is,
not a substitute for it.

## Two tools, two quotas

ChatGPT Plus ($20) runs Codex CLI on an allowance completely separate from
Claude Pro. Draining one while the other idles is the most common waste —
route deliberately:

| Work | Tool |
|---|---|
| Architecture, planning, "what should I build" | Claude Code, `/model opusplan` |
| Multi-file refactors, cross-cutting changes, review | Claude Code (Sonnet) |
| Bulk implementation from a written spec | Codex (Terra) |
| Test writing, log/CI triage, mechanical transforms | Codex (Luna) |
| Anything after one tool caps out | the other one |

Handing a task across tools is the one place a cold start is unavoidable —
Codex has no access to a Claude Code session's context. Writing the plan to a
file first makes that handoff nearly free instead of re-explaining context to
a cold tool from scratch.
