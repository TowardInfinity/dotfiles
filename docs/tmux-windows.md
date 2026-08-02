---
title: Windows
group: tmux
order: 30
summary: Create, rename and switch windows and sessions
---

# Windows

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
| `w` | Window picker (`choose-tree`) | both |
| `P` | Pick a project under `~/Codes` and switch to or create its session | mac |
| `.` | Move this window to another index | mac |
| `:` | tmux's own command prompt | both |
