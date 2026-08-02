---
title: tmux Gotchas
group: Maintain
order: 30
summary: tmux prefixes, clipboard and clock gotchas
---

# tmux Gotchas

Things that cost real time to work out. Each one is a decision, not an
accident — changing it back will reintroduce the bug.

## Why the two tmux prefixes differ

Mac is `Ctrl a`, servers keep the stock `Ctrl b`. When you SSH into a box
and run tmux there, you have two tmux layers stacked. Different prefixes
mean each keystroke lands unambiguously — no toggle mode, no double-tap.
The status bar colour tells you which layer you are in.

*Source: macos/tmux/tmux.conf · linux/tmux/tmux.conf*

## Don't add xclip to the servers

Copying inside tmux on a server lands on your Mac's clipboard already, via
OSC 52, through both tmux layers and the SSH pipe. That is why the Linux
copy bindings don't pipe to a local clipboard tool — there isn't one, and
there doesn't need to be.

*Source: linux/tmux/tmux.conf*

## The `%%` in the tmux clock is not a typo

tmux runs `status-right` through strftime *before* executing any `#()`
shell job inside it. So a plain `date +%H:%M` gets rewritten to
`date +14:29` first, and the shell just echoes that back — silently
showing UTC while looking like it worked. Escaping as `%%H:%%M` passes a
literal `%` through to the shell.

The servers deliberately stay on UTC. Changing the system timezone would
shift cron and every log line, so the bar renders IST for wall-clock
parity and keeps raw UTC beside it.

*Source: linux/tmux/tmux.conf*
