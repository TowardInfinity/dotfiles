---
title: Visibility
group: Models
order: 20
summary: What a resumed session actually runs on, and how to check
---

# Visibility

## Resumed sessions ignore all of this

`claude --resume` (or `--continue`) restarts on **whatever model that session
last used**, not the current `settings.json` default. Change the default
after a session was already running Opus, resume it later, and it is still on
Opus — the new default only applies to sessions that hadn't started yet.

There is no setting that fixes this; it's a property of how a session is
serialized. The only fix is checking, every time a resumed session matters,
rather than trusting the default. That's what the next section is for.

## Seeing which model you are on

`/model` with no argument shows the current model without changing it.
The footer also shows it, but only in some terminal widths — `/model` is the
one that always works.

`/cost` shows the running total for the session in USD-equivalent, and is the
fastest way to notice a resumed session landed on a more expensive model than
intended: a Sonnet session an hour in should not already be reading as an
Opus-shaped number.

### The quota gauge

`/usage` shows the 5-hour and weekly windows directly, as fractions used, not
dollars — the thing that actually gates whether the next call works at all.
Both `/cost` and `/usage` read local session state, not a server call, so
they're free to check often. Checking before a long delegated task (a big
`Agent` fan-out, a long `Agent` run) is worth the habit: finding out a window
is nearly spent after starting a 20-minute subagent is the expensive way to
learn it.
