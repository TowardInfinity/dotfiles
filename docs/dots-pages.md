---
title: Authoring Pages
group: dots
order: 40
summary: Where docs pages come from, and how to add or edit one
---

# Authoring Pages

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

## Keeping a page from growing past its own point

A page gets split, the way this one and `models` were, once it stops answering
one question. The signal isn't a line count — it's whether the table of
contents in the sidebar ("ON THIS PAGE") stops being a map of one topic and
starts being a map of several unrelated ones bolted together. When that
happens: pick the first, most load-bearing facet as the anchor page and keep
its original filename and `group`, so nothing that already linked or searched
for it breaks; give the rest their own `<topic>-<facet>.md` files in the same
`group`, ordered after it. `tmux`, `zsh`, `Neovim` and `dots` itself all follow
this shape — none of them started as more than one page.
