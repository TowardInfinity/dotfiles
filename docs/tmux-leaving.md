---
title: Leaving
group: tmux
order: 50
summary: Detach, re-attach, and what actually kills what
---

# Leaving

The short answer is `prefix d` — **detach**. The session keeps running with
everything in it: your shells, your editors, that build you started an hour
ago. Detaching is not closing. It is stepping away from a machine that carries
on without you, which is the whole reason tmux exists.

Come back with `ta <name>`, or `tm` for the most recent one. Both are in
`dots zsh-tmux`.

## Leave, or destroy

The distinction worth holding on to: one of these throws work away and the
other does not.

| Action | Keys | What survives |
|---|---|---|
| Detach | `prefix d` | everything — processes keep running |
| Leave copy mode | `Escape` or `q` | you were only looking |
| Close a pane | `exit` or `Ctrl d` | the window, if other panes remain |
| Kill a pane | `prefix x` | the window, no confirmation asked |
| Kill a window | `prefix X` | the session |
| Kill a session | `prefix Q` | tmux itself — see below |
| Kill the server | `tks` | nothing at all |

`exit` is worth preferring over `prefix x` for a pane you are finished with:
it lets the shell close cleanly rather than having the pane pulled out from
under it.

## Why `Q` does not drop you back to the shell

Both configs set `detach-on-destroy off`. Killing the session you are sitting
in attaches you to another one instead of ejecting you from tmux. So `Q` will
appear not to work when you meant "get me out of here" — it did work, it just
put you somewhere else.

To actually leave, detach. To leave *and* stop the session, `Q` and then `d`.

## `tks` kills everything, on purpose

`tks` is `tmux kill-server` — every session on the machine, no confirmation.
It exists only on macOS, deliberately: the Linux boxes run long-lived sessions
and `linux/tmux/tmux.conf` says never to kill-server there. If you want one
session gone, use `tk <name>` or `prefix Q`.

## Forgotten a key?

`prefix ?` lists every binding tmux currently has, including the defaults these
configs never touch. It is the authoritative answer when this page and the
machine disagree — the machine is right.
