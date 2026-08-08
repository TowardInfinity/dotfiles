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

This page covers what's configured and why. `dots models-visibility` covers
how you actually see what's running; `dots models-cost` and `dots
models-delegation` cover what to do about the bill.

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
Which model policy against it as a *main* model (below) is a spend argument,
and an advisor that fires unattended on every substantive step is a worse fit
for that argument than a main model you have to deliberately switch to — same
5x, less friction before it runs. Opus is the pick: the tier the policy
already reserves for hard design work, invoked as a second opinion instead of
the main seat. (Moot for now regardless — Fable is currently listed as an
unselectable "temporarily unavailable" row in the `/advisor` picker, per a
remotely controlled rollout, so `/advisor fable` is rejected either way.)

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

**`availableModels`** is a real allowlist, checked server-side by the same
code path `/model` calls to validate a switch — not just a prose rule. Fable
sitting outside the list means `/model fable` is rejected server-side rather
than merely discouraged; the same check gates subagent, skill, and teammate
model resolution too. It's excluded on purpose: it once silently became the
single largest line item on this account — 4,256 main-session turns on a
model that's 5x Sonnet's cost — as a deliberate main-model pick, not an
accident a prose rule would have caught in time. Wanting Fable back for
narrative/creative work means editing this key first, deliberately, rather
than it staying reachable by default.

**`fastModePerSessionOptIn`** — Fast mode runs Opus. Without this key,
turning it on with `/fast` risks persisting as the saved default for
sessions after the one that needed it, the same silent-drift shape as a
resumed session staying on the wrong model. `true` scopes it to the session
that opted in.

**`CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS: "3"`** and
**`CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH: "1"`** put a floor under "never
enable agent teams": agent teams are the extreme case of many concurrent,
nested agents, and these two caps block the same shape short of the feature
flag itself. 3 concurrent still allows a real Explore/Plan fan-out; depth 1
still allows a normal single-level `Agent` call, it just stops that subagent
from spawning one of its own — so ordinary delegation is untouched, and only
the runaway-nesting shape is closed off.
