---
title: Updating
group: Maintain
order: 20
summary: Pulling updates and rolling them out
---

# Updating

## The short version

```sh
dots update
```

Pulls, relinks, and prints the commits you just picked up. Safe to run any
time — correct symlinks are left alone and nothing is overwritten silently.

Then, to make a running session notice:

```sh
exec zsh
tmux source-file ~/.config/tmux/tmux.conf
```

Never `tmux kill-server` on a box with live sessions. `source-file` applies
the config in place and leaves your sessions running.

## Changing a config

The live files are symlinks into the repo, so your edit is already live —
there is nothing to copy. What remains is committing it:

```sh
cd "$(dots path)"
git add -A && git commit -m "..."
git push
```

Then on any other machine, `dots update`.

## Rolling a change out to the servers

```sh
ssh a1
dots update
```

If a box does not have `dots` yet, it needs one install first:

```sh
sh -c "$(curl -fsSL https://toin.in/install)"
```

To test a risky tmux change without touching live sessions, load it on a
throwaway socket:

```sh
tmux -L probe -f linux/tmux/tmux.conf new-session -d
tmux -L probe show-messages | grep -i error
tmux -L probe kill-server
```

## Adding a tool

Two places, and both matter:

1. `bootstrap.sh` — add it to the brew list or the apt list so a fresh
   machine gets it.
2. The `verify()` list in `bootstrap.sh`, **but only if a config actually
   calls it**. That list is deliberately built from what `.zshrc` and
   friends reference, not from what `--deps` installs. Conflating the two is
   how a missing tool went unnoticed until an alias failed at a prompt.

Then `dots doctor` should show it, and a fresh machine converges on its own.

## Updating Neovim plugins

```sh
nvim +Lazy
```

`U` updates, `S` syncs, `X` cleans. Afterwards, commit `lazy-lock.json` — it
is tracked so every machine can land on the same commits.

To bring a second machine to those exact commits:

```sh
nvim --headless "+Lazy! restore" +qa
```

Be aware of what restore does and does not do. It checks out **commits**, never
branches. If a plugin needs to stay on a particular branch, that has to be
`branch =` in the plugin spec — the lockfile records a branch but cannot
enforce it, and lazy rewrites the lockfile from whatever is on disk after
every operation, so drift gets saved as if it were intended. See
`dots gotchas`.

## Updating these docs

The docs are markdown in `docs/`, one file per topic. Add a file and it shows
up in `dots` automatically — the topic list is read from the directory, not
hardcoded.

```sh
dots edit tmux        # edit one topic
dots edit             # open the whole repo
```

## When something looks wrong

```sh
dots doctor
```

Checks every tool the configs actually call, plus Oh My Zsh and TPM, and
prints the command to fix what is missing.

If a binding in the docs disagrees with the machine, **the machine is
right** — the docs were generated from the configs and have drifted. Fix the
doc.

## Reinstalling from scratch

```sh
sh -c "$(curl -fsSL https://toin.in/install)" -- --deps
```

Existing files are moved to `<path>.backup.<timestamp>` rather than
overwritten. `--dry` first if you want to see the plan without committing to
it.
