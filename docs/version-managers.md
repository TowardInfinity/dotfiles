---
title: Version Managers
group: Reference
order: 25
summary: Installing, switching and removing versions with fnm, uv and SDKMAN
---

# Version Managers

`docs/tools.md` says these three exist and why — `--deps` installs the
manager, not the language, so versions stay per-project instead of
per-machine. This page is the part that comes after that: the actual
commands for installing a version, seeing what's there, switching, and
getting rid of one.

All three keep versions side by side rather than replacing one with another —
installing a new version never removes the old one, and switching never
reaches into a project that pinned something specific.

## fnm — Node

| | |
|---|---|
| Install a version | `fnm install <version>` (`fnm install --lts` for latest LTS) |
| List installed | `fnm list` |
| Current shell's version | `fnm current` |
| Switch (this shell) | `fnm use <version>` |
| Set the default | `fnm default <version>` |
| Remove | `fnm uninstall <version>` |

`fnm use` also fires automatically on `cd` into a directory with a
`.nvmrc`/`.node-version` file. Without one, and without an explicit `fnm use`,
the shell falls back to whatever `fnm default` last set — or to `system` if
that was never run, meaning fnm is installed but Node is actually whatever
Homebrew (or the OS) put on `PATH`. `fnm list` reporting only `* system` is
how you tell the two apart.

## uv — Python

Two different things share the `uv` name here: `uv python` manages
interpreters (this section); `uv tool` manages CLIs built with one — see
`docs/packages.md` for that half, it's a different command family entirely.

| | |
|---|---|
| Install a version | `uv python install <version>` |
| List installed | `uv python list --only-installed` |
| List everything downloadable | `uv python list` |
| Pin a project to one | `uv python pin <version>` |
| Remove | `uv python uninstall <version>` |

No `use`/`default` here — uv's model is per-project, not per-shell. `pin`
writes a `.python-version` file into the current directory, and every `uv
run`/`uv venv` there honors it; there's nothing global to switch. `uv python
list --only-installed` mixes uv-managed interpreters with ones it merely
found (Homebrew's own Python, the system `/usr/bin/python3`) — appearing in
that list means uv knows where it is, not that uv fetched it.

## SDKMAN — Java (and friends)

| | |
|---|---|
| See what's available | `sdk list java` |
| Install a version | `sdk install java <version>` |
| See installed + current | `sdk current java` |
| Switch (this shell only) | `sdk use java <version>` |
| Set the default | `sdk default java <version>` |
| Remove | `sdk uninstall java <version>` |

Despite the candidate name, SDKMAN isn't Java-only — `sdk list` with no
candidate shows everything it can manage (gradle, maven, kotlin, and more),
though Java is the only one `--deps` installs by default. `sdk default`
repoints `~/.sdkman/candidates/java/current`, the symlink Neovim's `jdtls`
config and both `.zshrc`s point at; `sdk use` changes it for the current
shell only and leaves that symlink alone — the same current-vs-this-shell
split `fnm default`/`fnm use` draw.
