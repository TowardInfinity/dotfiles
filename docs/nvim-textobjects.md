---
title: Text Objects
group: Neovim
order: 60
summary: Treesitter text objects and rename
---

# Text Objects

These compose with any operator, so `daf` deletes a whole function
however many lines it spans, and `vic` selects a class body.

| Object | Around / inside | Jump to next / previous |
| --- | --- | --- |
| Function | `af` / `if` | `]f` / `[f` |
| Class | `ac` / `ic` | `]c` / `[c` |
| Parameter | `aa` / `ia` | `]a` / `[a` |
| Conditional | `ai` / `ii` | — |
| Loop | `al` / `il` | — |
| Swap parameter | `Space n a` next | `Space p a` previous |

> **Rename is `Space r a`, not `Space r n`**
>
> NvChad already uses `Space r n` for toggling relative line numbers, so
> rename deliberately lives elsewhere rather than fighting it.
