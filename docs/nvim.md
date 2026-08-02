# Neovim

Leader is `Space`. NvChad's own defaults are not repeated here — this is
only what these configs add or override. Press `Space` and wait to see
which-key's live menu.

## Essentials

| Keys | Action | Mode |
| --- | --- | --- |
| `j k` | Escape from insert mode | `i` |
| `;` | Enter command mode, without reaching for shift | `n` |
| `Ctrl s` | Save | `n i v` |
| `Space q q` | Quit everything | `n` |
| `Esc` | Clear the search highlight | `n` |

## Moving & editing

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

## Splits

| Keys | Action | Mode |
| --- | --- | --- |
| `Space s v` | Split vertically | `n` |
| `Space s s` | Split horizontally | `n` |
| `Space s c` | Close the split | `n` |
| `Ctrl ↑↓←→` | Resize the split | `n` |

## Code — lsp, diagnostics, trouble

| Keys | Action | Mode |
| --- | --- | --- |
| `g r` | Find references | `n` |
| `g i` | Go to implementation | `n` |
| `Space c a` | Code action | `n v` |
| `Space s g` | Signature help (also `Ctrl k` in insert) | `n` |
| `Space d d` | Show the diagnostic under the cursor | `n` |
| `[ d` / `] d` | Previous / next diagnostic | `n` |
| `[ q` / `] q` | Previous / next quickfix item | `n` |
| `Space x x` | Trouble — every diagnostic in the project | `n` |
| `Space x b` | Trouble — this buffer only | `n` |
| `Space x s` | Symbol outline | `n` |
| `Space x q` | Quickfix list | `n` |
| `Space f t` | Find every TODO / FIXME in the project | `n` |
| `Space u u` | Undo tree | `n` |
| `Space J b` / `J d` | Java — toggle breakpoint / continue debugging | `n` |

## Toggles — all under `Space u`

| Keys | Action | Mode |
| --- | --- | --- |
| `Space u f` | Format-on-save for this buffer | `n` |
| `Space u F` | Format-on-save globally | `n` |
| `Space u w` | Line wrap | `n` |
| `Space u s` | Spellcheck | `n` |
| `Space u v` | Inline diagnostic text | `n` |
| `Space u h` | Inlay hints | `n` |
| `Space u m` | Rendered markdown | `n` |
| `Space z r` / `z m` | Open / close all folds | `n` |

## Treesitter text objects

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
