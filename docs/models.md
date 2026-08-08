---
title: Models
group: Reference
order: 30
summary: Which model to use where, and what actually burns quota
---

# Models

Two subscriptions, $20 each: **Claude Pro** (Claude Code) and **ChatGPT Plus**
(Codex). Both cap usage on a rolling 5-hour window *and* a weekly window that
stacks on top. Claude's window is shared with claude.ai chat; Codex's is shared
across CLI, IDE and web.

Switching model mid-session does **not** restore access once you are capped —
every model draws on the same allowance. The only thing that helps is spending
less per turn.

## Defaults

| | Default | Escalate with |
|---|---|---|
| Claude Code | Sonnet 5, effort `high` | `/model opusplan` |
| Codex | Terra, effort `medium` | `codex -p sol` |

Both defaults live in the repo and travel with `dots sync`:
`common/claude/settings.json` is symlinked into place, and
`common/codex/config.policy.toml` is *merged* into `~/.codex/config.toml`
between `# >>> dots: managed block` markers — Codex writes its own
`[projects.*]` trust and `[plugins.*]` entries into that file, so it cannot be a
symlink, but everything outside the markers is left untouched.

Edit the policy in the repo, never the managed block: the next install
overwrites it. You should rarely need to override either default.

`/model opusplan` runs Opus while planning and switches to Sonnet for
execution, which is the cheapest way to get Opus-quality design decisions.
`codex -p sol` layers `~/.codex/sol.config.toml` on top of the base config.

## The advisor

`advisor()` sends the whole transcript to a second, stronger model for a review
before you commit to an approach or declare a task done. It is not a per-turn
cost — it only runs when the agent calls it — so pairing a cheap main model
with a dear advisor is the opposite of running the whole session at the dear
tier: occasional 2.5x calls instead of continuous 2.5x.

```json
"advisorModel": "opus"
```

Persisted in `settings.json` next to `model` and `effortLevel` — confirmed in
the installed binary's settings schema, not just in the `/advisor` help text.
`/advisor <model>` writes it there directly (`/advisor off` clears it); editing
the file by hand works identically.

