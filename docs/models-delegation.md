---
title: Delegation
group: Models
order: 40
summary: Why subagents cost more not less, picking Terra vs Luna, and the two-tool split
---

# Delegation

## Subagents cost more, not less

A subagent does **not** save tokens. It starts cold and reloads CLAUDE.md /
AGENTS.md, git status, and the files it needs, so total spend goes *up*. What it
buys is a smaller main context and wall-clock parallelism.

Delegate only when the work produces verbose, disposable output — test runs, log
triage, broad search, dependency scans — so the raw output stays in the subagent
and only a summary returns. Do not delegate quick targeted edits.

### Claude Code subagents

There is **no settings.json key** for the subagent model. Resolution order,
verified in the v2.1.221 binary:

1. `CLAUDE_CODE_SUBAGENT_MODEL` — checked first, and it returns before the
   per-call value is ever read. **Never set it.** It is a hard override that
   flattens every tier below onto one model. (`"inherit"` is the one safe value,
   and it is the same as leaving it unset.)
2. The `model` parameter on the Agent call, or an agent's `model:` frontmatter.
3. Otherwise **inherit the main-loop model.**

Because of (3), the subagent tier is set by whatever the main session is running
— there is nothing global to configure, only something not to break. This is
also why the Opus→Sonnet change mattered twice over: **`Explore` agents inherit
the parent model rather than defaulting to Haiku**, so every unnoticed fan-out
was previously running Opus.

Pass `model` explicitly on every Agent call: **Sonnet** where judgment is
needed, **Haiku** for mechanical lookups and rote transforms (batch those into
one call rather than spawning several). `Explore` and `Plan` additionally skip
CLAUDE.md and git status, so they are the cheap agent *types* regardless of
model.

Never enable agent teams (`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS`) — roughly 7×
a normal session.

Codex is capped at 1 concurrent subagent, routed to **Terra at `low`** — see
below for why not Luna. Never enable Codex's ultra / parallel-agent mode either:
~3× the cost for ~3 benchmark points, and it is widely reported to get stuck in
review loops.

## Luna is not "the cheap Terra"

Worth knowing before reaching for it. Luna is within ~3 points of Terra on
everything that looks like agent work:

| | Sol | Terra | Luna |
|---|---|---|---|
| Agents' Last Exam | 53.6 | 50.4 | 50.3 |
| Terminal-Bench 2.1 | 88.8% | 87.4% | 84.7% |
| Coding Agent Index | 80 | 77.4 | 74.6 |
| **MRCR long-context recall** | **91.5%** | **89.6%** | **41.3%** |

One cliff, and it is enormous. A 1M context window does not mean 1M usable
tokens. The rule every independent write-up converges on: **do not hand Luna a
whole spec, log file, or codebase.**

That is why subagents run Terra. Delegated work here is read-heavy by
definition — logs, search results, test output — which is exactly the axis Luna
fails on, and a wrong summary costs a full re-run on top of the wasted call.
Take the saving from **effort** instead: `low` cuts planning and checking tokens
without touching retrieval fidelity. Raise effort only after a run comes back
with incomplete or missing fields.

Luna is still the right pick for bounded, short-context, mechanically-specified
work with a clear success test — a rename with a known pattern, an extraction
checked by an existing test.

### Why not "Luna at high effort" instead

Tempting: let Terra plan, let a cheap model at high effort do the coding, and
lean on the fact that subagents are throwaway. It doesn't hold up.

**Effort and recall are different axes.** Raising effort does not repair
retrieval. The research finds long-context retrieval failures "stem from deeper
architectural limitations rather than insufficient reasoning capacity" — and the
*lost-in-thought* result is worse than neutral: extra reasoning tokens actively
**degrade** long-context retrieval, because attention shifts toward the model's
own reasoning trace and away from the source documents. High effort on a
low-recall model buys tokens, not accuracy.

**"Throwaway" does not mean "small context" — it means the opposite.** From the
live Codex system prompt:

> Full-history forks (`fork_turns` omitted or `"all"`) inherit the parent model
> and reasoning effort and do not accept overrides.

So running a subagent on a *different* model requires `fork_turns="none"` or an
integer. A cheap-tier subagent is by construction one that inherits almost
nothing and must therefore go read every file and log itself — raw, unsummarised
material piling up fast. That is exactly the axis it is weakest on.

**And the cheap tier is not cheaper on coding.** CodeRabbit, 100 long-horizon
coding tasks:

| | Pass rate | Avg output tokens |
|---|---|---|
| Sol | 63.7% | 20,968 |
| Terra | 40.7% | 55,594 |

The cheaper tier used **2.65× more tokens and passed 23 points less** — $2.05 vs
$0.99 per *solved* task. Flailing can cost more than the per-token saving.

But do not over-read that. It measures Sol vs Terra (a 2× price gap) and says
nothing directly about Luna, whose gap to Terra is another 2.5×. Run the
break-even honestly: at equal token burn, **Luna only has to pass 16% against
Terra's 40.7% to tie on token cost.** That is a lot of headroom, and on pure API
economics the cheap tier is hard to beat.

The reason it still isn't the default here is that **this account is not billed
in tokens.** The Plus meter is *requests per 5-hour window* — Terra 20–110,
Luna 50–280. So Luna wins only if it needs fewer than ~2.5× Terra's *turns*, and
turn count is exactly what degrades when a model retries. Broader analysis puts
a 5× retry multiplier at $5.73 → $28.65 per solved task, and concludes loop
design moves cost more than model choice does.

The deciding factor is therefore not cost at all — it is that a recall miss
produces *plausible wrong code*, silently, and the cost of that lands on your
review time rather than your quota.

One last trap: the orchestrator is told *"all agents in the team are equally
intelligent and capable."* Downgrade the subagent and the parent sizes handoffs
for a peer it no longer has. It also only sees the final payload, not the work —
so "the main model will verify it" means re-reading the same material, which is
the expensive part you were trying to avoid.

**The sound version of the instinct:** split by *task shape*, not by making
coding the cheap tier. Coding is the worst candidate — it is long-horizon and
read-heavy. Bounded single-file transforms with a test as the oracle are the
good candidate, and that is what Luna is already reserved for.

## Two tools, two quotas

Draining one while the other idles is the most common waste.

| Work | Tool |
|---|---|
| Architecture, planning, "what should I build" | Claude Code, `/model opusplan` |
| Multi-file refactors, cross-cutting changes, review | Claude Code (Sonnet) |
| Bulk implementation from a written spec | Codex (Terra) |
| Log/CI triage, reading long output | Codex (Terra — *not* Luna) |
| Mechanical transforms with a known pattern | Codex (Luna) |
| Anything after one tool caps out | the other one |

When handing work across, **write the plan to a file first**. Re-explaining
context to a cold tool is the expensive part; a spec file makes it nearly free.

