---
title: AI launches
group: dots
order: 36
summary: Resume Claude, Codex, Grok, or Cursor in the current project with one consistent command
---

# AI launches

Each AI CLI has its own spelling for “continue where I left off.” `dots` gives
the four installed tools the same project-scoped entry point without replacing
their own session picker, history, or configuration.

```sh
dots claude              # claude --continue
dots codex               # codex resume --last
dots grok                # grok --continue
dots cursor              # cursor-agent --workspace <project-root> resume
```

The commands replace the current process with the selected CLI, so its normal
terminal controls, signals, and exit status behave exactly as if it had been
launched directly. Any trailing arguments are forwarded to that CLI.

## Project scope

Claude, Codex, and Grok all select their latest session from the current
working directory. `dots` preserves that native behaviour. Cursor receives the
resolved Git root explicitly, so invoking `dots cursor` from a subdirectory
does not create a second workspace identity.

Project identity follows `dots memory`: Git remote first, then Git root, then
the current directory. Phase 1 uses the tools' own cwd-scoped resume behaviour;
the later cross-tool console will use the same identity to select a session
without relying on an index or local model.

`ChatGPT` is intentionally absent: its desktop app has no CLI resume surface.