**Pairing rule, enforced server-side:** the advisor must be at least as capable
as the session's main model — equals allowed, downgrades rejected. On Sonnet,
that leaves Opus, Sonnet itself, or Fable as valid picks. Fable is excluded
anyway — the [Which model](#defaults) rule against it applies here too: 5x
Sonnet for a review that fires on every substantive step is exactly the kind of
quiet line item that made Fable the single biggest expense the last time it ran
unsupervised. Opus is the right pick — same tier already reserved for hard
design work, just invoked as a second opinion instead of as the main seat.

## Resumed sessions ignore all of this

`"model"` in `settings.json` is the default for a **new** session. Resume an old
one and it keeps whatever model it was already on — silently, indefinitely.
Verified, not assumed: a session started on Haiku and resumed with no `--model`
came back on Haiku, not on the configured Sonnet.

So every session you started before a policy change goes on ignoring that change
for as long as you keep resuming it. The long-lived sessions are the expensive
ones, which is the wrong way round.

Nothing can fix this from outside — no hook can set a session's model, and
`/model` is yours to run. What the `SessionStart` hook
(`common/claude/session-start.sh`) does is stop the drift being invisible: on
resume it compares the transcript's last turn against the policy and, when they
disagree, says so twice — once to you in the UI, once into the session so it
mentions it rather than quietly spending the dearer tier.

```
Resumed on model opus — 2.5x sonnet; policy is sonnet.
Switch with /model sonnet, or carry on if it is deliberate.
```

It names the multiplier only where one is true: `opus` is a fact, `2.5x` is a
reason, and claiming one for Sonnet would just train you to skip the line.
Effort counts too — the right model at the wrong effort is still off policy.

**It is quiet when the session is on policy** — no message, no injected context,
no cost. `opusplan` accepts a session on either Opus or Sonnet, since that is
what it does. A fresh start is never second-guessed: it already obeyed
`settings.json`, and `claude --model opus` is a deliberate choice.

To start a resume on the right model without switching afterwards:

```sh
claude --resume <id> --model sonnet
```

## Seeing which model you are on

The defaults above only apply to *new* sessions. `/model opus` or `codex -p sol`
changes what you are being billed for and changes nothing on disk, so a policy
you cannot see is a policy you find out about at the end of the month. The tmux
status bar names the model of whatever agent is in the current pane:

```
󰚩 opus/xh   … red    — opus · fable · sol   2.5x to 5x the default
󰚩 sonnet/hi … green  — sonnet · terra       on policy
󰚩 luna/md   … cyan   — haiku · luna         cheap
```

Nothing renders when the pane is not running an agent.

The suffix is the reasoning effort — `lo md hi xh mx` — and it is there because
effort is the *only* reasoning lever on Opus 5 and Sonnet 5 (`MAX_THINKING_TOKENS`
does nothing on them), so the same model at `max` and at `low` are quite
different amounts of spend. Both sides use the same abbreviations, so one glance
reads the same either way.

**How each side reports itself.** Neither tool answers questions from outside,
and the model is not visible in `ps` — `claude` started on Sonnet and switched
to Opus looks identical. So each writes a marker into
`~/.cache/dots/panes/<pane>` and `common/tmux/model.sh` reads it:

| | Reports via | Carries model + effort from | Catches a mid-session switch |
|---|---|---|---|
| Claude Code | `statusLine` hook (`common/claude/statusline.sh`) | the payload's `model.id` and `effort.level` | yes — it runs on every UI refresh |
| Codex | zsh `codex` wrapper, refreshed from the session rollout | the last `turn_context` record | yes — rollouts log model+effort per turn |

Codex needs the wrapper because its only hook, `notify`, is already taken by the
Computer Use app. A pane running `codex` with no marker at all — started before
the wrapper existed, or via `command codex` — still gets a segment, from the
configured default.

### The quota gauge

After the model comes the burn itself — the number every other line on this page
is trying to move:

```
5h 30%      dim    — under 60%, nothing to think about
7d 66% 1h   amber  — 60-84%, with time to reset
5h 91% 12m  red    — 85%+, this is what will stop you
```

Only one window shows: **whichever is closer to capping**. A 7-day at 88% stops
you while the 5-hour still reads a calm 12%, so showing both and leaving you to
compare them is the version of this that gets ignored. Time to reset appears
only from amber up, because below that the answer to "how long until this
clears" is "you do not care".

It comes from `rate_limits` in the statusLine payload, which is the only place
the live figure is readable without spending a turn on `/usage`. Unlike the
model, it is an **account** fact rather than a pane fact, so it lives in
`~/.cache/dots/claude-quota` and renders on *every* pane — the point is to see
the burn from a plain shell, not only while looking at a Claude session.

Whichever Claude session is running keeps the file current. With none running it
goes stale, and after 15 minutes the gauge disappears rather than showing an old
reading as though it were live. Codex contributes nothing here: its rollouts
carry no quota figures, and its allowance is a separate bucket on a separate
subscription anyway.

Two things this gauge is not: it is not Codex's usage, and it is not a
substitute for `./bin/claude-usage.py`, which is where the *per-model* breakdown
lives. This is a live fuel gauge; that is the logbook.

**Staleness is handled by asking someone who knows**, not by a timeout.
`statusline.sh` records its `$PPID`, which is the Claude process itself, so a
marker whose owner has exited is dropped on sight. Codex markers are checked
against tmux's own `pane_current_command`, which is authoritative even when a
killed terminal never ran the wrapper's cleanup.

The same script drives Claude Code's own in-session status line, so the model
is visible there too, in the same colours.

## Relative cost per token

| Claude | | Codex | |
|---|---|---|---|
| Fable 5 | 5× | Sol | 5× |
| Opus 5 | 2.5× | Terra | 2.5× |
| Sonnet 5 | 1× | Luna | 1× |
| Haiku 4.5 | 0.5× | | |

Codex 5-hour message allowance on Plus: Sol 15–90 · Terra 20–110 · Luna 50–280.

**Never use Fable for engineering.** It is never a default and only runs if
something selects it. On this account it silently became the single largest
line item — 4,256 main-session turns, ~46% of all spend — while doing work
Sonnet would have done at a fifth the price.

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
