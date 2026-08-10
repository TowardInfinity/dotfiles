---
title: Defaults
group: Models
order: 10
summary: Which model to use where, and the settings that actually enforce it
---

# Defaults

Two subscriptions, $20 each: **Claude Pro** (Claude Code) and **ChatGPT Plus**
(Codex). Both cap usage on a rolling 5-hour window *and* a weekly window that
stacks on top. Claude's window is shared with claude.ai chat; Codex's is shared
across CLI, IDE and web.

Switching model mid-session does **not** restore access once you are capped —
every model draws on the same allowance. The only thing that helps is spending
less per turn.

This page is the configured policy and what enforces it. `dots
models-visibility` is how to see what a session is actually running, `dots
models-cost` what burns the allowance, `dots models-delegation` when to hand
work to a subagent or the other tool.

## Defaults

| | Default | Escalate with |
|---|---|---|
| Claude Code | Sonnet 5, effort `high` | `/model opusplan` |
| Codex | Terra, effort `medium` | `codex -p sol` |

Both defaults live in the repo and travel through `dots publish` followed by
`dots rollout`:
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

The `advisor` tool (server-side, Anthropic API only — not Bedrock, Vertex, or
Foundry) sends the whole transcript to a second, typically stronger model at
decision points: before committing to an approach, when an error keeps
recurring, before declaring a task done. Claude decides when to call it, not a
fixed schedule.

```json
"advisorModel": "opus"
```

Persisted in `settings.json` next to `model` and `effortLevel` — confirmed both
in the installed binary's settings schema and in the
[official docs](https://code.claude.com/docs/en/advisor). Three equivalent ways
to set it: `/advisor opus` (saves it, same as editing the file), `--advisor
opus` for one session without touching the saved default, or the JSON key
directly. `/advisor off` clears it; `CLAUDE_CODE_DISABLE_ADVISOR_TOOL=1` kills
the tool outright regardless of what's saved.

**Pairing rule, enforced server-side:** the advisor must be at least as capable
as the session's main model — equals allowed, downgrades rejected. On Sonnet
that admits Opus, Sonnet itself, or Fable. Fable is not the choice here: the
[Which model](#defaults) policy against it as a *main* model is a spend
argument, and an advisor that fires unattended on every substantive step is a
worse fit for that argument than a main model you have to deliberately switch
to — same 5x, less friction before it runs. Opus is the pick: the tier the
policy already reserves for hard design work, invoked as a second opinion
instead of the main seat. (Moot for now regardless — Fable is currently listed
as an unselectable "temporarily unavailable" row in the `/advisor` picker, per
a remotely controlled rollout, so `/advisor fable` is rejected either way.)

**Cost:** each call bills at the advisor's rate on top of the main model's
usage and counts toward the same plan limits shown by `/usage` — this is why
it's occasional-2.5x rather than continuous-2.5x, not why it's free. Toggling
`/advisor` mid-session does not invalidate the main model's prompt cache,
unlike switching `/model` or `/effort`, which do.

## Enforcement, not just prose

Everything above this line is Claude *deciding* to follow policy — a resumed
session, an advisor pick, a `/model` typo can still land on the wrong tier.
Four keys close specific gaps instead of just documenting them. All four were
confirmed live in the installed binary's settings schema, not assumed from
docs, the same way `advisorModel` was.

```json
"availableModels": ["opus", "sonnet", "haiku"],
"fastModePerSessionOptIn": true,
"env": {
  "CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS": "3",
  "CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH": "1"
}
```

**`availableModels`** is a correction to an earlier version of this doc, which
said no setting could pin Fable off. Wrong: it's a real allowlist, checked
server-side by the same code path `/model` calls to validate a switch. Fable
outside the list means `/model fable` is rejected outright —
`"Model 'fable' is not available. Your organization restricts model
selection."` — and the same check gates subagent, skill, and teammate model
resolution. The docs call it "typically set in managed settings by enterprise
administrators"; that's a description of who usually bothers, not a
restriction on where it's read from — the merge order
(`userSettings → projectSettings → localSettings → flagSettings →
policySettings`) treats plain `~/.claude/settings.json` as a normal layer.
Fable is excluded on purpose: it was 46% of spend as a deliberate main-model
pick, not an accident a prose rule would have caught anyway. The tradeoff,
accepted knowingly: the documented "creative and narrative work only"
exception is no longer reachable without editing this file first. If that
exception is ever wanted again, it's a one-line settings.json edit and a
`policy_key` update, not a policy violation.

**`fastModePerSessionOptIn`** — Fast mode runs Opus. Without this key, turning
it on with `/fast` can persist as the saved default for sessions after the one
that needed it, the same silent-drift shape as a resumed session staying on
the wrong model. `true` scopes it to the session that opted in.

**`CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS`** (unset default: 20) and
**`CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH`** (unset default: 3) give
`delegation.md`'s "never enable agent teams" line a floor under it. Agent
teams are the extreme case of many concurrent, nested agents; an unset default
of 20 concurrent and 3 levels deep is most of the way there without the
feature flag. 3 concurrent still allows a real Explore/Plan fan-out; depth 1
still allows a normal single-level `Agent` call, it just stops that subagent
from spawning one of its own.

Not hypothetical: this is Anthropic's own back-and-forth, not a guess about
what could go wrong. `v2.1.217` (Jul 21 '26) shipped depth 1 as the default —
no nesting — specifically to stop unbounded fan-out, alongside the 20-concurrent
cap ("so one message can't fan out unbounded background agents," per their own
release notes) and a fix so `--max-budget-usd` actually halted background
subagents instead of spending past it silently. Three days later, `v2.1.219`
walked the depth back up to 3 to "reinstate nesting." [Issue
#68110](https://github.com/anthropics/claude-code/issues/68110) — opened
before either release, still open — is what depth 1 was protecting against
and depth 3 reopened: a single "research X" delegation recursively spawned 48+
background agents, burned 1.5M+ tokens, with duplicate agents redoing the same
sub-task (four separate agents independently researching the same API). Pinning
`CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH: "1"` here just keeps the fix Anthropic
itself shipped and then reverted three days later, for a bug that's still open.
