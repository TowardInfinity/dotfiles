---
title: Copy Mode
group: tmux
order: 40
summary: Popups, copy mode and clipboard behavior
---

# Copy Mode

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
