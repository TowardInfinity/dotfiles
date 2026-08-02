# tmux

**The prefixes differ on purpose.** macOS uses `Ctrl a`; the Linux boxes
keep the stock `Ctrl b`. Nested sessions therefore never fight — outer
keystrokes act on the Mac, inner ones pass through to the server — and the
status bar is blue locally, green remotely, so a glance tells you which
layer you are in.

Everything below is pressed *after* the prefix unless marked no-prefix.
The Linux config targets tmux 3.2a and deliberately avoids anything newer,
which accounts for most of the macOS-only entries.

## Panes

| Keys | Action | Where |
| --- | --- | --- |
| `\|` | Split horizontally, keeping the current directory | both |
| `-` | Split vertically, keeping the current directory | both |
| `\` | Full-height split | both |
| `_` | Full-width split | both |
| `h j k l` | Move to the pane left / down / up / right | both |
| `Ctrl h/j/k/l` | Same, repeatable without re-pressing the prefix | both |
| `H J K L` | Resize the pane by 5, repeatable | both |
| `=` | Even-horizontal layout | both |
| `+` | Even-vertical layout | both |
| `z` | Zoom the pane in or out | mac |
| `S` | Toggle synchronised panes — type once, into all of them | both |
| `x` | Kill the pane, no confirmation | both |
| `e` | Break the pane out into its own window | both |
| `m` | Join this pane into a named window | mac |
| `Space` | Cycle to the next layout | mac |
| `Ctrl o` | Rotate panes within the window | mac |
| `B` | Toggle the pane border labels | both |

## Windows & sessions

| Keys | Action | Where |
| --- | --- | --- |
| `c` | New window, keeping the current directory | both |
| `a` | Jump back to the last window | both |
| `Ctrl p` / `Ctrl n` | Previous / next window, repeatable | both |
| `Shift ←` / `Shift →` | Previous / next window — **no prefix needed** | both |
| `,` | Rename the window | both |
| `<` / `>` | Move the window left / right in the list | both |
| `X` | Kill the window | both |
| `$` | Rename the session | both |
| `Q` | Kill the session, with a confirmation prompt | both |
| `Ctrl c` | New session | mac |
| `(` / `)` | Switch to the previous / next session | mac |
| `s` | Session picker — fzf popup on macOS, `choose-tree` on Linux | both |
| `w` | Window picker (`choose-tree`) | linux |
| `P` | Pick a project under `~/Codes` and switch to or create its session | mac |

## Popups & copy mode

| Keys | Action | Where |
| --- | --- | --- |
| `g` | lazygit in a popup over the current directory | mac |
| `t` | A scratch shell in a popup | both |
| `Ctrl f` | Pick any pane across every session and jump to it | mac |
| `Enter` | Enter copy mode | both |
| `v` | Start selecting (in copy mode) | both |
| `V` | Select the whole line | both |
| `Ctrl v` | Toggle block (rectangle) selection | both |
| `y` | Copy and exit — pbcopy on macOS, OSC 52 on Linux | both |
| `Y` | Copy, then paste it straight back | mac |
| `H` / `L` | Jump to start / end of line | both |
| `p` | Paste the buffer | both |
| `b` | List the paste buffers | mac |
| Scroll wheel | Enters copy mode instead of spamming the shell | both |
| Double-click | Select the word and copy it | mac |
| Right-click | Paste | mac |
| `r` | Reload `tmux.conf` | both |
| `F12` | Escape hatch — turn all outer bindings off, then on again | mac |
