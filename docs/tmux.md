---
title: Prefixes
group: tmux
order: 10
summary: Why the two tmux prefixes differ
---

# Prefixes

**The prefixes differ on purpose.** macOS uses `Ctrl a`; the Linux boxes
keep the stock `Ctrl b`. Nested sessions therefore never fight — outer
keystrokes act on the Mac, inner ones pass through to the server — and the
status bar is blue locally, green remotely, so a glance tells you which
layer you are in.

Everything below is pressed *after* the prefix unless marked no-prefix.
The Linux config targets tmux 3.2a and deliberately avoids anything newer,
which accounts for most of the macOS-only entries.

On macOS, `prefix Ctrl-a` sends a literal `Ctrl-a` through to whatever is
running in the pane — needed when that program wants the key itself, readline's
start-of-line being the common case.

`prefix ?` lists every binding the running server has, including the defaults
these files never touch. When this reference and the machine disagree, the
machine is right.
