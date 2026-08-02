---
title: Editing
group: Neovim
order: 20
summary: Moving around and editing text
---

# Editing

| Keys | Action | Mode |
| --- | --- | --- |
| `Ctrl d` / `Ctrl u` | Half page down / up, cursor re-centred | `n` |
| `n` / `N` | Next / previous search result, re-centred | `n` |
| `J` | Join lines without the cursor jumping | `n` |
| `Alt j` / `Alt k` | Move the line or selection down / up | `n v` |
| `<` / `>` | Indent / outdent, keeping the selection | `v` |
| `p` | Paste over a selection without losing your register | `v` |
| `Space y` / `Space Y` | Yank to the system clipboard / yank the line | `n v` |
| `Space r w` | Replace the word under the cursor across the file | `n` |
| `s` / `S` | Flash — jump anywhere on screen / jump by syntax node | `n x o` |
