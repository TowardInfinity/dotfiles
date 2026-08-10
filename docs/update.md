---
title: Updating
group: Maintain
order: 20
summary: Pulling updates and rolling them out
---

# Updating

## The short version

```sh
dots sync
```

Fetches, fast-forwards when the checkout can advance, applies the new
configuration, and prints the commits you just picked up. A current checkout is
only verified; correct symlinks are left alone and nothing is overwritten
silently.

Then, to make a running session notice:

```sh
exec zsh
tmux source-file ~/.config/tmux/tmux.conf
```

Never `tmux kill-server` on a box with live sessions. `source-file` applies
the config in place and leaves your sessions running.

`dots sync` updates only this machine. `dots update` is a deprecated alias for
it, kept for scripts and muscle memory. Publishing a reviewed change and
rolling it out are separate, explicit operations; `dots operations` records
their exact boundaries.

## Changing a config

The live files are symlinks into the repo, so your edit is already live —
there is nothing to copy. Publish the paths you reviewed, rather than staging
the whole working tree by default:

```sh
dots publish common/nvim/lua/options.lua -m "nvim: tune options"
```

Then roll it out deliberately.

## Rolling a change out to every machine

`dots sync` no longer commits, pushes, or contacts other machines. It fetches,
checks for local drift or divergence, fast-forwards safely, and applies this
checkout. To change other machines, publish a reviewed selection and then
choose the rollout targets:

```sh
dots publish common/nvim/lua/options.lua -m "nvim: tune options"
dots rollout a1                         # canary
dots rollout v1 v2                      # remaining servers
```

The former `dots sync -m` / `--push-only` flags now fail and direct you to
`dots publish`; `--remotes-only` directs you to `dots rollout`. A bare `dots
update` remains the same safe inbound operation for older habits and scripts.

### Being reminded

The failure mode sync exists to prevent is silent: edit a config, relink, and
this box looks correct while every other one quietly disagrees. So `dots` shows
a status-bar badge and `dots doctor` prints a row when the checkout is ahead:

```
  !  repo           1 uncommitted file, 2 unpushed commits
     publish reviewed paths with `dots publish`, then use `dots rollout`
```

It only ever reports. Nothing pushes without you asking, because a push is
outward-facing and updates machines you are not looking at. Untracked files are
not counted — a working machine collects backups and caches inside the repo, and
counting those would nag on every run until the badge stopped meaning anything.
`dots doctor`'s exit status is unaffected: unpushed work is normal mid-edit, not
a failure.

## main is protected

The GitHub repo is public, so anyone can read it, fork it and open a pull
request — but write access is the owner's alone, and a ruleset on `main` blocks
force-pushes and branch deletion. It deliberately does **not** require pull
requests: `dots publish` pushes reviewed selections straight to `main`, and
requiring review would break that direct-main workflow. The threat being defended
against is an accident, not an intruder.

Secret scanning and push protection are on, so a credential in a config is
caught at `git push` rather than after it is public.

## Rolling a change out to one server

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

## Re-running the installer

On a machine that already has `dots`, you do not need the curl one-liner:

```sh
dots install          # relink every config
dots install --deps   # also install the tools the configs call
dots install --dry    # print what would happen, change nothing
```

`dots install` is repair and top-up — you already have the checkout. The
one-liner below is for a machine that has none of this yet.

## Reinstalling from scratch

```sh
sh -c "$(curl -fsSL https://toin.in/install)" -- --deps
```

Existing files are moved to `<path>.backup.<timestamp>` rather than
overwritten. `--dry` first if you want to see the plan without committing to
it.

## `install` and `update` are commands

`dots install` and `dots update` do things. The pages of the same name — this
one included — are at `dots docs install` and `dots docs update`, and are
always available in the Docs tab.
