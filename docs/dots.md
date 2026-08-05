---
title: dots
group: Start
order: 5
summary: The command you are already running — panes, keys and every subcommand
---

# dots

Every other page here documents a tool. This one documents the thing you are
reading them in.

`dots` is two programs wearing one name. With no arguments it opens a
full-screen browser, which is what you want when you are looking *for*
something. With a subcommand it prints and exits, which is what you want in a
script, in a pipe, or when you already know the answer's name and do not want a
full-screen program in the way.

```sh
dots              # browse
dots tmux         # print the tmux page
dots search copy  # find which page mentions it
```

## The three panes

`1` `2` `3` jump straight to one; `tab` cycles.

| | Pane | What it is for |
|---|---|---|
| `1` | **Docs** | These pages. `/` filters, `j`/`k` moves, `d`/`u` scrolls by half a screen, the wheel scrolls. |
| `2` | **Doctor** | Whether the tools these configs actually call are installed. `r` re-checks, `i` installs what is missing. |
| `3` | **Manage** | Everything stateful. `h`/`l` moves between its five sections. |

`q` quits from anywhere.

### The arrow keys

All four work, everywhere something can move. The status bar shows the letter
form because it is shorter, but the arrows are always aliases for it:

| | Moves the rail | Moves the list |
|---|---|---|
| **Docs** | `↑` `↓` `←` `→` · `j` `k` `h` `l` | — |
| **Manage** — Overview, Dotfiles | `↑` `↓` `←` `→` · `j` `k` `h` `l` | — |
| **Manage** — Services, Projects, Machines | `←` `→` · `h` `l` | `↑` `↓` · `j` `k` |

The one asymmetry is deliberate. Three of Manage's sections put a list in the
body, and there `↑`/`↓` has to drive the list — so the rail keeps `←`/`→`. In
the two sections with no list, `↑`/`↓` falls through to the rail instead of
doing nothing.

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

## Subcommands

| | |
|---|---|
| `dots` | browse interactively |
| `dots <topic>` | print one page |
| `dots search <term>` | search every page |
| `dots topics` | list every page with its summary |
| `dots doctor` | check the tools these configs call |
| `dots install` | relink configs; `--deps` also installs the tools |
| `dots update` | pull the latest configs, then relink |
| `dots sync` | push changes here, then update every other machine |
| `dots edit [topic]` | open a page in `$EDITOR` |
| `dots path` | print the repo path |
| `dots version` | print this binary's version |

### install, update and docs are verbs first

`dots install` and `dots update` *do* things. The pages of those names are one
level down:

```sh
dots docs install
dots docs update
```

Everything else is a page directly: `dots tmux`, `dots models`, `dots zsh`.

### It behaves in a pipe

Colour and word-wrapping are dropped when stdout is not a terminal, so
`dots tmux | grep prefix` reads like a text file rather than a screenshot.
`dots doctor` exits non-zero when something is missing, so it works as a CI or
script step. `dots search` exits non-zero on no match.

## Where the pages come from

`docs/*.md` in the repo. Each has front matter that decides where it lands:

```markdown
---
title: dots
group: Start
order: 5
summary: One line, shown in `dots topics`
---
```

`group` is the section in the Docs pane, `order` sorts within it. Add a file,
and it appears — there is no index to update. `dots edit <topic>` opens one in
`$EDITOR`; `dots path` tells you where they live.

Pages are read from the checkout when there is one, so an edit shows up
immediately. A `--copy` install has no checkout, and the binary falls back to
the copy baked into it at build time.

## When it says you are ahead

```
  !  repo           1 uncommitted file, 2 unpushed commits
     run `dots sync` to push and update the other machines
```

The badge in the status bar and that row in `dots doctor` mean this machine's
configs have moved on and the others have not. Neither pushes anything — see
`dots docs update` for what `dots sync` does and why it asks twice.
