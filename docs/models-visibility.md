---
title: Visibility
group: Models
order: 20
summary: What a resumed session actually runs on, and how to check
---

# Visibility

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

