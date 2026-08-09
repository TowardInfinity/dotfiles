---
title: Panes & Keys
group: dots
order: 20
summary: The three panes, the arrow keys, and Manage's per-section keys
---

# Panes & Keys

## The three panes

`1` `2` `3` jump straight to one; `tab` cycles.

| | Pane | What it is for |
|---|---|---|
| `1` | **Docs** | These pages. `/` filters, `j`/`k` moves, `d`/`u` scrolls by half a screen, the wheel scrolls. |
| `2` | **Doctor** | Whether the tools these configs actually call are installed. `r` re-checks, `i` installs what is missing. |
| `3` | **Manage** | Everything stateful. `h`/`l` moves between its six sections. |

`q` quits from anywhere.

### The arrow keys

All four work, everywhere something can move. The status bar shows the letter
form because it is shorter, but the arrows are always aliases for it:

| | Moves the rail | Moves within the section |
|---|---|---|
| **Docs** | `↑` `↓` `←` `→` · `j` `k` `h` `l` | — |
| **Manage** | `←` `→` · `h` `l` | `↑` `↓` · `j` `k` |

Manage's rail and its section content never share a key: `←`/`→` always
switches sections, `↑`/`↓` always acts within whichever section is showing —
a row cursor where there's a list (Services, Packages, Projects, Machines), a
scroll offset where there's just text (Overview, Dotfiles). Where a section
has nothing to move, `↑`/`↓` does nothing rather than falling back to a
second meaning — an earlier version had `↑`/`↓` move the rail in the two
sections with no list, which meant the same keys meant two different things
depending on where you were; landing on Services mid-scroll and having
`↑`/`↓` suddenly mean something else was the actual bug report that got it
removed.

While a filter box is open (Services' or Packages' `/`), every key including
`h`/`j`/`k`/`l` goes into the filter text instead — none of the above fires
until you `enter`/`esc` out of it.

Doctor has neither a rail nor a list, so the arrows have nothing to move there.

Nothing scrolls sideways anywhere: the body is wrapped to the reading measure,
which is why `←`/`→` were free to mean "move the rail" in the first place.

### Manage's sections

**Overview** · **Dotfiles** · **Services** · **Projects** · **Machines**

| Key | Where | Does |
|---|---|---|
| `u` | Dotfiles | update — pull, then relink |
| `L` | Dotfiles | relink only |
| `p` `t` `D` | Dotfiles | nvim plugins · TPM · dependencies |
| `s` `x` `R` | Services | start · stop · restart |
| `a` | Services | toggle all / running only |
| `enter` | Projects | open a tmux session there |
| `d` | Machines | run doctor on that machine over SSH |
| `r` | most | rescan |

The status bar always shows the keys for whatever is in front of you, so this
table is a reference, not something to memorise.

## When it says you are ahead

```
  !  repo           1 uncommitted file, 2 unpushed commits
     publish reviewed paths with `dots publish`, then use `dots rollout`
```

The badge in the status bar and that row in `dots doctor` mean this machine's
configs have moved on and the others have not. Neither pushes anything — see
`dots operations` for the separate repository and fleet actions.
